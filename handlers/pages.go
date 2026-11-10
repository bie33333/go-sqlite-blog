package handlers

import (
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/AndreHeber/go-sqlite-blog/middleware"
	"github.com/AndreHeber/go-sqlite-blog/models"
)

const (
	maxPageHTMLBytes = 1 << 20
	maxPageBodyBytes = maxPageHTMLBytes + 128<<10
)

var pageSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func listAdminPages(env *models.Env) ([]models.Page, error) {
	rows, err := env.DB.QueryContext(env.Ctx, `
		SELECT id, title, slug, status, allow_scripts,
		       strftime('%Y-%m-%d %H:%M', created_at),
		       COALESCE(strftime('%Y-%m-%d %H:%M', updated_at), '')
		FROM pages
		ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	pages := make([]models.Page, 0)
	for rows.Next() {
		var page models.Page
		if err := rows.Scan(&page.ID, &page.Title, &page.Slug, &page.Status,
			&page.AllowScripts, &page.CreatedAt, &page.UpdatedAt); err != nil {
			return nil, err
		}
		pages = append(pages, page)
	}
	return pages, rows.Err()
}

func ShowAdminPages(a *middleware.Adapter) error {
	if ok, err := requireAdmin(a); !ok {
		return err
	}
	pages, err := listAdminPages(models.EnvFromAdapter(a))
	if err != nil {
		return fmt.Errorf("ShowAdminPages: %w", err)
	}
	return renderPage(a, "admin_pages.html", ViewData{Title: "自定义页面", Pages: pages})
}

func ShowNewAdminPage(a *middleware.Adapter) error {
	if ok, err := requireAdmin(a); !ok {
		return err
	}
	return renderPage(a, "admin_page_form.html", ViewData{Title: "新建自定义页面"})
}

func ShowEditAdminPage(a *middleware.Adapter) error {
	if ok, err := requireAdmin(a); !ok {
		return err
	}
	page, found, err := getAdminPage(a)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	return renderPage(a, "admin_page_form.html", pageFormData(page, "", ""))
}

func CreateAdminPage(a *middleware.Adapter) error {
	if ok, err := requireAdmin(a); !ok {
		return err
	}
	title, slug, content, allowScripts, message, err := readPageInput(a)
	if err != nil {
		if message == "" {
			return fmt.Errorf("CreateAdminPage: %w", err)
		}
		a.ResponseWriter.WriteHeader(http.StatusBadRequest)
		return renderPage(a, "admin_page_form.html", ViewData{
			Title: title, FormTitle: title, PageSlug: slug, FormContent: content,
			PageAllowScripts: allowScripts, Error: message,
		})
	}
	if !validPageCSRF(a) {
		return pageForbidden(a)
	}

	session, _ := currentSession(a.Request)
	env := models.EnvFromAdapter(a)
	_, err = env.DB.ExecContext(env.Ctx,
		`INSERT INTO pages (title, slug, content, status, allow_scripts, created_by) VALUES (?, ?, ?, 'draft', ?, ?)`,
		title, slug, content, allowScripts, session.UserID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed: pages.slug") {
			a.ResponseWriter.WriteHeader(http.StatusBadRequest)
			return renderPage(a, "admin_page_form.html", ViewData{
				Title: "新建自定义页面", FormTitle: title, PageSlug: slug, FormContent: content,
				PageAllowScripts: allowScripts,
				Error:            "这个 URL slug 已被使用，请换一个。",
			})
		}
		return fmt.Errorf("CreateAdminPage: %w", err)
	}
	http.Redirect(a.ResponseWriter, a.Request, "/admin/pages", http.StatusSeeOther)
	return nil
}

func UpdateAdminPage(a *middleware.Adapter) error {
	if ok, err := requireAdmin(a); !ok {
		return err
	}
	page, found, err := getAdminPage(a)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	title, slug, content, allowScripts, message, err := readPageInput(a)
	if err != nil {
		if message == "" {
			return fmt.Errorf("UpdateAdminPage: %w", err)
		}
		a.ResponseWriter.WriteHeader(http.StatusBadRequest)
		data := pageFormData(page, message, content)
		data.FormTitle = a.Request.FormValue("title")
		data.PageSlug = a.Request.FormValue("slug")
		data.FormContent = content
		data.PageAllowScripts = allowScripts
		return renderPage(a, "admin_page_form.html", data)
	}
	if !validPageCSRF(a) {
		return pageForbidden(a)
	}

	env := models.EnvFromAdapter(a)
	_, err = env.DB.ExecContext(env.Ctx,
		`UPDATE pages SET title = ?, slug = ?, content = ?, status = 'draft', allow_scripts = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		title, slug, content, allowScripts, page.ID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed: pages.slug") {
			a.ResponseWriter.WriteHeader(http.StatusBadRequest)
			return renderPage(a, "admin_page_form.html", ViewData{
				Title: "编辑自定义页面", Page: page, PageEditing: true,
				FormTitle: title, PageSlug: slug, FormContent: content,
				PageAllowScripts: allowScripts,
				Error:            "这个 URL slug 已被使用，请换一个。",
			})
		}
		return fmt.Errorf("UpdateAdminPage: %w", err)
	}
	http.Redirect(a.ResponseWriter, a.Request, "/admin/pages", http.StatusSeeOther)
	return nil
}

