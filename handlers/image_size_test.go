package handlers

import (
	"strings"
	"testing"
)

func TestMarkdownImageSizeAttributes(t *testing.T) {
	tests := []struct {
		name        string
		source      string
		wantContain []string
		wantNotHave []string
	}{
		{
			name:        "width 百分比",
			source:      `![封面](https://example.com/a.png){width=40%}`,
			wantContain: []string{`style="width:40%"`},
			wantNotHave: []string{"{width", "40%}"},
		},
		{
			name:        "width 纯数字按 px",
			source:      `![封面](https://example.com/a.png){width=400}`,
			wantContain: []string{`style="width:400px"`},
			wantNotHave: []string{"{width"},
		},
		{
			name:        "zoom 转成 width",
			source:      `![封面](https://example.com/a.png){zoom=25%}`,
			wantContain: []string{`style="width:25%"`},
			wantNotHave: []string{"zoom", "{zoom"},
		},
		{
			name:        "width 与 height 同时给出",
			source:      `![封面](https://example.com/a.png){width=400, height=200}`,
			wantContain: []string{`style="width:400px;height:200px"`},
		},
		{
			name:        "简写 w/h",
			source:      `![封面](https://example.com/a.png){w=50% h=30%}`,
			wantContain: []string{`style="width:50%;height:30%"`},
		},
		{
			name:        "zoom 与 width 同时给出时 width 优先",
			source:      `![封面](https://example.com/a.png){zoom=10%, width=300px}`,
			wantContain: []string{`style="width:300px"`},
			wantNotHave: []string{"zoom", "10%"},
		},
		{
			name:        "非法值被忽略，花括号保留为正文",
			source:      `![封面](https://example.com/a.png){color=red}`,
			wantContain: []string{"{color=red}"},
			wantNotHave: []string{"style="},
		},
		{
			name:        "缺少闭合括号时不处理",
			source:      `![封面](https://example.com/a.png){width=40%`,
			wantContain: []string{"{width=40%"},
		},
		{
			name:        "花括号后还有正文时正文保留",
			source:      `![封面](https://example.com/a.png){width=40%}后面的文字`,
			wantContain: []string{`style="width:40%"`, "后面的文字"},
			wantNotHave: []string{"{width"},
		},
		{
			name:        "普通图片不受影响",
			source:      `![封面](https://example.com/a.png)`,
			wantContain: []string{`<img src="https://example.com/a.png"`, `loading="lazy"`},
			wantNotHave: []string{"style="},
		},
		{
			name:        "注入尝试被拒绝",
			source:      `![封面](https://example.com/a.png){width=40%; color:red}`,
			wantContain: []string{`style="width:40%"`},
			wantNotHave: []string{"color:red"},
		},
		{
			name:        "代码块里的花括号原样保留",
			source:      "```\n![a](b.png){width=40%}\n```",
			wantContain: []string{"{width=40%}"},
			wantNotHave: []string{"style="},
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

func TestNormalizeSizeValue(t *testing.T) {
	valid := map[string]string{
		"400":   "400px",
		"40%":   "40%",
		"12.5%": "12.5%",
		"300px": "300px",
		"2rem":  "2rem",
		"50vw":  "50vw",
	}
	for in, want := range valid {
		got, ok := normalizeSizeValue(in)
		if !ok || got != want {
			t.Errorf("normalizeSizeValue(%q) = (%q, %v), want (%q, true)", in, got, ok, want)
		}
	}

	invalid := []string{"", "abc", "40 %", "calc(100%)", "40px;", "-10%", "expression(1)"}
	for _, in := range invalid {
		if got, ok := normalizeSizeValue(in); ok {
			t.Errorf("normalizeSizeValue(%q) 应当被拒绝，实际 = %q", in, got)
		}
	}
}

func TestParseSizeAttributes(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"width=40%", "width:40%"},
		{"width=400", "width:400px"},
		{"zoom=25%", "width:25%"},
		{"w=50% h=30%", "width:50%;height:30%"},
		{"width=400, height=200", "width:400px;height:200px"},
		{"zoom=10%, width=300px", "width:300px"},
		{"width=40%; color:red", "width:40%"},
		{"color=red", ""},
		{"", ""},
	}

	for _, tt := range tests {
		if got := parseSizeAttributes(tt.in); got != tt.want {
			t.Errorf("parseSizeAttributes(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
