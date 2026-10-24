package handlers

import (
	"errors"
	"fmt"
	"time"

	"github.com/AndreHeber/go-sqlite-blog/middleware"
	"github.com/AndreHeber/go-sqlite-blog/models"
	"github.com/AndreHeber/go-sqlite-blog/models/users"
)

// ShowRegister renders the register page
func ShowRegister(a *middleware.Adapter) error {
	return renderPage(a, "register.html", ViewData{Title: "注册"})
}

// TryRegister handles the registration form submission
func TryRegister(a *middleware.Adapter) error {
	r := a.Request

	err := r.ParseForm()
	if err != nil {
		return fmt.Errorf("TryRegister: %w", err)
	}
	username := r.FormValue("username")
	password := r.FormValue("password")
	email := r.FormValue("email")

	if err := register(models.EnvFromAdapter(a), username, password, email); err != nil {
		message := "注册失败，请稍后重试"
		if a.ErrorInResponse {
			message = err.Error()
		}
		// 表单错误直接回到注册页展示，避免返回 500
		return renderPage(a, "register.html", ViewData{
			Title: "注册",
			Error: message,
		})
	}

	return renderPage(a, "register.html", ViewData{
		Title:   "注册",
		Message: "注册成功，请前往登录",
	})
}

// register saves the user to the database, password is hashed before saving using bcrypt
// also an email is sent to the user with a link to verify their account
func register(env *models.Env, username, password, email string) error {
	// verify input
	if username == "" || password == "" || email == "" {
		return errors.New("register: username, password and email are required")
	}

	encodedHash, encodedSalt := hashPassword(password, 16)

	// save user to database
	user := users.User{
		Username:       username,
		HashedPassword: encodedHash,
		Salt:           encodedSalt,
		Email:          email,
		Verified:       false,
		RoleID:         1,
		CreatedAt:      time.Now(),
		LastLogin:      time.Now(),
	}

	err := users.CreateUser(env, user)
	if err != nil {
		return fmt.Errorf("register: %w", err)
	}

	return nil
}
