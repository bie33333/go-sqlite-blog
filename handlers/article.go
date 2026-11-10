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
	"github.com/AndreHeber/go-sqlite-blog/models/users"
)

// articles 表的作者信息取自 article_revisions 中最早的一条记录。
const listArticlesQuery = `
SELECT a.id, a.title, a.content,
       strftime('%Y-%m-%d %H:%M', a.created_at),
       COALESCE(u.username, ''), a.approved, a.pinned
FROM articles a
LEFT JOIN article_revisions r
       ON r.id = (SELECT MIN(id) FROM article_revisions WHERE article_id = a.id)
LEFT JOIN users u ON u.id = r.edited_by
WHERE 1 = 1
ORDER BY a.pinned DESC, a.created_at DESC, a.id DESC`

const listPublishedArticlesQuery = `
SELECT a.id, a.title, a.content,
       strftime('%Y-%m-%d %H:%M', a.created_at),
       COALESCE(u.username, ''), a.approved, a.pinned
FROM articles a
LEFT JOIN article_revisions r
       ON r.id = (SELECT MIN(id) FROM article_revisions WHERE article_id = a.id)
LEFT JOIN users u ON u.id = r.edited_by
WHERE a.approved = 1
ORDER BY a.pinned DESC, a.created_at DESC, a.id DESC`

const listUserArticlesQuery = `
SELECT a.id, a.title, a.content,
       strftime('%Y-%m-%d %H:%M', a.created_at),
       COALESCE(u.username, ''), a.approved, a.pinned
FROM articles a
LEFT JOIN article_revisions r
       ON r.id = (SELECT MIN(id) FROM article_revisions WHERE article_id = a.id)
LEFT JOIN users u ON u.id = r.edited_by
WHERE (a.approved = 1 OR r.edited_by = ?)
ORDER BY a.pinned DESC, a.created_at DESC, a.id DESC`

const getArticleQuery = `
SELECT a.id, a.title, a.content,
       strftime('%Y-%m-%d %H:%M', a.created_at),
       COALESCE(u.username, ''), a.approved, a.pinned
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

// 未审核评论只对管理员可见，普通访客只看已通过的评论
const listApprovedCommentsQuery = `
SELECT id, article_id, user_id, COALESCE(author_name, ''), content,
       strftime('%Y-%m-%d %H:%M', created_at), approved
