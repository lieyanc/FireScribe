package exporter

import (
	"bytes"
	"fmt"
	"strings"
)

type PageText struct {
	PageNo int
	Text   string
}

func Render(title string, pages []PageText, format string, includePageNumbers bool) ([]byte, error) {
	switch strings.ToLower(format) {
	case "txt":
		return renderTXT(pages, includePageNumbers), nil
	case "md", "markdown":
		return renderMarkdown(title, pages, includePageNumbers), nil
	default:
		return nil, fmt.Errorf("unsupported export format %q", format)
	}
}

func renderTXT(pages []PageText, includePageNumbers bool) []byte {
	var buf bytes.Buffer
	for i, page := range pages {
		if i > 0 {
			buf.WriteString("\n\n")
		}
		if includePageNumbers {
			fmt.Fprintf(&buf, "[第 %d 页]\n\n", page.PageNo)
		}
		buf.WriteString(strings.TrimSpace(page.Text))
	}
	buf.WriteString("\n")
	return buf.Bytes()
}

func renderMarkdown(title string, pages []PageText, includePageNumbers bool) []byte {
	var buf bytes.Buffer
	if strings.TrimSpace(title) != "" {
		fmt.Fprintf(&buf, "# %s\n\n", title)
	}
	for i, page := range pages {
		if i > 0 {
			buf.WriteString("\n\n")
		}
		if includePageNumbers {
			fmt.Fprintf(&buf, "## 第 %d 页\n\n", page.PageNo)
		}
		buf.WriteString(strings.TrimSpace(page.Text))
	}
	buf.WriteString("\n")
	return buf.Bytes()
}
