package handlers

import (
	"context"
	"testing"

	"github.com/AndreHeber/go-sqlite-blog/db"
	"github.com/AndreHeber/go-sqlite-blog/models"
)

func TestParseArticleTags(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int
		wantErr bool
	}{
		{name: "trim and deduplicate", input: " Solana,教程,solana ,,", want: 2},
		{name: "empty", input: " , ", want: 0},
		{name: "too many", input: "a,b,c,d,e,f,g,h,i,j,k", wantErr: true},
		{name: "too long", input: "abcdefghijklmnopqrstuvwxyzabcdefghijklmnop", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, message := parseArticleTags(tt.input)
			if (message != "") != tt.wantErr {
				t.Fatalf("parseArticleTags() error = %q", message)
			}
			if !tt.wantErr && len(got) != tt.want {
				t.Fatalf("got %d tags, want %d", len(got), tt.want)
			}
		})
	}
}

func TestReplaceArticleTagsAndAttach(t *testing.T) {
	testDB, err := db.ConnectDB("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer testDB.Close()
	if _, err := testDB.Exec(`
		CREATE TABLE tags (id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE, slug TEXT NOT NULL UNIQUE);
		CREATE TABLE article_tags (article_id INTEGER NOT NULL, tag_id INTEGER NOT NULL, PRIMARY KEY(article_id, tag_id));
		INSERT INTO tags (name, slug) VALUES ('Old', 'old');
		INSERT INTO article_tags (article_id, tag_id) VALUES (1, 1);
	`); err != nil {
		t.Fatal(err)
	}

	tx, err := testDB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := replaceArticleTags(context.Background(), tx, 1, []string{"Solana", "教程"}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	articles := []models.Article{{ID: 1}}
	env := &models.Env{DB: testDB, Ctx: context.Background()}
	if err := attachArticleTags(env, articles); err != nil {
		t.Fatal(err)
	}
	if len(articles[0].Tags) != 2 || articles[0].Tags[0].Name != "Solana" {
		t.Fatalf("loaded article tags = %#v", articles[0].Tags)
	}
}

func TestArticleTagFilterQueries(t *testing.T) {
	testDB, err := db.ConnectDB("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer testDB.Close()
	if _, err := testDB.Exec(`
		CREATE TABLE articles (id INTEGER PRIMARY KEY, title TEXT, content TEXT, approved BOOLEAN, pinned BOOLEAN NOT NULL DEFAULT 0, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP);
		CREATE TABLE users (id INTEGER PRIMARY KEY, username TEXT);
		CREATE TABLE article_revisions (id INTEGER PRIMARY KEY, article_id INTEGER, edited_by INTEGER);
		CREATE TABLE tags (id INTEGER PRIMARY KEY, name TEXT, slug TEXT);
		CREATE TABLE article_tags (article_id INTEGER, tag_id INTEGER);
		INSERT INTO articles (id, title, content, approved) VALUES (1, 'Published with tag', '', 1), (2, 'Own draft with tag', '', 0), (3, 'Published without tag', '', 1);
		INSERT INTO article_revisions (id, article_id, edited_by) VALUES (1, 1, 7), (2, 2, 7), (3, 3, 8);
		INSERT INTO tags (id, name, slug) VALUES (4, 'Solana', 'solana');
		INSERT INTO article_tags (article_id, tag_id) VALUES (1, 4), (2, 4);
	`); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		base string
		args []any
		want []int64
	}{
		{name: "administrator list includes tagged pending articles", base: listArticlesQuery, want: []int64{2, 1}},
		{name: "public list excludes pending articles", base: listPublishedArticlesQuery, want: []int64{1}},
		{name: "author can see own tagged pending article", base: listUserArticlesQuery, args: []any{int64(7)}, want: []int64{2, 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append(append([]any(nil), tt.args...), int64(4))
			rows, err := testDB.Query(addTagFilter(tt.base), args...)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()

			var got []int64
			for rows.Next() {
				var id int64
				var title, content, createdAt, author string
				var approved, pinned bool
				if err := rows.Scan(&id, &title, &content, &createdAt, &author, &approved, &pinned); err != nil {
					t.Fatal(err)
				}
				got = append(got, id)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got article IDs %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("got article IDs %v, want %v", got, tt.want)
				}
			}
		})
	}
}
