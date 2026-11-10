package db

import (
	"database/sql"
	"testing"
)

func TestMigrateArticleApprovalPreservesExistingArticles(t *testing.T) {
	testDB, err := ConnectDB("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer testDB.Close()

	if _, err := testDB.Exec(`
		CREATE TABLE articles (
			id INTEGER PRIMARY KEY,
			title TEXT NOT NULL,
			content TEXT NOT NULL
		);
		INSERT INTO articles (title, content) VALUES ('Existing post', 'Existing content');
	`); err != nil {
		t.Fatal(err)
	}

	if err := migrateArticleApproval(testDB); err != nil {
		t.Fatal(err)
	}
	if err := migrateArticleApproval(testDB); err != nil {
		t.Fatalf("migration is not idempotent: %v", err)
	}

	var approved bool
	if err := testDB.QueryRow(`SELECT approved FROM articles WHERE id = 1`).Scan(&approved); err != nil {
		t.Fatal(err)
	}
	if !approved {
		t.Fatal("existing articles should remain published after migration")
	}
}

func TestMigrateStaticPagesPreservesExistingPagesAndIsIdempotent(t *testing.T) {
	testDB, err := ConnectDB("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer testDB.Close()

	if _, err := testDB.Exec(`
		CREATE TABLE pages (
			id INTEGER PRIMARY KEY,
			title TEXT NOT NULL,
			slug TEXT NOT NULL UNIQUE,
			content TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP
		);
		INSERT INTO pages (title, slug, content) VALUES ('Tutorial', 'tutorial', '<h1>Hi</h1>');
		CREATE TABLE users (id INTEGER PRIMARY KEY);
	`); err != nil {
		t.Fatal(err)
	}

	if err := migrateStaticPages(testDB); err != nil {
		t.Fatal(err)
	}
	if err := migrateStaticPages(testDB); err != nil {
		t.Fatalf("migration is not idempotent: %v", err)
	}

	var title, slug, content, status string
	if err := testDB.QueryRow(`SELECT title, slug, content, status FROM pages WHERE id = 1`).
		Scan(&title, &slug, &content, &status); err != nil {
		t.Fatal(err)
	}
	if title != "Tutorial" || slug != "tutorial" || content != "<h1>Hi</h1>" || status != "published" {
		t.Fatalf("existing page changed unexpectedly: %q, %q, %q, %q", title, slug, content, status)
	}
}

func TestMigrateArticlePinningAndStaticPageScripts(t *testing.T) {
	testDB, err := ConnectDB("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer testDB.Close()

	if _, err := testDB.Exec(`
		CREATE TABLE articles (id INTEGER PRIMARY KEY, title TEXT NOT NULL, content TEXT NOT NULL);
		CREATE TABLE pages (id INTEGER PRIMARY KEY, title TEXT NOT NULL, slug TEXT NOT NULL, content TEXT NOT NULL);
		INSERT INTO articles (title, content) VALUES ('Existing', 'Post');
		INSERT INTO pages (title, slug, content) VALUES ('Existing', 'existing', '<h1>Page</h1>');
	`); err != nil {
		t.Fatal(err)
	}

	for _, migrate := range []func(*sql.DB) error{migrateArticlePinning, migrateStaticPageScripts} {
		if err := migrate(testDB); err != nil {
			t.Fatal(err)
		}
		if err := migrate(testDB); err != nil {
			t.Fatalf("migration is not idempotent: %v", err)
		}
	}

	var pinned, allowScripts bool
	if err := testDB.QueryRow(`SELECT pinned FROM articles WHERE id = 1`).Scan(&pinned); err != nil {
		t.Fatal(err)
	}
	if err := testDB.QueryRow(`SELECT allow_scripts FROM pages WHERE id = 1`).Scan(&allowScripts); err != nil {
		t.Fatal(err)
	}
	if pinned || allowScripts {
		t.Fatalf("existing rows should use secure defaults: pinned=%v allow_scripts=%v", pinned, allowScripts)
	}
}
