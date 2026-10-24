package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/AndreHeber/go-sqlite-blog/middleware"
	"github.com/AndreHeber/go-sqlite-blog/models"
)

// articles 表本身没有作者字段，作者信息取自 article_revisions 中最早的一条记录
// （创建文章时写入的初版），这样无需改动现有表结构。
const listArticlesQuery = `
SELECT a.id, a.title, a.content,
       strftime('%Y-%m-%d %H:%M', a.created_at),
       COALESCE(u.username, '')
FROM articles a
LEFT JOIN article_revisions r
       ON r.id = (SELECT MIN(id) FROM article_revisions WHERE article_id = a.id)
LEFT JOIN users u ON u.id = r.edited_by
ORDER BY a.created_at DESC, a.id DESC`

const getArticleQuery = `
SELECT a.id, a.title, a.content,
       strftime('%Y-%m-%d %H:%M', a.created_at),
       COALESCE(u.username, '')
FROM articles a
LEFT JOIN article_revisions r
       ON r.id = (SELECT MIN(id) FROM article_revisions WHERE article_id = a.id)
LEFT JOIN users u ON u.id = r.edited_by
WHERE a.id = ?`

// ListArticles 渲染首页文章列表
func ListArticles(a *middleware.Adapter) error {
	w := a.ResponseWriter
	if a.Request.URL.Path != "/" {
		return notFound(a, "页面不存在")
	}

	env := models.EnvFromAdapter(a)
	rows, err := env.DB.QueryContext(env.Ctx, listArticlesQuery)
	if err != nil {
		return fmt.Errorf("ListArticles: %w", err)
	}
	defer rows.Close()

	articles := make([]models.Article, 0)
	for rows.Next() {
		var article models.Article
		if err := rows.Scan(&article.ID, &article.Title, &article.Content, &article.CreatedAt, &article.Author); err != nil {
			return fmt.Errorf("ListArticles: %w", err)
		}
		article.Excerpt = excerpt(article.Content, 160)
		articles = append(articles, article)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("ListArticles: %w", err)
	}

	return renderPage(w, "index.html", ViewData{
		Title:    "首页",
		LoggedIn: isLoggedIn(a.Request),
		Articles: articles,
	})
}

// ShowArticle 渲染文章详情
func ShowArticle(a *middleware.Adapter) error {
	id, err := strconv.ParseInt(a.Request.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return notFound(a, "文章不存在")
	}

	env := models.EnvFromAdapter(a)
	var article models.Article
	err = env.DB.QueryRowContext(env.Ctx, getArticleQuery, id).
		Scan(&article.ID, &article.Title, &article.Content, &article.CreatedAt, &article.Author)
	if err == sql.ErrNoRows {
		return notFound(a, "文章不存在")
	}
	if err != nil {
		return fmt.Errorf("ShowArticle: %w", err)
	}

	return renderPage(a.ResponseWriter, "post.html", ViewData{
		Title:     article.Title,
		LoggedIn:  isLoggedIn(a.Request),
		Article:   article,
		ArticleID: article.ID,
	})
}

// notFound 返回 404 页面
func notFound(a *middleware.Adapter, message string) error {
	a.ResponseWriter.WriteHeader(http.StatusNotFound)
	return renderPage(a.ResponseWriter, "404.html", ViewData{
		Title:    "404",
		LoggedIn: isLoggedIn(a.Request),
		Error:    message,
	})
}

// excerpt 生成列表页摘要
func excerpt(content string, limit int) string {
	text := strings.Join(strings.Fields(content), " ")
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit]) + "…"
}
