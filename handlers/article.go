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

	return renderPage(a, "index.html", ViewData{
		Title:    "首页",
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

	return renderPage(a, "post.html", ViewData{
		Title:     article.Title,
		Article:   article,
		ArticleID: article.ID,
	})
}

// notFound 返回 404 页面
func notFound(a *middleware.Adapter, message string) error {
	a.ResponseWriter.WriteHeader(http.StatusNotFound)
	return renderPage(a, "404.html", ViewData{
		Title: "404",
		Error: message,
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

// ShowEditor 渲染发文表单（需登录）
func ShowEditor(a *middleware.Adapter) error {
	return renderPage(a, "editor.html", ViewData{Title: "写文章"})
}

// CreateArticle 处理发文表单（需登录）
func CreateArticle(a *middleware.Adapter) error {
	r := a.Request

	if err := r.ParseForm(); err != nil {
		return fmt.Errorf("CreateArticle: %w", err)
	}

	session, ok := currentSession(r)
	if !ok {
		// RequireLogin 已挡住，这里只是兜底
		http.Redirect(a.ResponseWriter, r, "/login", http.StatusSeeOther)
		return nil
	}

	title := strings.TrimSpace(r.FormValue("title"))
	content := strings.TrimSpace(r.FormValue("content"))

	if title == "" || content == "" {
		return renderPage(a, "editor.html", ViewData{
			Title:       "写文章",
			Error:       "标题和正文都不能为空",
			FormTitle:   title,
			FormContent: content,
		})
	}
	if utf8.RuneCountInString(title) > 200 {
		return renderPage(a, "editor.html", ViewData{
			Title:       "写文章",
			Error:       "标题不能超过 200 个字符",
			FormTitle:   title,
			FormContent: content,
		})
	}

	env := models.EnvFromAdapter(a)
	tx, err := env.DB.BeginTx(env.Ctx, nil)
	if err != nil {
		return fmt.Errorf("CreateArticle: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(env.Ctx,
		`INSERT INTO articles (title, content) VALUES (?, ?)`, title, content)
	if err != nil {
		return fmt.Errorf("CreateArticle: %w", err)
	}

	articleID, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("CreateArticle: %w", err)
	}

	// articles 表没有作者字段，用 article_revisions 的初版记录保存作者
	_, err = tx.ExecContext(env.Ctx,
		`INSERT INTO article_revisions (article_id, title, content, edited_by) VALUES (?, ?, ?, ?)`,
		articleID, title, content, session.UserID)
	if err != nil {
		return fmt.Errorf("CreateArticle: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("CreateArticle: %w", err)
	}

	http.Redirect(a.ResponseWriter, r, fmt.Sprintf("/post/%d", articleID), http.StatusSeeOther)
	return nil
}
