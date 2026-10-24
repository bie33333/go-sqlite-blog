package handlers

import (
	"net/http"

	"github.com/AndreHeber/go-sqlite-blog/middleware"
)

// isLoggedIn 用于模板中判断是否显示登录态导航。
func isLoggedIn(r *http.Request) bool {
	_, ok := middleware.GetSession(r)
	return ok
}

// currentSession 返回当前登录用户信息。
func currentSession(r *http.Request) (middleware.Session, bool) {
	return middleware.GetSession(r)
}
