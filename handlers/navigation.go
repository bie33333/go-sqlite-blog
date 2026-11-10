package handlers

import (
	"fmt"

	"github.com/AndreHeber/go-sqlite-blog/models"
)

func loadNavigation(env *models.Env) ([]models.Tag, []models.Page, error) {
	tagRows, err := env.DB.QueryContext(env.Ctx, `
		SELECT DISTINCT t.id, t.name, t.slug
		FROM tags t
		JOIN article_tags at ON at.tag_id = t.id
		JOIN articles a ON a.id = at.article_id AND a.approved = 1
		ORDER BY t.name COLLATE NOCASE`)
	if err != nil {
		return nil, nil, fmt.Errorf("load navigation tags: %w", err)
	}
	tags := make([]models.Tag, 0)
	for tagRows.Next() {
		var tag models.Tag
		if err := tagRows.Scan(&tag.ID, &tag.Name, &tag.Slug); err != nil {
			tagRows.Close()
			return nil, nil, fmt.Errorf("load navigation tags: %w", err)
		}
		tags = append(tags, tag)
	}
	if err := tagRows.Err(); err != nil {
		tagRows.Close()
		return nil, nil, fmt.Errorf("load navigation tags: %w", err)
	}
	if err := tagRows.Close(); err != nil {
		return nil, nil, fmt.Errorf("load navigation tags: %w", err)
	}

	pageRows, err := env.DB.QueryContext(env.Ctx, `
		SELECT title, slug
		FROM pages
		WHERE status = 'published'
		ORDER BY title COLLATE NOCASE`)
	if err != nil {
		return nil, nil, fmt.Errorf("load navigation pages: %w", err)
	}
	pages := make([]models.Page, 0)
	for pageRows.Next() {
		var page models.Page
		if err := pageRows.Scan(&page.Title, &page.Slug); err != nil {
			pageRows.Close()
			return nil, nil, fmt.Errorf("load navigation pages: %w", err)
		}
		pages = append(pages, page)
	}
	if err := pageRows.Err(); err != nil {
		pageRows.Close()
		return nil, nil, fmt.Errorf("load navigation pages: %w", err)
	}
	if err := pageRows.Close(); err != nil {
		return nil, nil, fmt.Errorf("load navigation pages: %w", err)
	}
	return tags, pages, nil
}
