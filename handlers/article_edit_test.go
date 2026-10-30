package handlers

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/AndreHeber/go-sqlite-blog/db"
	"github.com/AndreHeber/go-sqlite-blog/middleware"
)

func TestArticleEditingPermissionsAndUpdates(t *testing.T) {
	testDB, err := db.ConnectDB("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer testDB.Close()
	testDB.SetMaxOpenConns(1)

	_, err = testDB.Exec(`
		CREATE TABLE users (id INTEGER PRIMARY KEY, username TEXT, role_id INTEGER);
		CREATE TABLE articles (id INTEGER PRIMARY KEY, title TEXT NOT NULL, content TEXT NOT NULL, approved BOOLEAN NOT NULL DEFAULT 1, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP);
		CREATE TABLE article_revisions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			article_id INTEGER NOT NULL,
			title TEXT NOT NULL,
			content TEXT NOT NULL,
			edited_by INTEGER NOT NULL,
			edited_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE comments (
			id INTEGER PRIMARY KEY,
			article_id INTEGER NOT NULL,
			user_id INTEGER,
			author_name TEXT,
			content TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			approved BOOLEAN DEFAULT 0
		);
		INSERT INTO users (id, username, role_id) VALUES (1, 'author', 3), (2, 'other', 3), (3, 'admin', 1);
		INSERT INTO articles (id, title, content) VALUES (1, 'Original title', 'Original content');
		INSERT INTO article_revisions (article_id, title, content, edited_by)
		VALUES (1, 'Original title', 'Original content', 1);
	`)
	if err != nil {
		t.Fatal(err)
	}

	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	projectRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(workingDirectory); err != nil {
			t.Error(err)
		}
	}()

	t.Run("author can open edit form", func(t *testing.T) {
		response := serveEditTestRequest(t, testDB, 1, http.MethodGet, "/post/1/edit", "")
		if response.Code != http.StatusOK {
			t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
		}
		if !strings.Contains(response.Body.String(), "Original title") {
			t.Fatal("edit form did not contain the existing article title")
		}
	})

	t.Run("another author cannot edit", func(t *testing.T) {
		response := serveEditTestRequest(t, testDB, 2, http.MethodPost, "/post/1/edit", "title=Changed&content=Changed")
		if response.Code != http.StatusForbidden {
			t.Fatalf("expected status %d, got %d", http.StatusForbidden, response.Code)
		}

		var title string
		if err := testDB.QueryRow(`SELECT title FROM articles WHERE id = 1`).Scan(&title); err != nil {
			t.Fatal(err)
		}
		if title != "Original title" {
			t.Fatalf("unauthorized edit changed article title to %q", title)
		}
	})

	t.Run("author can update and admin can also update", func(t *testing.T) {
		for _, userID := range []int64{1, 3} {
			response := serveEditTestRequest(t, testDB, userID, http.MethodPost, "/post/1/edit",
				url.Values{"title": {"Updated by user"}, "content": {"Updated content"}}.Encode())
			if response.Code != http.StatusSeeOther {
				t.Fatalf("user %d: expected status %d, got %d", userID, http.StatusSeeOther, response.Code)
			}
		}

		var title, content string
		var revisionCount int
		if err := testDB.QueryRow(`SELECT title, content FROM articles WHERE id = 1`).Scan(&title, &content); err != nil {
			t.Fatal(err)
		}
		if err := testDB.QueryRow(`SELECT COUNT(*) FROM article_revisions WHERE article_id = 1`).Scan(&revisionCount); err != nil {
			t.Fatal(err)
		}
		if title != "Updated by user" || content != "Updated content" {
			t.Fatalf("saved article = (%q, %q)", title, content)
		}
		if revisionCount != 3 {
			t.Fatalf("expected 3 revisions, got %d", revisionCount)
		}
	})

	t.Run("ordinary articles stay pending until admin approval", func(t *testing.T) {
		form := url.Values{"title": {"Pending article"}, "content": {"Review me"}}.Encode()
		response := serveEditTestRequest(t, testDB, 1, http.MethodPost, "/post", form)
		if response.Code != http.StatusSeeOther {
			t.Fatalf("expected status %d, got %d", http.StatusSeeOther, response.Code)
		}

		var articleID int64
		var approved bool
		if err := testDB.QueryRow(`SELECT id, approved FROM articles WHERE title = 'Pending article'`).Scan(&articleID, &approved); err != nil {
			t.Fatal(err)
		}
		if approved {
			t.Fatal("ordinary user's new article should not be published")
		}

		response = serveEditTestRequest(t, testDB, 2, http.MethodGet, "/post/"+strconv.FormatInt(articleID, 10), "")
		if response.Code != http.StatusNotFound {
			t.Fatalf("unrelated user should not see pending article: got status %d", response.Code)
		}
		response = serveEditTestRequest(t, testDB, 1, http.MethodGet, "/post/"+strconv.FormatInt(articleID, 10), "")
		if response.Code != http.StatusOK {
			t.Fatalf("author should be able to preview pending article: got status %d", response.Code)
		}

		response = serveEditTestRequest(t, testDB, 3, http.MethodPost,
			"/admin/articles/"+strconv.FormatInt(articleID, 10)+"/approve", "")
		if response.Code != http.StatusSeeOther {
			t.Fatalf("admin approval returned status %d", response.Code)
		}
		if err := testDB.QueryRow(`SELECT approved FROM articles WHERE id = ?`, articleID).Scan(&approved); err != nil {
			t.Fatal(err)
		}
		if !approved {
			t.Fatal("admin approval should publish the article")
		}

		response = serveEditTestRequest(t, testDB, 3, http.MethodPost, "/post", form)
		if response.Code != http.StatusSeeOther {
			t.Fatalf("admin article submission returned status %d", response.Code)
		}
		if err := testDB.QueryRow(`SELECT approved FROM articles WHERE title = 'Pending article' ORDER BY id DESC LIMIT 1`).Scan(&approved); err != nil {
			t.Fatal(err)
		}
		if !approved {
			t.Fatal("admin-created articles should publish immediately")
		}
	})
}

func serveEditTestRequest(t *testing.T, db *sql.DB, userID int64, method, path, form string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(method, path, strings.NewReader(form))
	pathParts := strings.Split(strings.Trim(path, "/"), "/")
	if len(pathParts) >= 2 && pathParts[0] == "post" {
		if pathParts[1] != "new" {
			request.SetPathValue("id", pathParts[1])
		}
	} else if len(pathParts) >= 3 && pathParts[0] == "admin" && pathParts[1] == "articles" {
		request.SetPathValue("id", pathParts[2])
	}
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	cookieRecorder := httptest.NewRecorder()
	if err := middleware.SetSession(cookieRecorder, middleware.Session{UserID: userID}); err != nil {
		t.Fatal(err)
	}
	request.AddCookie(cookieRecorder.Result().Cookies()[0])

	response := httptest.NewRecorder()
	adapter := &middleware.Adapter{
		Request:        request,
		ResponseWriter: response,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		DB:             db,
		Ctx:            context.Background(),
	}
	var handler func(*middleware.Adapter) error
	switch {
	case path == "/post":
		handler = CreateArticle
	case strings.HasPrefix(path, "/admin/articles/"):
		handler = ApproveArticle
	case method == http.MethodGet && strings.HasSuffix(path, "/edit"):
		handler = ShowEditArticle
	case method == http.MethodGet:
		handler = ShowArticle
	default:
		handler = UpdateArticle
	}
	if err := handler(adapter); err != nil {
		t.Fatalf("%s returned an error: %v", method, err)
	}
	return response
}
