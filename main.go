package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AndreHeber/go-sqlite-blog/config"
	dbService "github.com/AndreHeber/go-sqlite-blog/db"
	"github.com/AndreHeber/go-sqlite-blog/handlers"
	"github.com/AndreHeber/go-sqlite-blog/middleware"
	"github.com/AndreHeber/go-sqlite-blog/models"
	"github.com/AndreHeber/go-sqlite-blog/models/users"
)

func main() {
	// reading env variables
	cfg, err := config.LoadConfig()
	if err != nil {
		slog.Error("main: Error loading config", "error", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))
	slog.SetDefault(logger)

	if cfg.Database.Reset {
		slog.Info("Resetting database", "source", cfg.Database.Source)
		// delete database file (不存在时忽略，保证首次启动 --database-reset 也能工作)
		if err := os.Remove(cfg.Database.Source); err != nil && !os.IsNotExist(err) {
			slog.Error("main: Error deleting database file", "error", err)
			os.Exit(1)
		}
	}

	slog.Info("Initializing database", "driver", cfg.Database.Driver, "source", cfg.Database.Source)
	db, err := dbService.Init(logger, cfg.Database.Driver, cfg.Database.Source, "./tables.sql")
	if err != nil {
		os.Exit(1)
	}

	// 管理员提升机制：启动时把指定用户设为 admin，便于没有后台入口时完成首次授权
	if cfg.PromoteAdmin != "" {
		env := &models.Env{DB: db, Ctx: context.Background(), Logger: logger, LogDBQueries: cfg.Database.LogQueries}
		if err := users.PromoteToAdmin(env, cfg.PromoteAdmin); err != nil {
			slog.Error("main: Error promoting user to admin", "username", cfg.PromoteAdmin, "error", err)
			os.Exit(1)
		}
		slog.Info("main: User promoted to admin", "username", cfg.PromoteAdmin)
	}

	adapter := middleware.Init(logger, db, cfg.ErrorsInResponse, cfg.Database.LogQueries, cfg.IPRateLimit, cfg.BurstRateLimit)
	mux := setupRouter(adapter)

	// start server
	slog.Info("Starting server", "port", cfg.Port)

	// Create custom server with timeouts
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           mux,
		ReadTimeout:       15 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}

	// Graceful shutdown
	// 监听失败（例如端口被占用）必须让进程退出，否则会僵死在下面的 <-quit 上，
	// 让人误以为「改了代码但服务没生效」。
	serverErr := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		slog.Error("main: Error starting server", "error", err, "port", cfg.Port)
		os.Exit(1)
	case <-quit:
	}

	// Gracefully shutdown the server
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("main: Server forced to shutdown", "error", err)
	}
}

func setupRouter(adapter *middleware.Adapter) *http.ServeMux {
	mux := http.NewServeMux()

	// 上传的图片：/uploads/<name> 映射到 static/uploads/<name>，不列目录
	if err := os.MkdirAll(handlers.UploadsDir, 0o755); err != nil {
		slog.Error("main: Error creating uploads directory", "dir", handlers.UploadsDir, "error", err)
	}
	mux.Handle("GET "+handlers.UploadsURLPrefix, middleware.StaticFiles(handlers.UploadsURLPrefix, handlers.UploadsDir))

	mux.Handle("GET /health", adapter.HTTPToContextHandler(handlers.Health))
	mux.Handle("GET /time-consuming", adapter.HTTPToContextHandler(handlers.TimeConsumingHandler))

	mux.Handle("GET /register", adapter.HTTPToContextHandler(handlers.ShowRegister))
	mux.Handle("POST /register", adapter.HTTPToContextHandler(handlers.TryRegister))

	mux.Handle("GET /login", adapter.HTTPToContextHandler(handlers.ShowLogin))
	mux.Handle("POST /login", adapter.HTTPToContextHandler(handlers.TryLogin))
	mux.Handle("POST /logout", adapter.HTTPToContextHandler(handlers.Logout))

	mux.Handle("GET /post/new", adapter.HTTPToContextHandler(middleware.RequireLogin(handlers.ShowEditor)))
	mux.Handle("POST /post", adapter.HTTPToContextHandler(middleware.RequireLogin(handlers.CreateArticle)))
	mux.Handle("GET /post/{id}", adapter.HTTPToContextHandler(handlers.ShowArticle))
	mux.Handle("GET /post/{id}/edit", adapter.HTTPToContextHandler(middleware.RequireLogin(handlers.ShowEditArticle)))
	mux.Handle("POST /post/{id}/edit", adapter.HTTPToContextHandler(middleware.RequireLogin(handlers.UpdateArticle)))
	mux.Handle("POST /post/{id}/comment", adapter.HTTPToContextHandler(middleware.RequireLogin(handlers.CreateComment)))

	// 图片上传（登录用户即可，用于在文章中插图）
	mux.Handle("POST "+handlers.UploadsPostPath, adapter.HTTPToContextHandler(middleware.RequireLogin(handlers.UploadImage)))

	// 管理后台（handler 内部校验 admin 角色，非管理员返回 403 页面）
	mux.Handle("GET /admin", adapter.HTTPToContextHandler(handlers.AdminDashboard))
	mux.Handle("POST /admin/comments/{id}/approve", adapter.HTTPToContextHandler(handlers.ApproveComment))
	mux.Handle("POST /admin/comments/{id}/delete", adapter.HTTPToContextHandler(handlers.DeleteComment))
	mux.Handle("POST /admin/articles/{id}/approve", adapter.HTTPToContextHandler(handlers.ApproveArticle))
	mux.Handle("POST /admin/articles/{id}/delete", adapter.HTTPToContextHandler(handlers.DeleteArticle))

	// 注意：Go 1.22+ ServeMux 中 "GET /" 会兜底所有未匹配的 GET 路径，
	// ListArticles 内部会对非 "/" 路径返回 404。
	mux.Handle("GET /", adapter.HTTPToContextHandler(handlers.ListArticles))

	return mux
}
