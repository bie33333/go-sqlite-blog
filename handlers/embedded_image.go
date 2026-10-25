package handlers

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// 说明：正文里粘贴进来的原始 HTML（例如从网页复制的 <img src="..." style="zoom: 5%;">）
// 默认会被 goldmark 丢弃，只留下 alt 文本，看起来就像「图片显示不出来」。
//
// 这里不打开 goldmark 的 WithUnsafe（那等于允许任意 HTML，会带来 XSS 风险），
// 而是走「白名单抽取」：
//  1. 渲染前把正文里的 <img ...> 抽出来，换成一个不会与正文冲突的占位符；
//  2. 按白名单重建 <img> 标签（只保留 src / alt / title / 尺寸类样式）；
//  3. 渲染完成后把占位符替换回重建好的标签。
//
// 因此除了被白名单允许的图片标签，其它原始 HTML 依然会被丢弃。

const (
	imagePlaceholderPrefix = "BLOGIMGHOLDER"
	imagePlaceholderSuffix = "END"

	// 图片占位符最大数量，防止恶意正文塞入几万个标签
	maxEmbeddedImages = 200
)

// htmlImagePattern 匹配 <img ...> 或 <img ... />（大小写不敏感，允许跨行）。
var htmlImagePattern = regexp.MustCompile(`(?is)<img\b([^>]*?)/?>`)

// pseudoImageLinkPattern 匹配网页/AI 复制常见的 [image](url) 写法。
// 只有链接文字是 image/图片/图像 时才当作图片，避免把普通链接误变成图片。
var pseudoImageLinkPattern = regexp.MustCompile(
	`(?i)\[(?:image|图片|图像)\]\((https?://[^\s()<>]+?)\)`)

// htmlAttributePattern 解析标签内的 name="value" / name='value' / name=value。
var htmlAttributePattern = regexp.MustCompile(
	`(?i)([a-z_:][a-z0-9_.:-]*)\s*=\s*("[^"]*"|'[^']*'|[^\s"'>]+)`)

// safeImageURLPattern 只允许 http(s)、站内绝对路径与相对路径。
var safeImageURLPattern = regexp.MustCompile(`(?i)^(https?://|/|\./)`)

// stylePropPattern 解析 style="width: 40%; height: auto"。
var stylePropPattern = regexp.MustCompile(`([a-zA-Z-]+)\s*:\s*([^;]+)`)

// allowedStyleProps 是允许透传的样式属性白名单。
// 故意不包含 position / background 等：
//   - background 可以用 url() 引入外部资源，属于攻击面；
//   - position / top / left 可以让元素跑到页面任意位置，属于点击劫持帮凶。
//
// 注意：zoom 不在这里。它不是标准 CSS 属性（Firefox 直到 126 才支持），
// 因此不直接透传，而是由 sanitizeStyle 转换成等价的 width 百分比。
var allowedStyleProps = map[string]bool{
	"width":         true,
	"height":        true,
	"max-width":     true,
	"max-height":    true,
	"object-fit":    true,
	"border-radius": true,
}

// safeStyleValuePattern 限制样式值只能是数字/单位/百分号等安全字符。
// 故意不含括号：老版本 IE 的 CSS `expression()` 可以执行脚本，
// 而 width/height/max-width 这类尺寸属性本来也不需要函数语法。
var safeStyleValuePattern = regexp.MustCompile(`^[0-9a-zA-Z.%\s,#-]+$`)

// embeddedImage 是从正文里抽取出来的一张图片。
type embeddedImage struct {
	URL    string
	Alt    string
	Title  string
	Width  string
	Height string
	Style  string
}

// prepareEmbeddedImages 预处理正文：抽出 HTML 图片与 [image](url) 伪链接，
// 换成占位符，返回处理后的正文以及「占位符 -> <img> 标签」的映射。
func prepareEmbeddedImages(source string) (string, map[string]string) {
	images := make(map[string]string)

	source = htmlImagePattern.ReplaceAllStringFunc(source, func(tag string) string {
		if len(images) >= maxEmbeddedImages {
			return ""
		}
		match := htmlImagePattern.FindStringSubmatch(tag)
		if len(match) < 2 {
			return ""
		}
		attrs := parseHTMLAttributes(match[1])

		img, ok := buildEmbeddedImage(attrs)
		if !ok {
			return ""
		}
		return registerEmbeddedImage(images, img)
	})

	source = pseudoImageLinkPattern.ReplaceAllStringFunc(source, func(link string) string {
		if len(images) >= maxEmbeddedImages {
			return link
		}
		match := pseudoImageLinkPattern.FindStringSubmatch(link)
		if len(match) < 2 {
			return link
		}
		img, ok := buildEmbeddedImage(map[string]string{"src": match[1]})
		if !ok {
			return link
		}
		return registerEmbeddedImage(images, img)
	})

	return source, images
}

