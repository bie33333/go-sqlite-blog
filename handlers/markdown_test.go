package handlers

import (
	"strings"
	"testing"
)

func TestRenderMarkdownEmbeddedImages(t *testing.T) {
	tests := []struct {
		name        string
		source      string
		wantContain []string
		wantNotHave []string
	}{
		{
			name:   "HTML img 带 style 与 title",
			source: `<img src="https://images.example.com/a.png" alt="架构图" style="width: 40%" title="图1">`,
			wantContain: []string{
				`src="https://images.example.com/a.png"`,
				`alt="架构图"`,
				`title="图1"`,
				`style="width:40%"`,
				`loading="lazy"`,
				`referrerpolicy="no-referrer"`,
			},
		},
		{
			name:        "zoom 不再被剔除，而是转成 width",
			source:      `<img src="/uploads/a.png" style="zoom: 30%;">`,
			wantContain: []string{`style="width:30%"`},
			wantNotHave: []string{"zoom"},
		},
		{
			name:        "background 被剔除",
			source:      `<img src="/uploads/a.png" style="zoom: 5%; background: url(http://evil.com/x.png); width: 50%">`,
			wantContain: []string{`style="width:50%"`},
			wantNotHave: []string{"background", "evil.com"},
		},
		{
			name:        "自闭合写法",
			source:      `<img src="https://example.com/b.gif" alt="x" />`,
			wantContain: []string{`src="https://example.com/b.gif"`, `alt="x"`},
		},
		{
			name:        "单引号属性",
			source:      `<img src='https://example.com/c.png' alt='单引号'>`,
			wantContain: []string{`src="https://example.com/c.png"`, `alt="单引号"`},
		},
		{
			name:        "无引号属性",
			source:      `<img src=https://example.com/d.png alt=noquote>`,
			wantContain: []string{`src="https://example.com/d.png"`, `alt="noquote"`},
		},
		{
			name:        "javascript 协议被拒绝",
			source:      `<img src="javascript:alert(1)">`,
			wantNotHave: []string{"javascript", "<img"},
		},
		{
			name:        "data 协议被拒绝",
			source:      `<img src="data:image/svg+xml,<svg onload=alert(1)>">`,
			wantNotHave: []string{"<svg", "data:image"},
		},
		{
			name:        "onerror 事件被丢弃",
			source:      `<img src="/uploads/a.png" onerror="alert(1)" alt="x">`,
			wantContain: []string{`src="/uploads/a.png"`},
			wantNotHave: []string{"onerror", "alert"},
		},
		{
			// goldmark 把 HTML block 渲染成 <!-- raw HTML omitted -->，
			// 因此这里只断言 script 内容不会出现；正文用空行隔开才能保留。
			name:        "script 标签依旧被丢弃",
			source:      "<script>alert(1)</script>\n\n正文",
			wantContain: []string{"正文"},
			wantNotHave: []string{"<script", "alert(1)"},
		},
		{
			name:        "iframe 依旧被丢弃",
			source:      "<iframe src=\"https://evil.com\"></iframe>\n\n正文",
			wantContain: []string{"正文"},
			wantNotHave: []string{"<iframe"},
		},
		{
			name:        "伪链接 [image](url) 变成图片",
			source:      `[image](https://images.openai.com/x.png?purpose=inline)`,
			wantContain: []string{`<img src="https://images.openai.com/x.png?purpose=inline"`},
			wantNotHave: []string{"[image]"},
		},
		{
			name:        "普通链接不会被改成图片",
			source:      `[点这里](https://example.com/page)`,
			wantContain: []string{`<a href="https://example.com/page"`},
			wantNotHave: []string{"<img"},
		},
		{
			name:        "markdown 图片语法仍然正常",
			source:      `![说明](/uploads/demo.png)`,
			wantContain: []string{`<img src="/uploads/demo.png"`, `loading="lazy"`},
		},
		{
			name:        "width 属性限制为数字",
			source:      `<img src="/uploads/a.png" width="40" height="abc">`,
			wantContain: []string{`width="40"`},
			wantNotHave: []string{`height=`},
		},
		{
			name:        "alt 缺失时用文件名兜底",
			source:      `<img src="https://example.com/pic.png">`,
			wantContain: []string{`alt="pic.png"`},
		},
		{
			name:        "长 hash 文件名兜底为「图片」",
			source:      `<img src="https://cdn.example.com/K1Z4K25s7d5zmxQbyFiK2owJ66zemtCt.png">`,
			wantContain: []string{`alt="图片"`},
		},
		{
			name:        "HTML 图片单独成段",
			source:      "文字前\n<img src=\"/uploads/a.png\" alt=\"图\">\n文字后",
			wantContain: []string{`<img src="/uploads/a.png"`, "文字前", "文字后"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := renderMarkdown(tt.source)
			if err != nil {
				t.Fatalf("renderMarkdown() error = %v", err)
			}
			out := string(got)

			for _, want := range tt.wantContain {
				if !strings.Contains(out, want) {
					t.Errorf("输出缺少 %q\n实际输出: %s", want, out)
				}
			}
			for _, bad := range tt.wantNotHave {
				if strings.Contains(out, bad) {
					t.Errorf("输出不应包含 %q\n实际输出: %s", bad, out)
				}
			}
		})
	}
}

