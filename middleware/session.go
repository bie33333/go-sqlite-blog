package middleware

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// 使用标准库实现的签名 cookie session，避免引入额外依赖。
const (
	sessionCookieName = "blog_session"
	sessionMaxAge     = 7 * 24 * time.Hour
)

// Session 是登录后保存在 cookie 中的用户信息。
type Session struct {
	UserID    int64  `json:"user_id"`
	Username  string `json:"username"`
	RoleID    int64  `json:"role_id"`
	CSRFToken string `json:"csrf_token,omitempty"`
	Expires   int64  `json:"exp"`
}

var sessionKey = loadSessionKey()

func loadSessionKey() []byte {
	if secret := os.Getenv("SESSION_SECRET"); secret != "" {
		return []byte(secret)
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic(err)
	}
	slog.Warn("middleware: SESSION_SECRET is not set, using a random key; sessions will be invalid after restart")
	return key
}

func sign(payload []byte) string {
	mac := hmac.New(sha256.New, sessionKey)
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// SetSession 把登录信息写入签名 cookie。
func SetSession(w http.ResponseWriter, session Session) error {
	session.Expires = time.Now().Add(sessionMaxAge).Unix()

	raw, err := json.Marshal(session)
	if err != nil {
		return err
	}

	encoded := base64.RawURLEncoding.EncodeToString(raw)
	value := encoded + "." + sign([]byte(encoded))

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionMaxAge.Seconds()),
	})
	return nil
}

// NewCSRFToken returns a cryptographically random token for forms bound to a session.
func NewCSRFToken() (string, error) {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(token), nil
}

// GetSession 从请求中解析并校验 session。
func GetSession(r *http.Request) (Session, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return Session{}, false
	}

	encoded, signature, ok := strings.Cut(cookie.Value, ".")
	if !ok {
		return Session{}, false
	}

	if !hmac.Equal([]byte(signature), []byte(sign([]byte(encoded)))) {
		return Session{}, false
	}

	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return Session{}, false
	}

	var session Session
	if err := json.Unmarshal(raw, &session); err != nil {
		return Session{}, false
	}

	if session.UserID <= 0 || time.Now().Unix() > session.Expires {
		return Session{}, false
	}

	return session, true
}

// ClearSession 删除 session cookie。
func ClearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// RequireLogin 包装需要登录的 handler：未登录时 302 跳转到 /login。
func RequireLogin(next func(*Adapter) error) func(*Adapter) error {
	return func(a *Adapter) error {
		if _, ok := GetSession(a.Request); !ok {
			http.Redirect(a.ResponseWriter, a.Request, "/login", http.StatusSeeOther)
			return nil
		}
		return next(a)
	}
}
