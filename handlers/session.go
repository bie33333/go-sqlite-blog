package handlers

import (
	"net/http"

	"github.com/AndreHeber/go-sqlite-blog/middleware"
)

// currentSession 返回当前登录用户信息。
func currentSession(r *http.Request) (middleware.Session, bool) {
	return middleware.GetSession(r)
}
