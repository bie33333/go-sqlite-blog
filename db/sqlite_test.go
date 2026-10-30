package db

import "testing"

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