FROM comments
WHERE article_id = ? AND approved = 1
ORDER BY created_at ASC, id ASC`

// ListArticles 渲染首页文章列表
func ListArticles(a *middleware.Adapter) error {
	if a.Request.URL.Path != "/" {
		return notFound(a, "页面不存在")
	}

	env := models.EnvFromAdapter(a)
	var (
		query = listPublishedArticlesQuery
		tag   models.Tag
		tagID int64
		rows  *sql.Rows
		args  []any
		err   error
	)
	if rawTag := strings.TrimSpace(a.Request.URL.Query().Get("tag")); rawTag != "" {
		tagID, err = strconv.ParseInt(rawTag, 10, 64)
		if err != nil || tagID <= 0 {
			return notFound(a, "标签不存在")
		}
		if err := env.DB.QueryRowContext(env.Ctx,
			`SELECT id, name, slug FROM tags WHERE id = ?`, tagID).
			Scan(&tag.ID, &tag.Name, &tag.Slug); err != nil {
			if err == sql.ErrNoRows {
				return notFound(a, "标签不存在")
			}
			return fmt.Errorf("ListArticles: load tag: %w", err)
		}
	}
	if session, ok := currentSession(a.Request); ok {
		roleID, err := currentRoleID(a)
		if err != nil {
			return fmt.Errorf("ListArticles: %w", err)
		}
		if users.IsAdminRole(roleID) {
			query = listArticlesQuery
		} else {
			query = listUserArticlesQuery
			args = append(args, session.UserID)
		}
	}
	if tagID > 0 {
		query = addTagFilter(query)
		args = append(args, tagID)
	}
	rows, err = env.DB.QueryContext(env.Ctx, query, args...)
	if err != nil {
		return fmt.Errorf("ListArticles: %w", err)
	}
	defer rows.Close()

	articles := make([]models.Article, 0)
	for rows.Next() {
		var article models.Article
		if err := rows.Scan(&article.ID, &article.Title, &article.Content, &article.CreatedAt,
			&article.Author, &article.Approved, &article.Pinned); err != nil {
			return fmt.Errorf("ListArticles: %w", err)
		}
		article.Excerpt = excerpt(article.Content, 160)
		articles = append(articles, article)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("ListArticles: %w", err)
	}
	if err := attachArticleTags(env, articles); err != nil {
		return fmt.Errorf("ListArticles: %w", err)
	}

	return renderPage(a, "index.html", ViewData{
		Title:       "首页",
		Articles:    articles,
		SelectedTag: tag,
		Breadcrumbs: articleListBreadcrumbs(tag),
	})
}

func addTagFilter(query string) string {
	orderBy := "\nORDER BY "
	index := strings.Index(query, orderBy)
	if index < 0 {
		return query
	}
	return query[:index] +
		"\nAND EXISTS (SELECT 1 FROM article_tags at WHERE at.article_id = a.id AND at.tag_id = ?)" +
		query[index:]
}

func articleListBreadcrumbs(tag models.Tag) []models.Breadcrumb {
	if tag.ID == 0 {
		return []models.Breadcrumb{{Title: "首页", URL: "/"}}
	}
	return []models.Breadcrumb{{Title: "首页", URL: "/"}, {Title: "标签：" + tag.Name}}
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
	if !article.Approved {
		allowed, err := canEditArticle(a, id)
		if err != nil {
			return fmt.Errorf("ShowArticle: %w", err)
		}
		if !allowed {
			return notFound(a, "文章不存在")
		}
	}

	comments, err := listComments(env, id, isAdminUser(a))
	if err != nil {
		return fmt.Errorf("ShowArticle: %w", err)
	}

	message := ""
	switch a.Request.URL.Query().Get("pending") {
	case "approved":
		message = "评论已通过审核，现在所有访客都能看到"
	case "1":
		message = "评论已提交，等待管理员审核后展示"
	}
	return renderPostPage(a, article, comments, message, "")
}

// getArticle 查询单篇文章
func getArticle(env *models.Env, id int64) (models.Article, error) {
	var article models.Article
	err := env.DB.QueryRowContext(env.Ctx, getArticleQuery, id).
		Scan(&article.ID, &article.Title, &article.Content, &article.CreatedAt,
			&article.Author, &article.Approved, &article.Pinned)
	return article, err
}

// listComments 查询文章下的评论；includePending 为 true 时包含待审核评论（仅管理员）。
func listComments(env *models.Env, articleID int64, includePending bool) ([]models.Comment, error) {
	query := listCommentsQuery
	if !includePending {
		query = listApprovedCommentsQuery
	}

	rows, err := env.DB.QueryContext(env.Ctx, query, articleID)
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

// renderPostPage 渲染详情页（含 Markdown 正文、评论列表与提示信息）。
// notice 为绿色提示，errMsg 为红色错误提示，两者互斥使用。
func renderPostPage(a *middleware.Adapter, article models.Article, comments []models.Comment, notice, errMsg string) error {
	articles := []models.Article{article}
	if err := attachArticleTags(models.EnvFromAdapter(a), articles); err != nil {
		return fmt.Errorf("renderPostPage: load tags: %w", err)
	}
	article = articles[0]

	bodyHTML, err := renderMarkdown(article.Content)
	if err != nil {
		return fmt.Errorf("renderPostPage: %w", err)
	}

	canEdit, err := canEditArticle(a, article.ID)
	if err != nil {
		return fmt.Errorf("renderPostPage: %w", err)
	}

	return renderPage(a, "post.html", ViewData{
		Title:          article.Title,
		Article:        article,
		ArticleHTML:    bodyHTML,
		ArticleID:      article.ID,
		Breadcrumbs:    []models.Breadcrumb{{Title: "首页", URL: "/"}, {Title: article.Title}},
		Comments:       comments,
		Message:        notice,
		Error:          errMsg,
		CanEditArticle: canEdit,
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
		comments, err := listComments(env, id, isAdminUser(a))
		if err != nil {
			return fmt.Errorf("CreateComment: %w", err)
		}
		return renderPostPage(a, article, comments, "", "评论内容不能为空")
	}

	// comments.approved 默认 0（待审核），这里显式写入待审核状态
	_, err = env.DB.ExecContext(env.Ctx,
		`INSERT INTO comments (article_id, user_id, author_name, content, approved) VALUES (?, ?, ?, ?, 0)`,
		id, session.UserID, session.Username, content)
	if err != nil {
		return fmt.Errorf("CreateComment: %w", err)
	}

	http.Redirect(a.ResponseWriter, r, fmt.Sprintf("/post/%d?pending=1#comments", id), http.StatusSeeOther)
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

// ShowEditor 渲染发文表单（需登录）
func ShowEditor(a *middleware.Adapter) error {
	return renderPage(a, "editor.html", ViewData{Title: "写文章"})
}

// ShowEditArticle 渲染现有文章的编辑表单（仅原作者或管理员）。
func ShowEditArticle(a *middleware.Adapter) error {
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
		return fmt.Errorf("ShowEditArticle: %w", err)
	}
	articles := []models.Article{article}
	if err := attachArticleTags(env, articles); err != nil {
		return fmt.Errorf("ShowEditArticle: load tags: %w", err)
	}
	article = articles[0]

	allowed, err := requireArticleEditPermission(a, id)
	if !allowed {
		return err
	}

	return renderPage(a, "editor.html", ViewData{
		Title:          "编辑文章",
		ArticleID:      article.ID,
		EditingArticle: true,
		FormTitle:      article.Title,
		FormContent:    article.Content,
		FormTags:       tagNames(article.Tags),
	})
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
	roleID, err := currentRoleID(a)
	if err != nil {
		return fmt.Errorf("CreateArticle: %w", err)
	}

	title := strings.TrimSpace(r.FormValue("title"))
	content := strings.TrimSpace(r.FormValue("content"))
	tags, tagError := parseArticleTags(r.FormValue("tags"))

	if formError := firstValidationError(validateArticle(title, content), tagError); formError != "" {
		return renderPage(a, "editor.html", ViewData{
			Title:       "写文章",
			Error:       formError,
			FormTitle:   title,
			FormContent: content,
			FormTags:    r.FormValue("tags"),
		})
	}

	env := models.EnvFromAdapter(a)
	tx, err := env.DB.BeginTx(env.Ctx, nil)
	if err != nil {
		return fmt.Errorf("CreateArticle: %w", err)
	}
	defer tx.Rollback()

	approved := users.IsAdminRole(roleID)
	result, err := tx.ExecContext(env.Ctx,
		`INSERT INTO articles (title, content, approved) VALUES (?, ?, ?)`, title, content, approved)
	if err != nil {
		return fmt.Errorf("CreateArticle: %w", err)
	}

	articleID, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("CreateArticle: %w", err)
	}
	if err := replaceArticleTags(env.Ctx, tx, articleID, tags); err != nil {
		return fmt.Errorf("CreateArticle: save tags: %w", err)
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

// UpdateArticle 保存文章修改，并记录一条修订。
func UpdateArticle(a *middleware.Adapter) error {
	r := a.Request
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return notFound(a, "文章不存在")
	}

	env := models.EnvFromAdapter(a)
	article, err := getArticle(env, id)
	if err == sql.ErrNoRows {
		return notFound(a, "文章不存在")
	}
	if err != nil {
		return fmt.Errorf("UpdateArticle: %w", err)
	}

	allowed, err := requireArticleEditPermission(a, id)
	if !allowed {
		return err
	}
	if err := r.ParseForm(); err != nil {
		return fmt.Errorf("UpdateArticle: %w", err)
	}

	title := strings.TrimSpace(r.FormValue("title"))
	content := strings.TrimSpace(r.FormValue("content"))
	tags, tagError := parseArticleTags(r.FormValue("tags"))
	if formError := firstValidationError(validateArticle(title, content), tagError); formError != "" {
		return renderPage(a, "editor.html", ViewData{
			Title:          "编辑文章",
			ArticleID:      article.ID,
			EditingArticle: true,
			FormTitle:      title,
			FormContent:    content,
			FormTags:       r.FormValue("tags"),
			Error:          formError,
		})
	}

	session, ok := currentSession(r)
	if !ok {
		http.Redirect(a.ResponseWriter, r, "/login", http.StatusSeeOther)
		return nil
	}
	roleID, err := currentRoleID(a)
	if err != nil {
		return fmt.Errorf("UpdateArticle: %w", err)
	}

	tx, err := env.DB.BeginTx(env.Ctx, nil)
	if err != nil {
		return fmt.Errorf("UpdateArticle: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(env.Ctx,
		`UPDATE articles SET title = ?, content = ?, approved = ? WHERE id = ?`,
		title, content, users.IsAdminRole(roleID), id)
	if err != nil {
		return fmt.Errorf("UpdateArticle: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("UpdateArticle: %w", err)
	} else if affected == 0 {
		return notFound(a, "文章不存在")
	}

	if _, err := tx.ExecContext(env.Ctx,
		`INSERT INTO article_revisions (article_id, title, content, edited_by) VALUES (?, ?, ?, ?)`,
		id, title, content, session.UserID); err != nil {
		return fmt.Errorf("UpdateArticle: %w", err)
	}
	if err := replaceArticleTags(env.Ctx, tx, id, tags); err != nil {
		return fmt.Errorf("UpdateArticle: save tags: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("UpdateArticle: %w", err)
	}

	http.Redirect(a.ResponseWriter, r, fmt.Sprintf("/post/%d", id), http.StatusSeeOther)
	return nil
}

func firstValidationError(first, second string) string {
	if first != "" {
		return first
	}
	return second
}

func validateArticle(title, content string) string {
	if title == "" || content == "" {
		return "标题和正文都不能为空"
	}
	if utf8.RuneCountInString(title) > 200 {
		return "标题不能超过 200 个字符"
	}
	return ""
}

func canEditArticle(a *middleware.Adapter, articleID int64) (bool, error) {
	session, ok := currentSession(a.Request)
	if !ok {
		return false, nil
	}

	roleID, err := currentRoleID(a)
	if err != nil {
		return false, fmt.Errorf("canEditArticle: %w", err)
	}
	if roleID == users.RoleAdmin {
		return true, nil
	}

	var authorID sql.NullInt64
	env := models.EnvFromAdapter(a)
	err = env.DB.QueryRowContext(
		env.Ctx,
		`SELECT edited_by FROM article_revisions WHERE article_id = ? ORDER BY id ASC LIMIT 1`,
		articleID,
	).Scan(&authorID)
	if err != nil && err != sql.ErrNoRows {
		return false, fmt.Errorf("canEditArticle: %w", err)
	}
	return authorID.Valid && authorID.Int64 == session.UserID, nil
}

func requireArticleEditPermission(a *middleware.Adapter, articleID int64) (bool, error) {
	if _, ok := currentSession(a.Request); !ok {
		http.Redirect(a.ResponseWriter, a.Request, "/login", http.StatusSeeOther)
		return false, nil
	}

	allowed, err := canEditArticle(a, articleID)
	if err != nil {
		return false, err
	}
	if allowed {
		return true, nil
	}

	a.ResponseWriter.WriteHeader(http.StatusForbidden)
	return false, renderPage(a, "403.html", ViewData{
		Title: "403",
		Error: "只有文章作者或管理员可以编辑此文章",
	})
}