func registerEmbeddedImage(images map[string]string, img embeddedImage) string {
	placeholder := imagePlaceholderPrefix + strconv.Itoa(len(images)) + imagePlaceholderSuffix
	images[placeholder] = img.html()
	// 图片单独成段，避免被当成行内元素挤在文字中间
	return "\n\n" + placeholder + "\n\n"
}

// buildEmbeddedImage 按白名单构造图片；URL 不合法时返回 false（该标签被丢弃）。
func buildEmbeddedImage(attrs map[string]string) (embeddedImage, bool) {
	url, ok := safeImageURL(attrs["src"])
	if !ok {
		return embeddedImage{}, false
	}

	img := embeddedImage{
		URL:    url,
		Alt:    strings.TrimSpace(attrs["alt"]),
		Title:  strings.TrimSpace(attrs["title"]),
		Width:  safeDimension(attrs["width"]),
		Height: safeDimension(attrs["height"]),
		Style:  sanitizeStyle(attrs["style"]),
	}

	// 没有 alt 时用文件名兜底，保证无障碍与「图片加载失败」时仍有信息
	if img.Alt == "" {
		img.Alt = fileNameFromURL(url)
	}
	return img, true
}

// html 输出最终的 <img> 标签。所有属性都已过白名单，因此可以安全地作为 HTML 输出。
func (img embeddedImage) html() string {
	var b strings.Builder
	b.WriteString(`<img src="`)
	b.WriteString(escapeAttr(img.URL))
	b.WriteString(`" alt="`)
	b.WriteString(escapeAttr(img.Alt))
	b.WriteString(`"`)

	if img.Title != "" {
		b.WriteString(` title="`)
		b.WriteString(escapeAttr(img.Title))
		b.WriteString(`"`)
	}
	if img.Width != "" {
		b.WriteString(` width="`)
		b.WriteString(escapeAttr(img.Width))
		b.WriteString(`"`)
	}
	if img.Height != "" {
		b.WriteString(` height="`)
		b.WriteString(escapeAttr(img.Height))
		b.WriteString(`"`)
	}
	if img.Style != "" {
		b.WriteString(` style="`)
		b.WriteString(escapeAttr(img.Style))
		b.WriteString(`"`)
	}

	// 与 markdown 图片保持一致的处理方式
	b.WriteString(` loading="lazy" decoding="async" referrerpolicy="no-referrer">`)
	return b.String()
}

// parseHTMLAttributes 把标签属性解析成 map（键统一小写）。
func parseHTMLAttributes(raw string) map[string]string {
	attrs := make(map[string]string)
	for _, match := range htmlAttributePattern.FindAllStringSubmatch(raw, -1) {
		name := strings.ToLower(match[1])
		value := strings.Trim(match[2], `"'`)
		attrs[name] = value
	}
	return attrs
}

// safeImageURL 校验图片地址，拒绝 javascript:/data:/vbscript: 等危险协议。
func safeImageURL(raw string) (string, bool) {
	url := strings.TrimSpace(raw)
	if url == "" {
		return "", false
	}
	// 过滤控制字符与可能打断属性/标签的字符
	for _, r := range url {
		if r < 0x20 || r == 0x7f {
			return "", false
		}
	}
	if strings.ContainsAny(url, `<>"'`) {
		return "", false
	}
	// 拒绝协议相对地址（//evil.com/a.png）：它等价于引用外部域名，
	// 与「站内路径」的意图不符，容易被误用。
	if strings.HasPrefix(url, "//") {
		return "", false
	}
	if !safeImageURLPattern.MatchString(url) {
		return "", false
	}
	return url, true
}

// safeDimension 校验 width/height 属性值，只接受纯数字。
func safeDimension(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if _, err := strconv.Atoi(value); err != nil {
		return ""
	}
	return value
}

