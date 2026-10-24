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
	return renderPage(a.ResponseWriter, "login.html", ViewData{Title: "登录"})
}

// TryLogin handles the login form submission
func TryLogin(a *middleware.Adapter) error {
	r := a.Request

	if err := r.ParseForm(); err != nil {
		return fmt.Errorf("TryLogin: %w", err)
	}
	username := r.FormValue("username")
	password := r.FormValue("password")

	if err := login(models.EnvFromAdapter(a), username, password); err != nil {
		message := "用户名或密码错误"
		if a.ErrorInResponse {
			message = err.Error()
		}
		// 表单错误直接回到登录页展示，避免返回 500
		return renderPage(a.ResponseWriter, "login.html", ViewData{
			Title: "登录",
			Error: message,
		})
	}

	http.Redirect(a.ResponseWriter, r, "/", http.StatusSeeOther)
	return nil
}

func login(env *models.Env, username, password string) error {
	// verify input
	if username == "" || password == "" {
		return errors.New("login: username and password are required")
	}

	// get user from database
	user, err := users.GetUserByUsername(env, username)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}

	// verify password
	if !verifyPassword(password, user.HashedPassword, user.Salt) {
		return errors.New("login: invalid password")
	}

	return nil
}
