package markdown

import (
	"strings"
	"testing"
)

func TestRenderMarkdownToHTML(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains []string
	}{
		{
			name:     "标题",
			input:    "# 测试标题",
			contains: []string{"<h1>测试标题</h1>"},
		},
		{
			name:     "粗体",
			input:    "这是 **粗体** 文本",
			contains: []string{"<strong>粗体</strong>"},
		},
		{
			name:  "列表",
			input: "- 项目1\n- 项目2",
			contains: []string{
				"<ul>",
				"<li>项目1</li>",
				"<li>项目2</li>",
				"</ul>",
			},
		},
		{
			name:  "表格",
			input: "| 列1 | 列2 |\n| --- | --- |\n| A | B |",
			contains: []string{
				"<table>",
				"<th>列1</th>",
				"<th>列2</th>",
				"<td>A</td>",
				"<td>B</td>",
				"</table>",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			html, err := RenderMarkdownToHTML(tt.input)
			if err != nil {
				t.Fatalf("RenderMarkdownToHTML returned error: %v", err)
			}

			for _, item := range tt.contains {
				if !strings.Contains(html, item) {
					t.Fatalf("expected html to contain %q, actual: %s", item, html)
				}
			}
		})
	}
}

func TestRenderMarkdownToHTML_DisableRawHTML(t *testing.T) {
	html, err := RenderMarkdownToHTML("<script>alert('xss')</script>")
	if err != nil {
		t.Fatalf("RenderMarkdownToHTML returned error: %v", err)
	}

	if strings.Contains(html, "<script>") || strings.Contains(html, "</script>") {
		t.Fatalf("raw html should not be rendered, actual: %s", html)
	}
}