// sanitizeStyle 只保留白名单里的样式属性，且值必须匹配安全字符集。
//
// 特殊处理 zoom：Typora 以及从网页/AI 复制的内容常用
//
//	<img src="..." style="zoom:10%;">
//
// 表示「按原尺寸的 10% 显示」。CSS 的 zoom 是非标准属性，兼容性差
// （Firefox 126 之前不支持），而且语义是缩放整个元素盒模型。
// 这里把它翻译成等价的 width 百分比，各浏览器表现一致：
//
//	zoom:10%  -> width:10%
//	zoom:0.1  -> width:10%
//	zoom:2    -> width:200%
func sanitizeStyle(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}

	props := make(map[string]string, 4)
	order := make([]string, 0, 4)

	setProp := func(prop, value string) {
		if _, exists := props[prop]; exists {
			return // 同名属性以先出现的为准
		}
		props[prop] = value
		order = append(order, prop)
	}

	// zoom 先记下来，等扫描完再决定是否采用：显式 width 的优先级更高
	zoomWidth := ""
	for _, match := range stylePropPattern.FindAllStringSubmatch(raw, -1) {
		prop := strings.ToLower(strings.TrimSpace(match[1]))
		value := strings.TrimSpace(match[2])

		if prop == "zoom" {
			if zoomWidth == "" {
				if percent, ok := zoomToWidthPercent(value); ok {
					zoomWidth = percent
				}
			}
			continue
		}

		if !allowedStyleProps[prop] {
			continue
		}
		if !safeStyleValuePattern.MatchString(value) {
			continue
		}
		setProp(prop, value)
	}

	parts := make([]string, 0, len(order)+1)
	if zoomWidth != "" {
		if _, hasWidth := props["width"]; !hasWidth {
			parts = append(parts, "width:"+zoomWidth)
		}
	}
	for _, prop := range order {
		parts = append(parts, prop+":"+props[prop])
	}
	return strings.Join(parts, ";")
}

// zoomToWidthPercent 把 zoom 的值换算成 width 百分比字符串。
// 支持 `10%`（带百分号）与 `0.1`/`2`（倍率）两种写法；
// 非法值、非正数或超过 500% 的值一律丢弃，避免生成荒唐的尺寸。
func zoomToWidthPercent(value string) (string, bool) {
	raw := strings.TrimSpace(strings.ToLower(value))
	if raw == "" {
		return "", false
	}

	isPercent := strings.HasSuffix(raw, "%")
	if isPercent {
		raw = strings.TrimSpace(strings.TrimSuffix(raw, "%"))
	}

	number, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return "", false
	}
	// 倍率写法换算成百分比：0.5 -> 50%
	if !isPercent {
		number *= 100
	}

	if number <= 0 || number > 500 {
		return "", false
	}

	return strconv.FormatFloat(number, 'f', -1, 64) + "%", true
}

// escapeAttr 转义属性值，避免引号/尖括号破坏标签结构。
func escapeAttr(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		`"`, "&quot;",
		"'", "&#39;",
		"<", "&lt;",
		">", "&gt;",
	)
	return replacer.Replace(value)
}

// fileNameFromURL 从图片地址里取一个文件名，用于 alt 兜底。
// 有些图片服务（如 AI 生成的图片链接）文件名是一长串 hash，
// 直接拿来当 alt 反而更丑，这种情况统一降级为「图片」。
func fileNameFromURL(url string) string {
	if i := strings.IndexAny(url, "?#"); i >= 0 {
		url = url[:i]
	}

	name := ""
	if i := strings.LastIndexByte(url, '/'); i >= 0 && i+1 < len(url) {
		name = url[i+1:]
	}
	if name == "" || len(name) > 60 {
		return "图片"
	}
	// 形如 rsc-4/K1Z4K25s... 的 hash，没有语义
	if isHashLike(name) {
		return "图片"
	}
	return name
}

// isHashLike 判断文件名是否像一段 hash（没有可读语义）。
func isHashLike(name string) bool {
	stem := name
	if i := strings.LastIndexByte(stem, '.'); i > 0 {
		stem = stem[:i]
	}
	if len(stem) < 12 {
		return false
	}

	hasLetter, hasDigit := false, false
	for _, r := range stem {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			hasLetter = true
		case r >= '0' && r <= '9':
			hasDigit = true
		default:
			return false
		}
	}
	return hasLetter && hasDigit
}

// stripHTMLImages 把正文里的 HTML 图片替换成 alt 文本，用于生成纯文本摘要。
func stripHTMLImages(source string) string {
	return htmlImagePattern.ReplaceAllStringFunc(source, func(tag string) string {
		match := htmlImagePattern.FindStringSubmatch(tag)
		if len(match) < 2 {
			return " "
		}
		alt := strings.TrimSpace(parseHTMLAttributes(match[1])["alt"])
		if alt == "" {
			return " "
		}
		return " " + alt + " "
	})
}
