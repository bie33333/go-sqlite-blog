package main

import (
	"bytes"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	dbService "github.com/AndreHeber/go-sqlite-blog/db"
	"github.com/AndreHeber/go-sqlite-blog/middleware"
	"golang.org/x/time/rate"
)

func TestCustomHTMLPageLifecycle(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pages.db")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := dbService.Init(logger, "sqlite3", dbPath, "./tables.sql")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO users (username, password_hash, salt, email, role_id) VALUES ('admin', 'x', 'x', 'admin@example.test', 1)`); err != nil {
		t.Fatal(err)
	}

	adapter := middleware.Init(logger, db, false, false, rate.Limit(100), 100)
	server := httptest.NewServer(setupRouter(adapter))
	defer server.Close()
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	csrfToken, err := middleware.NewCSRFToken()
	if err != nil {
		t.Fatal(err)
	}
	cookieRecorder := httptest.NewRecorder()
	if err := middleware.SetSession(cookieRecorder, middleware.Session{
		UserID: 1, Username: "admin", RoleID: 1, CSRFToken: csrfToken,
	}); err != nil {
		t.Fatal(err)
	}
	sessionCookie := cookieRecorder.Result().Cookies()[0]

	html := `<!DOCTYPE html><html lang="zh-CN"><head><meta charset="UTF-8"><style>body{background:#0d1117;color:#e6edf3}</style><title>教程</title></head><body><h1>教程</h1><nav><a href="#section">跳转</a></nav><table><tr><th>标题</th></tr><tr><td>内容</td></tr></table><h2 id="section">章节</h2><script>window.pwned = true</script></body></html>`
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range map[string]string{
		"title": "教程", "slug": "tutorial", "csrf_token": csrfToken, "content": html,
	} {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	request, err := http.NewRequest(http.MethodPost, server.URL+"/admin/pages", &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.AddCookie(sessionCookie)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	responseBody, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != "/admin/pages" {
		bodyText := string(responseBody)
		start := strings.Index(bodyText, `class="alert error"`)
		if start >= 0 {
			end := strings.Index(bodyText[start:], "</div>")
			if end >= 0 {
				bodyText = bodyText[start : start+end+6]
			}
		}
		t.Fatalf("create page: status=%d location=%q message=%s", response.StatusCode, response.Header.Get("Location"), bodyText)
	}

	anonymousClient := client
	response, err = anonymousClient.Get(server.URL + "/p/tutorial")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("draft should not be public, got status %d", response.StatusCode)
	}

	request, err = http.NewRequest(http.MethodGet, server.URL+"/admin/pages/1/preview", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(sessionCookie)
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || string(preview) != html {
		t.Fatalf("preview did not return the original HTML: status=%d body=%q", response.StatusCode, preview)
	}
	if !strings.Contains(response.Header.Get("Content-Security-Policy"), "sandbox") ||
		!strings.Contains(response.Header.Get("Content-Security-Policy"), "style-src 'unsafe-inline'") {
		t.Fatalf("expected isolated document CSP, got %q", response.Header.Get("Content-Security-Policy"))
	}

	request, err = http.NewRequest(http.MethodGet, server.URL+"/admin/pages/999/preview", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(sessionCookie)
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("preview of missing page should return 404, got %d", response.StatusCode)
	}

	form := url.Values{"csrf_token": {csrfToken}}
	request, err = http.NewRequest(http.MethodPost, server.URL+"/admin/pages/1/publish", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(sessionCookie)
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("publish page: status=%d", response.StatusCode)
	}

	response, err = client.Get(server.URL + "/p/tutorial")
	if err != nil {
		t.Fatal(err)
	}
	published, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || string(published) != html {
		t.Fatalf("published page did not return original HTML: status=%d", response.StatusCode)
	}
	if response.Header.Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("unexpected page content type: %q", response.Header.Get("Content-Type"))
	}

	fileHTML := `<html><head><style>body{color:red}</style></head><body>uploaded file</body></html>`
	var uploadBody bytes.Buffer
	uploadWriter := multipart.NewWriter(&uploadBody)
	for key, value := range map[string]string{
		"title": "文件页面", "slug": "uploaded-file", "csrf_token": csrfToken,
	} {
		if err := uploadWriter.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	filePart, err := uploadWriter.CreateFormFile("html_file", "tutorial.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := filePart.Write([]byte(fileHTML)); err != nil {
		t.Fatal(err)
	}
	if err := uploadWriter.Close(); err != nil {
		t.Fatal(err)
	}
	request, err = http.NewRequest(http.MethodPost, server.URL+"/admin/pages", &uploadBody)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", uploadWriter.FormDataContentType())
	request.AddCookie(sessionCookie)
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("upload HTML file: status=%d", response.StatusCode)
	}
	var storedHTML string
	if err := db.QueryRow(`SELECT content FROM pages WHERE slug = 'uploaded-file'`).Scan(&storedHTML); err != nil {
		t.Fatal(err)
	}
	if storedHTML != fileHTML {
		t.Fatalf("uploaded HTML changed: got %q", storedHTML)
	}
}

func TestCreateCustomHTMLPageRejectsMissingCSRF(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pages.db")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := dbService.Init(logger, "sqlite3", dbPath, "./tables.sql")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO users (username, password_hash, salt, email, role_id) VALUES ('admin', 'x', 'x', 'admin@example.test', 1)`); err != nil {
		t.Fatal(err)
	}

	adapter := middleware.Init(logger, db, false, false, rate.Limit(100), 100)
	server := httptest.NewServer(setupRouter(adapter))
	defer server.Close()
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	cookieRecorder := httptest.NewRecorder()
	if err := middleware.SetSession(cookieRecorder, middleware.Session{UserID: 1, Username: "admin", RoleID: 1}); err != nil {
		t.Fatal(err)
	}
	cookie := cookieRecorder.Result().Cookies()[0]

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("title", "Test")
	_ = writer.WriteField("slug", "test")
	_ = writer.WriteField("content", "<h1>Test</h1>")
	_ = writer.Close()
	request, err := http.NewRequest(http.MethodPost, server.URL+"/admin/pages", &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.AddCookie(cookie)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("missing CSRF token should be forbidden, got %d", response.StatusCode)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pages`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("CSRF failure created %d pages", count)
	}
}
