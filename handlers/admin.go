package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/AndreHeber/go-sqlite-blog/middleware"
	"github.com/AndreHeber/go-sqlite-blog/models"
	"github.com/AndreHeber/go-sqlite-blog/models/users"
)

// adminCommentsQuery 列出所有评论，待审核的排在前面。
const adminCommentsQuery = `
SELECT c.id, c.article_id, COALESCE(a.title, '(文章已删除)'), c.user_id,
       COALESCE(c.author_name, ''), c.content,
       strftime('%Y-%m-%d %H:%M', c.created_at), c.approved
FROM comments c
LEFT JOIN articles a ON a.id = c.article_id
ORDER BY c.approved ASC, c.created_at DESC, c.id DESC`

// currentRoleID 从数据库读取当前登录用户的角色；未登录或读取失败返回 0。
func currentRoleID(a *middleware.Adapter) (int64, error) {
	session, ok := currentSession(a.Request)
	if !ok {
		return 0, nil
	}
	return users.GetRoleID(models.EnvFromAdapter(a), session.UserID)
}

// isAdminUser 以数据库中的 role_id 为准判断当前用户是否管理员。
func isAdminUser(a *middleware.Adapter) bool {
	roleID, err := currentRoleID(a)
	if err != nil {
		a.Logger.Error("handlers: isAdminUser", "error", err)
		return false
	}
	return users.IsAdminRole(roleID)
}

// requireAdmin 校验管理员身份：未登录 302 到 /login，非管理员返回 403 页面。
// 返回 false 表示已经写过响应，调用方应直接返回。
func requireAdmin(a *middleware.Adapter) (bool, error) {
	if _, ok := currentSession(a.Request); !ok {
		http.Redirect(a.ResponseWriter, a.Request, "/login", http.StatusSeeOther)
		return false, nil
	}
	if isAdminUser(a) {
		return true, nil
	}

	a.ResponseWriter.WriteHeader(http.StatusForbidden)
	return false, renderPage(a, "403.html", ViewData{
		Title: "403",
		Error: "此页面仅管理员可访问",
	})
}

// adminRedirectTarget 返回管理操作完成后的跳转地址。
// 详情页也会放置管理按钮，通过表单里的 next 回到该页；仅接受站内相对路径，
// 避免开放重定向。默认回管理后台。
func adminRedirectTarget(r *http.Request) string {
	next := strings.TrimSpace(r.FormValue("next"))
	if strings.HasPrefix(next, "/") && !strings.HasPrefix(next, "//") {
		return next
	}
	return "/admin"
}

// AdminDashboard 渲染管理后台：评论审核 + 文章管理
func AdminDashboard(a *middleware.Adapter) error {
	if ok, err := requireAdmin(a); !ok {
		return err
	}

	env := models.EnvFromAdapter(a)

	comments, err := listAdminComments(env)
	if err != nil {
		return fmt.Errorf("AdminDashboard: %w", err)
	}

	articles, err := listAllArticles(env)
	if err != nil {
		return fmt.Errorf("AdminDashboard: %w", err)
	}
	pages, err := listAdminPages(env)
	if err != nil {
		return fmt.Errorf("AdminDashboard: %w", err)
	}

	return renderPage(a, "admin.html", ViewData{
		Title:         "管理后台",
		AdminComments: comments,
		Articles:      articles,
		Pages:         pages,
	})
}

// ApproveComment 通过评论审核
func ApproveComment(a *middleware.Adapter) error {
	if ok, err := requireAdmin(a); !ok {
		return err
	}

	id, err := strconv.ParseInt(a.Request.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return notFound(a, "评论不存在")
	}

	env := models.EnvFromAdapter(a)
	result, err := env.DB.ExecContext(env.Ctx, `UPDATE comments SET approved = 1 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("ApproveComment: %w", err)
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		return notFound(a, "评论不存在")
	}

	http.Redirect(a.ResponseWriter, a.Request, adminRedirectTarget(a.Request), http.StatusSeeOther)
	return nil
}

// ApproveArticle publishes a pending article.
func ApproveArticle(a *middleware.Adapter) error {
	if ok, err := requireAdmin(a); !ok {
		return err
	}

	id, err := strconv.ParseInt(a.Request.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return notFound(a, "文章不存在")
	}

	env := models.EnvFromAdapter(a)
	result, err := env.DB.ExecContext(env.Ctx, `UPDATE articles SET approved = 1 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("ApproveArticle: %w", err)
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		return notFound(a, "文章不存在")
	}

	http.Redirect(a.ResponseWriter, a.Request, adminRedirectTarget(a.Request), http.StatusSeeOther)
	return nil
}

