package markdown

import (
	"bytes"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

// RenderMarkdownToHTML 将 Markdown 转换为 HTML fragment。
// 该函数默认不启用 raw HTML 渲染，避免用户输入的危险 HTML 直通输出。
func RenderMarkdownToHTML(md string) (string, error) {
	renderer := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
	)

	var output bytes.Buffer
	if err := renderer.Convert([]byte(md), &output); err != nil {
		return "", err
	}

	return output.String(), nil
}
