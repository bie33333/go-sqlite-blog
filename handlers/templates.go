package handlers

import (
	"fmt"
	"html/template"
	"sync"

	"github.com/AndreHeber/go-sqlite-blog/middleware"
	"github.com/AndreHeber/go-sqlite-blog/models"
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
		data.IsAdmin = isAdminUser(a)
	} else {
		data.LoggedIn = false
		data.Username = ""
		data.IsAdmin = false
	}

	a.ResponseWriter.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(a.ResponseWriter, "layout.html", data); err != nil {
		return fmt.Errorf("renderPage(%s): %w", page, err)
	}
	return nil
}
