package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/AndreHeber/go-sqlite-blog/config"
	dbService "github.com/AndreHeber/go-sqlite-blog/db"
	"github.com/AndreHeber/go-sqlite-blog/handlers"
	"github.com/AndreHeber/go-sqlite-blog/middleware"
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
		slog.Info("Resetting database")
		// delete database file
		err = os.Remove(cfg.Database.Source)
		if err != nil {
			slog.Error("main: Error deleting database file", "error", err)
			os.Exit(1)
		}
	}

	slog.Info("Initializing database", "driver", cfg.Database.Driver, "source", cfg.Database.Source)
	db, err := dbService.Init(logger, cfg.Database.Driver, cfg.Database.Source, "./tables.sql")
	if err != nil {
		os.Exit(1)
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
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("main: Error starting server", "error", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit

	// Gracefully shutdown the server
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("main: Server forced to shutdown", "error", err)
	}
}

func setupRouter(adapter *middleware.Adapter) *http.ServeMux {
	mux := http.NewServeMux()

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

	// 注意：Go 1.22+ ServeMux 中 "GET /" 会兜底所有未匹配的 GET 路径，
	// ListArticles 内部会对非 "/" 路径返回 404。
	mux.Handle("GET /", adapter.HTTPToContextHandler(handlers.ListArticles))

	return mux
}