func PublishAdminPage(a *middleware.Adapter) error {
	return changeAdminPageStatus(a, "published")
}

func UnpublishAdminPage(a *middleware.Adapter) error {
	return changeAdminPageStatus(a, "draft")
}

func changeAdminPageStatus(a *middleware.Adapter, status string) error {
	if ok, err := requireAdmin(a); !ok {
		return err
	}
	if !validPageCSRF(a) {
		return pageForbidden(a)
	}
	page, found, err := getAdminPage(a)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	env := models.EnvFromAdapter(a)
	if _, err := env.DB.ExecContext(env.Ctx,
		`UPDATE pages SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, status, page.ID); err != nil {
		return fmt.Errorf("changeAdminPageStatus: %w", err)
	}
	http.Redirect(a.ResponseWriter, a.Request, "/admin/pages", http.StatusSeeOther)
	return nil
}

func DeleteAdminPage(a *middleware.Adapter) error {
	if ok, err := requireAdmin(a); !ok {
		return err
	}
	if !validPageCSRF(a) {
		return pageForbidden(a)
	}
	page, found, err := getAdminPage(a)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	env := models.EnvFromAdapter(a)
	if _, err := env.DB.ExecContext(env.Ctx, `DELETE FROM pages WHERE id = ?`, page.ID); err != nil {
		return fmt.Errorf("DeleteAdminPage: %w", err)
	}
	http.Redirect(a.ResponseWriter, a.Request, "/admin/pages", http.StatusSeeOther)
	return nil
}

func ShowStaticPage(a *middleware.Adapter) error {
	env := models.EnvFromAdapter(a)
	var page models.Page
	err := env.DB.QueryRowContext(env.Ctx, `
		SELECT title, slug, content, allow_scripts
		FROM pages WHERE slug = ? AND status = 'published'`, a.Request.PathValue("slug")).
		Scan(&page.Title, &page.Slug, &page.Content, &page.AllowScripts)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(a.ResponseWriter, a.Request)
			return nil
		}
		return fmt.Errorf("ShowStaticPage: %w", err)
	}
	return writeIsolatedHTML(a.ResponseWriter, page)
}

func PreviewAdminPage(a *middleware.Adapter) error {
	if ok, err := requireAdmin(a); !ok {
		return err
	}
	page, found, err := getAdminPage(a)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	return writeIsolatedHTML(a.ResponseWriter, page)
}

func writeIsolatedHTML(w http.ResponseWriter, page models.Page) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	scriptPolicy := "script-src 'none'; sandbox"
	if page.AllowScripts {
		scriptPolicy = "script-src 'unsafe-inline' https:; worker-src blob:"
	}
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; "+scriptPolicy+
			"; style-src 'unsafe-inline' https:; img-src data: https:; font-src data: https:;"+
			" connect-src https: wss:; frame-src https:; media-src https:; base-uri 'none';"+
			" form-action 'none'; frame-ancestors 'self'")
	if _, err := io.WriteString(w, page.Content); err != nil {
		return fmt.Errorf("writeIsolatedHTML: write response: %w", err)
	}
	return nil
}

func readPageInput(a *middleware.Adapter) (title, slug, content string, allowScripts bool, message string, err error) {
	r := a.Request
	r.Body = http.MaxBytesReader(a.ResponseWriter, r.Body, maxPageBodyBytes)
	if err := r.ParseMultipartForm(maxPageBodyBytes); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return "", "", "", false, "上传内容过大（HTML 上限 1 MiB）。", nil
		}
		return "", "", "", false, "", fmt.Errorf("parse multipart form: %w", err)
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	allowScripts = r.FormValue("allow_scripts") == "true"

	title = strings.TrimSpace(r.FormValue("title"))
	slug = strings.TrimSpace(r.FormValue("slug"))
	content = r.FormValue("content")
	if title == "" || utf8.RuneCountInString(title) > 200 {
		return title, slug, content, allowScripts, "标题不能为空且不能超过 200 个字符。", nil
	}
	if len(slug) > 80 || !pageSlugPattern.MatchString(slug) {
		return title, slug, content, allowScripts, "URL slug 只能包含小写字母、数字和连字符，且不能以连字符开头或结尾。", nil
	}

	file, header, fileErr := r.FormFile("html_file")
	if fileErr == nil {
		defer file.Close()
		ext := strings.ToLower(filepath.Ext(header.Filename))
		if ext != ".html" && ext != ".htm" {
			return title, slug, content, allowScripts, "只支持 .html 或 .htm 文件。", nil
		}
		if header.Size > maxPageHTMLBytes {
			return title, slug, content, allowScripts, "HTML 文件过大（上限 1 MiB）。", nil
		}
		uploaded, readErr := io.ReadAll(io.LimitReader(file, maxPageHTMLBytes+1))
		if readErr != nil {
			return "", "", "", false, "", fmt.Errorf("read HTML upload: %w", readErr)
		}
		content = string(uploaded)
	} else if !errors.Is(fileErr, http.ErrMissingFile) {
		return "", "", "", false, "", fmt.Errorf("read HTML upload: %w", fileErr)
	}

	if len(content) == 0 || len(content) > maxPageHTMLBytes || !utf8.ValidString(content) {
		return title, slug, content, allowScripts, "HTML 内容不能为空，且必须是 UTF-8 编码、大小不超过 1 MiB。", nil
	}
	return title, slug, content, allowScripts, "", nil
}

func getAdminPage(a *middleware.Adapter) (models.Page, bool, error) {
	id, err := strconv.ParseInt(a.Request.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return models.Page{}, false, notFound(a, "页面不存在")
	}
	env := models.EnvFromAdapter(a)
	var page models.Page
	err = env.DB.QueryRowContext(env.Ctx, `
		SELECT id, title, slug, content, status, allow_scripts,
		       strftime('%Y-%m-%d %H:%M', created_at),
		       COALESCE(strftime('%Y-%m-%d %H:%M', updated_at), '')
		FROM pages WHERE id = ?`, id).Scan(
		&page.ID, &page.Title, &page.Slug, &page.Content, &page.Status,
		&page.AllowScripts, &page.CreatedAt, &page.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Page{}, false, notFound(a, "页面不存在")
		}
		return models.Page{}, false, fmt.Errorf("getAdminPage: %w", err)
	}
	return page, true, nil
}

func pageFormData(page models.Page, message, content string) ViewData {
	data := ViewData{
		Title: "编辑自定义页面", Page: page, PageEditing: page.ID > 0,
		FormTitle: page.Title, PageSlug: page.Slug, FormContent: page.Content,
		PageAllowScripts: page.AllowScripts, Error: message,
	}
	if content != "" {
		data.FormContent = content
	}
	return data
}

func validPageCSRF(a *middleware.Adapter) bool {
	session, ok := middleware.GetSession(a.Request)
	if !ok || session.CSRFToken == "" {
		return false
	}
	provided := a.Request.FormValue("csrf_token")
	return len(provided) == len(session.CSRFToken) &&
		subtle.ConstantTimeCompare([]byte(provided), []byte(session.CSRFToken)) == 1
}

func pageForbidden(a *middleware.Adapter) error {
	a.ResponseWriter.WriteHeader(http.StatusForbidden)
	return renderPage(a, "403.html", ViewData{Title: "403", Error: "请求验证失败，请重新打开管理页面后重试。"})
}
