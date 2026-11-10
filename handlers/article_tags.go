package handlers

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/AndreHeber/go-sqlite-blog/models"
)

const (
	maxArticleTags     = 10
	maxArticleTagRunes = 40
)

func parseArticleTags(raw string) ([]string, string) {
	if utf8.RuneCountInString(raw) > 500 {
		return nil, "标签总长度不能超过 500 个字符"
	}

	seen := make(map[string]struct{})
	tags := make([]string, 0)
	for _, part := range strings.Split(raw, ",") {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		if utf8.RuneCountInString(name) > maxArticleTagRunes {
			return nil, "每个标签不能超过 40 个字符"
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		tags = append(tags, name)
		if len(tags) > maxArticleTags {
			return nil, "每篇文章最多添加 10 个标签"
		}
	}
	return tags, ""
}

func replaceArticleTags(ctx context.Context, tx *sql.Tx, articleID int64, names []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM article_tags WHERE article_id = ?`, articleID); err != nil {
		return err
	}
	for _, name := range names {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO tags (name, slug) VALUES (?, ?) ON CONFLICT(name) DO NOTHING`,
			name, articleTagSlug(name)); err != nil {
			return err
		}

		var tagID int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM tags WHERE name = ?`, name).Scan(&tagID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO article_tags (article_id, tag_id) VALUES (?, ?)`, articleID, tagID); err != nil {
			return err
		}
	}
	return nil
}

func articleTagSlug(name string) string {
	var slug strings.Builder
	lastHyphen := false
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			slug.WriteRune(r)
			lastHyphen = false
		} else if !lastHyphen && slug.Len() > 0 {
			slug.WriteByte('-')
			lastHyphen = true
		}
	}
	base := strings.Trim(slug.String(), "-")
	if base == "" {
		base = "tag"
	}
	hash := sha256.Sum256([]byte(name))
	return fmt.Sprintf("%s-%s", base, hex.EncodeToString(hash[:6]))
}

func attachArticleTags(env *models.Env, articles []models.Article) error {
	if len(articles) == 0 {
		return nil
	}

	indexes := make(map[int64]int, len(articles))
	for i := range articles {
		articles[i].Tags = nil
		indexes[articles[i].ID] = i
	}

	// Keep each IN clause below SQLite's usual bound-variable limit.
	for start := 0; start < len(articles); start += 900 {
		end := start + 900
		if end > len(articles) {
			end = len(articles)
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?,", end-start), ",")
		args := make([]any, 0, end-start)
		for i := start; i < end; i++ {
			args = append(args, articles[i].ID)
		}

		rows, err := env.DB.QueryContext(env.Ctx, `
			SELECT at.article_id, t.id, t.name, t.slug
			FROM article_tags at
			JOIN tags t ON t.id = at.tag_id
			WHERE at.article_id IN (`+placeholders+`)
			ORDER BY t.name COLLATE NOCASE`, args...)
		if err != nil {
			return err
		}

		for rows.Next() {
			var articleID int64
			var tag models.Tag
			if err := rows.Scan(&articleID, &tag.ID, &tag.Name, &tag.Slug); err != nil {
				rows.Close()
				return err
			}
			if i, ok := indexes[articleID]; ok {
				articles[i].Tags = append(articles[i].Tags, tag)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}
	return nil
}

func tagNames(tags []models.Tag) string {
	names := make([]string, 0, len(tags))
	for _, tag := range tags {
		names = append(names, tag.Name)
	}
	return strings.Join(names, ", ")
}
