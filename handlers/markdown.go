package handlers

import (
	"bytes"
	"html"
	"html/template"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	goldmarkhtml "github.com/yuin/goldmark/renderer/html"
)

// markdownConverter 是共用的 goldmark 实例（线程安全，可在多个请求间复用）。
//
// 安全说明：goldmark 的 HTML renderer 默认不允许原始 HTML 透传
// （未开启 WithUnsafe），Markdown 中的 <script> 等标签会被丢弃，
// 因此渲染结果可以安全地作为 template.HTML 输出。
// 同时 WithHardWraps 让单个换行渲染成 <br>，符合博客写作习惯。
var markdownConverter = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithRendererOptions(
		goldmarkhtml.WithHardWraps(),
	),
)

// renderMarkdown 把 Markdown 源文渲染成可直接嵌入模板的 HTML。
func renderMarkdown(source string) (template.HTML, error) {
	if strings.TrimSpace(source) == "" {
		return "", nil
	}

	var buf bytes.Buffer
	if err := markdownConverter.Convert([]byte(source), &buf); err != nil {
		return "", err
	}

	return template.HTML(buf.String()), nil //nolint:gosec // goldmark 默认转义原始 HTML
}

var htmlTagPattern = regexp.MustCompile(`(?s)<[^>]*>`)

// markdownToText 把 Markdown 转成纯文本，用于列表页摘要。
// 先经 goldmark 渲染再剥离标签，这样不会把 "#"、"**" 等标记带进摘要。
func markdownToText(source string) string {
	rendered, err := renderMarkdown(source)
	if err != nil {
		return strings.Join(strings.Fields(source), " ")
	}

	text := htmlTagPattern.ReplaceAllString(string(rendered), " ")
	text = html.UnescapeString(text)
	return strings.Join(strings.Fields(text), " ")
}

// excerpt 生成列表页摘要（按字符数截断，避免切断多字节字符）。
func excerpt(content string, limit int) string {
	text := markdownToText(content)
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit]) + "…"
}
