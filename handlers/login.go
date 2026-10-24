package handlers

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/AndreHeber/go-sqlite-blog/middleware"
	"github.com/AndreHeber/go-sqlite-blog/models"
	"github.com/AndreHeber/go-sqlite-blog/models/users"
)

// ShowLogin renders the login page
func ShowLogin(a *middleware.Adapter) error {
	return renderPage(a, "login.html", ViewData{Title: "登录"})
}

// TryLogin handles the login form submission
func TryLogin(a *middleware.Adapter) error {
	r := a.Request

	if err := r.ParseForm(); err != nil {
		return fmt.Errorf("TryLogin: %w", err)
	}
	username := r.FormValue("username")
	password := r.FormValue("password")

	user, err := login(models.EnvFromAdapter(a), username, password)
	if err != nil {
		message := "用户名或密码错误"
		if a.ErrorInResponse {
			message = err.Error()
		}
		// 表单错误直接回到登录页展示，避免返回 500
		return renderPage(a, "login.html", ViewData{
			Title: "登录",
			Error: message,
		})
	}

	// 登录成功：写入签名 cookie session
	if err := middleware.SetSession(a.ResponseWriter, middleware.Session{
		UserID:   int64(user.ID),
		Username: user.Username,
		RoleID:   int64(user.RoleID),
	}); err != nil {
		return fmt.Errorf("TryLogin: %w", err)
	}

	http.Redirect(a.ResponseWriter, r, "/", http.StatusSeeOther)
	return nil
}

// Logout 清除 session 并回到首页
func Logout(a *middleware.Adapter) error {
	middleware.ClearSession(a.ResponseWriter)
	http.Redirect(a.ResponseWriter, a.Request, "/", http.StatusSeeOther)
	return nil
}

func login(env *models.Env, username, password string) (users.User, error) {
	// verify input
	if username == "" || password == "" {
		return users.User{}, errors.New("login: username and password are required")
	}

	// get user from database
	user, err := users.GetUserByUsername(env, username)
	if err != nil {
		return users.User{}, fmt.Errorf("login: %w", err)
	}

	// verify password
	if !verifyPassword(password, user.HashedPassword, user.Salt) {
		return users.User{}, errors.New("login: invalid password")
	}

	return user, nil
}