func TestRenderMarkdownMermaidFence(t *testing.T) {
	source := "```mermaid\nflowchart LR\n    A[开始] --> B[结束]\n```"

	got, err := renderMarkdown(source)
	if err != nil {
		t.Fatalf("renderMarkdown() error = %v", err)
	}

	out := string(got)
	if !strings.Contains(out, `<code class="language-mermaid">`) {
		t.Fatalf("Mermaid 代码块应保留 language-mermaid class，实际输出: %s", out)
	}
	if !strings.Contains(out, "A[开始] --&gt; B[结束]") {
		t.Errorf("Mermaid 源码应安全地作为代码文本输出，实际输出: %s", out)
	}
}

func TestExcerptStripsEmbeddedImages(t *testing.T) {
	content := "开头文字\n\n<img src=\"/uploads/a.png\" alt=\"架构图\">\n\n结尾文字"
	got := excerpt(content, 100)

	if strings.Contains(got, imagePlaceholderPrefix) {
		t.Errorf("摘要里不应出现占位符: %q", got)
	}
	if strings.Contains(got, "<img") {
		t.Errorf("摘要里不应出现标签: %q", got)
	}
	if !strings.Contains(got, "开头文字") {
		t.Errorf("摘要应包含正文: %q", got)
	}
}

func TestSanitizeStyle(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"width: 40%", "width:40%"},
		{"WIDTH: 300px; HEIGHT: auto", "width:300px;height:auto"},
		{"width: 40%; color: red", "width:40%"},
		{"background: url(x)", ""},
		{"width: expression(alert(1))", ""},
		{"max-width: 100%; border-radius: 8px", "max-width:100%;border-radius:8px"},

		// zoom 转换：Typora / 网页复制的写法
		{"zoom:10%", "width:10%"},
		{"zoom: 10%", "width:10%"},
		{"zoom:50%;", "width:50%"},
		{"zoom: 0.1", "width:10%"},
		{"zoom: 0.5", "width:50%"},
		{"zoom: 2", "width:200%"},
		{"zoom: 100%", "width:100%"},
		{"ZOOM: 40%", "width:40%"},

		// 非法 zoom 一律丢弃
		{"zoom: 0", ""},
		{"zoom: 0%", ""},
		{"zoom: -10%", ""},
		{"zoom: abc", ""},
		{"zoom: 600%", ""},
		{"zoom:", ""},

		// zoom 与 width 同时出现时，显式 width 优先
		{"zoom:10%; width:200px", "width:200px"},
		{"width:200px; zoom:10%", "width:200px"},

		// zoom 与其它白名单属性共存
		{"zoom:30%; border-radius:4px", "width:30%;border-radius:4px"},
		{"zoom:30%; color:red", "width:30%"},
	}

	for _, tt := range tests {
		if got := sanitizeStyle(tt.in); got != tt.want {
			t.Errorf("sanitizeStyle(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestZoomRendersAsWidth(t *testing.T) {
	got, err := renderMarkdown(`<img src="https://images.example.com/a.png" alt="封面" style="zoom:10%;">`)
	if err != nil {
		t.Fatalf("renderMarkdown() error = %v", err)
	}
	out := string(got)

	if !strings.Contains(out, `style="width:10%"`) {
		t.Errorf("期望输出 width:10%%，实际: %s", out)
	}
	// 不应把非标准的 zoom 透传给浏览器
	if strings.Contains(out, "zoom") {
		t.Errorf("输出不应包含 zoom: %s", out)
	}
}

func TestSafeImageURL(t *testing.T) {
	allowed := []string{
		"https://example.com/a.png",
		"http://example.com/a.png",
		"/uploads/a.png",
		"./a.png",
	}
	for _, url := range allowed {
		if _, ok := safeImageURL(url); !ok {
			t.Errorf("safeImageURL(%q) 应当被允许", url)
		}
	}

	blocked := []string{
		"",
		"javascript:alert(1)",
		"JavaScript:alert(1)",
		"data:image/png;base64,xxx",
		"vbscript:msgbox",
		"//evil.com/a.png",
		"https://example.com/a.png\" onerror=\"alert(1)",
		"https://example.com/a\n.png",
	}
	for _, url := range blocked {
		if _, ok := safeImageURL(url); ok {
			t.Errorf("safeImageURL(%q) 应当被拒绝", url)
		}
	}
}
