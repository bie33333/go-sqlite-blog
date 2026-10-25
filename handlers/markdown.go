package handlers

import (
	"bytes"
	"html"
	"html/template"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	goldmarkhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// markdownConverter 是共用的 goldmark 实例（线程安全，可在多个请求间复用）。
//
// 安全说明：goldmark 的 HTML renderer 默认不允许原始 HTML 透传
// （未开启 WithUnsafe），Markdown 中的 <script> 等标签会被丢弃，
// 因此渲染结果可以安全地作为 template.HTML 输出。
// 同时 WithHardWraps 让单个换行渲染成 <br>，符合博客写作习惯。
var markdownConverter = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(
		parser.WithASTTransformers(
			util.Prioritized(imageAttributeTransformer{}, 100),
		),
	),
	goldmark.WithRendererOptions(
		goldmarkhtml.WithHardWraps(),
	),
)

// imageAttributeTransformer 给渲染出的 <img> 补上几个属性：
//   - loading="lazy" / decoding="async"：长文章里图片延迟加载，不阻塞首屏
//   - referrerpolicy="no-referrer"：外链图床常做 Referer 防盗链，不发送 referer 才显示得出来
//
// 另外解析 Markdown 图片后面紧跟的尺寸属性，例如：
//
//	![封面](https://example.com/a.png){width=40%}
//	![封面](https://example.com/a.png){zoom=40%}
//	![封面](https://example.com/a.png){width=400, height=200}
//
// goldmark 的 ImageAttributeFilter 白名单包含这几个属性，所以能被渲染出来。
type imageAttributeTransformer struct{}

func (imageAttributeTransformer) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()

	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		img, ok := n.(*ast.Image)
		if !ok {
			return ast.WalkContinue, nil
		}

		img.SetAttributeString("loading", []byte("lazy"))
		img.SetAttributeString("decoding", []byte("async"))
		img.SetAttributeString("referrerpolicy", []byte("no-referrer"))

		applyMarkdownImageSize(img, source)
		return ast.WalkContinue, nil
	})
}

// maxSizeAttributeBytes 限制花括号属性的扫描长度，避免在超长段落上做无谓扫描。
const maxSizeAttributeBytes = 200

// applyMarkdownImageSize 处理紧跟在图片后面的 {width=40%} 这类属性，
// 并把这段花括号从正文里裁掉，避免它被当成正文显示出来。
func applyMarkdownImageSize(img *ast.Image, source []byte) {
	textNode, ok := img.NextSibling().(*ast.Text)
	if !ok {
		return
	}

	start := textNode.Segment.Start
	if start < 0 || start >= len(source) || source[start] != '{' {
		return
	}

	// 花括号内容可能被 goldmark 拆成多个 Text 节点（例如 {width=400, height=200}
	// 中间的空格），所以直接在源文本上向后找闭合括号，而不是只看单个节点。
	limit := start + maxSizeAttributeBytes
	if limit > len(source) {
		limit = len(source)
	}

	end := -1
	for i := start + 1; i < limit; i++ {
		switch source[i] {
		case '}':
			end = i
		case '\n', '\r':
			return // 跨行说明不是紧邻图片的属性
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		return
	}

	style := parseSizeAttributes(string(source[start+1 : end]))
	if style == "" {
		return // 不是尺寸属性，保持原文不动
	}

	img.SetAttributeString("style", []byte(style))
	trimNodesFrom(textNode, end+1)
}

// trimNodesFrom 从 node 开始，把结束位置 end 之前的文本节点删掉（或裁短）。
func trimNodesFrom(node ast.Node, end int) {
	for node != nil {
		next := node.NextSibling()

		textNode, ok := node.(*ast.Text)
		if !ok {
			return
		}

		seg := textNode.Segment
		switch {
		case seg.Stop <= end:
			parent := textNode.Parent()
			if parent == nil {
				return
			}
			parent.RemoveChild(parent, textNode)
		case seg.Start < end:
			seg.Start = end
			textNode.Segment = seg
			return
		default:
			return
		}

		node = next
	}
}

// sizeValuePattern 限制尺寸值：数字 + 可选单位。
var sizeValuePattern = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?(px|%|em|rem|vw|vh)?$`)

// parseSizeAttributes 把 "width=40%, height=200" 解析成 style 字符串。
// 支持 width/w、height/h、zoom 三个键；显式 width 优先于 zoom。
func parseSizeAttributes(raw string) string {
	width, height, zoom := "", "", ""

	for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == ';'
	}) {
		key, value, found := strings.Cut(part, "=")
		if !found {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)

		switch key {
		case "width", "w":
			if width == "" {
				width, _ = normalizeSizeValue(value)
			}
		case "height", "h":
			if height == "" {
				height, _ = normalizeSizeValue(value)
			}
		case "zoom":
			if zoom == "" {
				if percent, ok := zoomToWidthPercent(value); ok {
					zoom = percent
				}
			}
		}
	}

	styles := make([]string, 0, 2)
	switch {
	case width != "":
		styles = append(styles, "width:"+width)
	case zoom != "":
		styles = append(styles, "width:"+zoom)
	}
	if height != "" {
		styles = append(styles, "height:"+height)
	}
	return strings.Join(styles, ";")
}

// normalizeSizeValue 校验尺寸值；纯数字按 px 处理（CSS 里不带单位的数字无效）。
func normalizeSizeValue(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if !sizeValuePattern.MatchString(value) {
		return "", false
	}
	if last := value[len(value)-1]; last >= '0' && last <= '9' {
		value += "px"
	}
	return value, true
}

// renderMarkdown 把 Markdown 源文渲染成可直接嵌入模板的 HTML。
//
// 正文里粘贴的 <img ...> 会先被抽取成占位符（见 embedded_image.go），
// 渲染后再替换成按白名单重建的图片标签，因此除了图片以外的原始 HTML
// 依然会被 goldmark 丢弃。
func renderMarkdown(source string) (template.HTML, error) {
	if strings.TrimSpace(source) == "" {
		return "", nil
	}

	source, embedded := prepareEmbeddedImages(source)

	var buf bytes.Buffer
	if err := markdownConverter.Convert([]byte(source), &buf); err != nil {
		return "", err
	}

	out := buf.String()
	for placeholder, tag := range embedded {
		out = strings.ReplaceAll(out, placeholder, tag)
	}

	return template.HTML(out), nil //nolint:gosec // 原始 HTML 已被 goldmark 丢弃，图片标签按白名单重建
}

var htmlTagPattern = regexp.MustCompile(`(?s)<[^>]*>`)

// markdownToText 把 Markdown 转成纯文本，用于列表页摘要。
// 先经 goldmark 渲染再剥离标签，这样不会把 "#"、"**" 等标记带进摘要。
func markdownToText(source string) string {
	// 先去掉 HTML 图片，避免摘要里留下占位符
	rendered, err := renderMarkdown(stripHTMLImages(source))
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
