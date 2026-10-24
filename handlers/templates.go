package handlers

import (
	"fmt"
	"html/template"
	"sync"

	"github.com/AndreHeber/go-sqlite-blog/middleware"
	"github.com/AndreHeber/go-sqlite-blog/models"
	"github.com/AndreHeber/go-sqlite-blog/models/users"
)

// templatesDir 是模板目录，相对于运行时工作目录（项目根目录）。
const templatesDir = "static/templates"

// ViewData 是所有页面共用的视图数据结构。
// 页面模板通过 layout.html 中的 {{template "content" .}} 使用它。
type ViewData struct {
	Title    string
	Username string
	LoggedIn bool
	IsAdmin  bool
	// RoleID / RoleName / RoleSlug 用于在界面上展示当前用户身份
	RoleID   int64
	RoleName string
	RoleSlug string
	Error    string
	Message  string

	// 页面数据
	Articles      []models.Article
	Article       models.Article
	Comments      []models.Comment
	AdminComments []models.Comment
	ArticleID     int64

	// 表单回显
	FormTitle   string
	FormContent string
}

// templateCache 缓存已解析的模板，避免每次请求都读盘。
var templateCache sync.Map

// loadTemplates 统一加载 layout.html + 具体页面模板。
func loadTemplates(page string) (*template.Template, error) {
	if cached, ok := templateCache.Load(page); ok {
		return cached.(*template.Template), nil
	}

	tmpl, err := template.ParseFiles(
		templatesDir+"/layout.html",
		templatesDir+"/"+page,
	)
	if err != nil {
		return nil, fmt.Errorf("loadTemplates(%s): %w", page, err)
	}

	templateCache.Store(page, tmpl)
	return tmpl, nil
}

// renderPage 渲染带有统一布局的页面，并自动填充登录态信息。
func renderPage(a *middleware.Adapter, page string, data ViewData) error {
	tmpl, err := loadTemplates(page)
	if err != nil {
		return err
	}

	if session, ok := middleware.GetSession(a.Request); ok {
		data.LoggedIn = true
		data.Username = session.Username

		// 角色以数据库为准，提升/降级后无需重新登录即可生效
		roleID, err := currentRoleID(a)
		if err != nil {
			a.Logger.Error("renderPage: failed to load role", "error", err, "user", session.Username)
		}
		data.RoleID = roleID
		data.RoleName = users.RoleLabel(roleID)
		data.RoleSlug = users.RoleName(roleID)
		data.IsAdmin = users.IsAdminRole(roleID)
	} else {
		data.LoggedIn = false
		data.Username = ""
		data.RoleID = 0
		data.RoleName = ""
		data.RoleSlug = ""
		data.IsAdmin = false
	}

	a.ResponseWriter.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(a.ResponseWriter, "layout.html", data); err != nil {
		return fmt.Errorf("renderPage(%s): %w", page, err)
	}
	return nil
}
