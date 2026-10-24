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

const listCommentsQuery = `
SELECT id, article_id, user_id, COALESCE(author_name, ''), content,
       strftime('%Y-%m-%d %H:%M', created_at), approved
FROM comments
WHERE article_id = ?
ORDER BY created_at ASC, id ASC`

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
	article, err := getArticle(env, id)
	if err == sql.ErrNoRows {
		return notFound(a, "文章不存在")
	}
	if err != nil {
		return fmt.Errorf("ShowArticle: %w", err)
	}

	comments, err := listComments(env, id)
	if err != nil {
		return fmt.Errorf("ShowArticle: %w", err)
	}

	return renderPostPage(a, article, comments, "")
}

// getArticle 查询单篇文章
func getArticle(env *models.Env, id int64) (models.Article, error) {
	var article models.Article
	err := env.DB.QueryRowContext(env.Ctx, getArticleQuery, id).
		Scan(&article.ID, &article.Title, &article.Content, &article.CreatedAt, &article.Author)
	return article, err
}

// listComments 查询文章下的评论
func listComments(env *models.Env, articleID int64) ([]models.Comment, error) {
	rows, err := env.DB.QueryContext(env.Ctx, listCommentsQuery, articleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	comments := make([]models.Comment, 0)
	for rows.Next() {
		var (
			comment models.Comment
			userID  sql.NullInt64
		)
		if err := rows.Scan(&comment.ID, &comment.ArticleID, &userID, &comment.AuthorName,
			&comment.Content, &comment.CreatedAt, &comment.Approved); err != nil {
			return nil, err
		}
		if userID.Valid {
			comment.UserID = &userID.Int64
		}
		comments = append(comments, comment)
	}
	return comments, rows.Err()
}

// renderPostPage 渲染详情页（含评论列表与错误提示）
func renderPostPage(a *middleware.Adapter, article models.Article, comments []models.Comment, message string) error {
	return renderPage(a, "post.html", ViewData{
		Title:     article.Title,
		Article:   article,
		ArticleID: article.ID,
		Comments:  comments,
		Error:     message,
	})
}

// CreateComment 提交评论（需登录）
func CreateComment(a *middleware.Adapter) error {
	r := a.Request

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return notFound(a, "文章不存在")
	}

	session, ok := currentSession(r)
	if !ok {
		http.Redirect(a.ResponseWriter, r, "/login", http.StatusSeeOther)
		return nil
	}

	env := models.EnvFromAdapter(a)
	article, err := getArticle(env, id)
	if err == sql.ErrNoRows {
		return notFound(a, "文章不存在")
	}
	if err != nil {
		return fmt.Errorf("CreateComment: %w", err)
	}

	content := strings.TrimSpace(r.FormValue("content"))
	if content == "" {
		comments, err := listComments(env, id)
		if err != nil {
			return fmt.Errorf("CreateComment: %w", err)
		}
		return renderPostPage(a, article, comments, "评论内容不能为空")
	}

	// comments.approved 默认 0（待审核），这里显式写入待审核状态
	_, err = env.DB.ExecContext(env.Ctx,
		`INSERT INTO comments (article_id, user_id, author_name, content, approved) VALUES (?, ?, ?, ?, 0)`,
		id, session.UserID, session.Username, content)
	if err != nil {
		return fmt.Errorf("CreateComment: %w", err)
	}

	http.Redirect(a.ResponseWriter, r, fmt.Sprintf("/post/%d#comments", id), http.StatusSeeOther)
	return nil
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