func SetArticlePinned(a *middleware.Adapter) error {
	if ok, err := requireAdmin(a); !ok {
		return err
	}
	if !validPageCSRF(a) {
		return pageForbidden(a)
	}

	id, err := strconv.ParseInt(a.Request.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return notFound(a, "文章不存在")
	}
	pinned, err := strconv.ParseBool(a.Request.FormValue("pinned"))
	if err != nil {
		http.Error(a.ResponseWriter, "置顶状态无效", http.StatusBadRequest)
		return nil
	}

	env := models.EnvFromAdapter(a)
	result, err := env.DB.ExecContext(env.Ctx,
		`UPDATE articles SET pinned = ? WHERE id = ?`, pinned, id)
	if err != nil {
		return fmt.Errorf("SetArticlePinned: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("SetArticlePinned: %w", err)
	} else if affected == 0 {
		return notFound(a, "文章不存在")
	}

	http.Redirect(a.ResponseWriter, a.Request, adminRedirectTarget(a.Request), http.StatusSeeOther)
	return nil
}

// DeleteComment 删除评论
func DeleteComment(a *middleware.Adapter) error {
	if ok, err := requireAdmin(a); !ok {
		return err
	}

	id, err := strconv.ParseInt(a.Request.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return notFound(a, "评论不存在")
	}

	env := models.EnvFromAdapter(a)
	tx, err := env.DB.BeginTx(env.Ctx, nil)
	if err != nil {
		return fmt.Errorf("DeleteComment: %w", err)
	}
	defer tx.Rollback()

	// 先删引用该评论的点赞记录，避免外键约束失败
	if _, err := tx.ExecContext(env.Ctx, `DELETE FROM likes WHERE comment_id = ?`, id); err != nil {
		return fmt.Errorf("DeleteComment: %w", err)
	}

	result, err := tx.ExecContext(env.Ctx, `DELETE FROM comments WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("DeleteComment: %w", err)
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		return notFound(a, "评论不存在")
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("DeleteComment: %w", err)
	}

	http.Redirect(a.ResponseWriter, a.Request, adminRedirectTarget(a.Request), http.StatusSeeOther)
	return nil
}

// DeleteArticle 删除文章及其关联数据
func DeleteArticle(a *middleware.Adapter) error {
	if ok, err := requireAdmin(a); !ok {
		return err
	}

	id, err := strconv.ParseInt(a.Request.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return notFound(a, "文章不存在")
	}

	env := models.EnvFromAdapter(a)
	tx, err := env.DB.BeginTx(env.Ctx, nil)
	if err != nil {
		return fmt.Errorf("DeleteArticle: %w", err)
	}
	defer tx.Rollback()

	// 按外键依赖顺序清理子表
	if _, err := tx.ExecContext(env.Ctx,
		`DELETE FROM likes WHERE article_id = ? OR comment_id IN (SELECT id FROM comments WHERE article_id = ?)`,
		id, id); err != nil {
		return fmt.Errorf("DeleteArticle: %w", err)
	}

	childStatements := []string{
		`DELETE FROM comments WHERE article_id = ?`,
		`DELETE FROM article_tags WHERE article_id = ?`,
		`DELETE FROM article_categories WHERE article_id = ?`,
		`DELETE FROM article_revisions WHERE article_id = ?`,
	}
	for _, statement := range childStatements {
		if _, err := tx.ExecContext(env.Ctx, statement, id); err != nil {
			return fmt.Errorf("DeleteArticle: %w", err)
		}
	}

	result, err := tx.ExecContext(env.Ctx, `DELETE FROM articles WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("DeleteArticle: %w", err)
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		return notFound(a, "文章不存在")
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("DeleteArticle: %w", err)
	}

	http.Redirect(a.ResponseWriter, a.Request, adminRedirectTarget(a.Request), http.StatusSeeOther)
	return nil
}

// listAdminComments 管理后台的评论列表（含待审核）
func listAdminComments(env *models.Env) ([]models.Comment, error) {
	rows, err := env.DB.QueryContext(env.Ctx, adminCommentsQuery)
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
		if err := rows.Scan(&comment.ID, &comment.ArticleID, &comment.ArticleTitle, &userID,
			&comment.AuthorName, &comment.Content, &comment.CreatedAt, &comment.Approved); err != nil {
			return nil, err
		}
		if userID.Valid {
			comment.UserID = &userID.Int64
		}
		comments = append(comments, comment)
	}
	return comments, rows.Err()
}

// listAllArticles 管理后台的文章列表
func listAllArticles(env *models.Env) ([]models.Article, error) {
	rows, err := env.DB.QueryContext(env.Ctx, listArticlesQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	articles := make([]models.Article, 0)
	for rows.Next() {
		var article models.Article
		if err := rows.Scan(&article.ID, &article.Title, &article.Content, &article.CreatedAt,
			&article.Author, &article.Approved, &article.Pinned); err != nil {
			return nil, err
		}
		articles = append(articles, article)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := attachArticleTags(env, articles); err != nil {
		return nil, err
	}
	return articles, nil
}
