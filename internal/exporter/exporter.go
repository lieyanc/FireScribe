package exporter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf16"
)

// Annotation is the export-facing, immutable snapshot of a review annotation.
// Text offsets are rune offsets so Chinese anchors remain stable across
// renderers and are not accidentally interpreted as UTF-8 byte offsets.
type Annotation struct {
	Kind       string
	Status     string
	Body       string
	AnchorType string
	Start      int
	End        int
	AnchorText string
	X          float64
	Y          float64
	Width      float64
	Height     float64
}

type PageText struct {
	PageNo      int
	Text        string
	VersionID   string
	VersionKind string
	Image       []byte
	Annotations []Annotation
}

type Options struct {
	Format             string
	IncludePageNumbers bool
	IncludeAnnotations bool
	IncludeUncertain   bool
}

// Render preserves the original TXT/Markdown API while newer callers can use
// RenderWithOptions for DOCX/PDF and review metadata.
func Render(title string, pages []PageText, format string, includePageNumbers bool) ([]byte, error) {
	return RenderWithOptions(title, pages, Options{Format: format, IncludePageNumbers: includePageNumbers})
}

func RenderWithOptions(title string, pages []PageText, options Options) ([]byte, error) {
	format := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(options.Format), "."))
	if format == "markdown" {
		format = "md"
	}
	prepared := preparePages(pages, options)
	switch format {
	case "txt":
		return renderTXT(prepared, options.IncludePageNumbers), nil
	case "md":
		return renderMarkdown(title, prepared, options.IncludePageNumbers), nil
	case "docx":
		return renderDOCX(title, prepared, options.IncludePageNumbers)
	case "pdf":
		return renderPDF(title, prepared, options.IncludePageNumbers)
	default:
		return nil, fmt.Errorf("unsupported export format %q", options.Format)
	}
}

type preparedPage struct {
	PageNo  int
	Text    string
	Image   []byte
	Notes   []string
	Regions []Annotation
}

func preparePages(pages []PageText, options Options) []preparedPage {
	prepared := make([]preparedPage, 0, len(pages))
	for _, page := range pages {
		originalText := strings.TrimSpace(page.Text)
		text := originalText
		if options.IncludeUncertain {
			text = insertUncertainMarkers(text, page.Annotations)
		}
		notes := make([]string, 0)
		regions := make([]Annotation, 0)
		for _, annotation := range page.Annotations {
			switch annotation.Kind {
			case "page_note":
				if options.IncludeAnnotations {
					notes = append(notes, formatAnnotationNote("批注", annotation))
				}
			case "page_region":
				if options.IncludeAnnotations {
					regions = append(regions, annotation)
					notes = append(notes, formatAnnotationNote(fmt.Sprintf("区域批注 #%d", len(regions)), annotation))
				}
			case "uncertain_text":
				if options.IncludeUncertain && !hasValidRange(annotation, originalText) {
					notes = append(notes, formatAnnotationNote("存疑", annotation))
				}
			}
		}
		prepared = append(prepared, preparedPage{PageNo: page.PageNo, Text: text, Image: page.Image, Notes: notes, Regions: regions})
	}
	return prepared
}

func insertUncertainMarkers(text string, annotations []Annotation) string {
	runes := []rune(text)
	type marker struct {
		start int
		end   int
		body  string
	}
	markers := make([]marker, 0)
	for _, annotation := range annotations {
		if annotation.Kind != "uncertain_text" || annotation.Status != "open" || annotation.Start < 0 || annotation.End <= annotation.Start || annotation.End > len(runes) {
			continue
		}
		markers = append(markers, marker{start: annotation.Start, end: annotation.End, body: strings.TrimSpace(annotation.Body)})
	}
	// Applying from the end keeps every stored offset valid. For overlapping
	// anchors, the more specific later range is inserted first and both markers
	// remain visible without corrupting the transcription text.
	sort.SliceStable(markers, func(i, j int) bool {
		if markers[i].end != markers[j].end {
			return markers[i].end > markers[j].end
		}
		return markers[i].start > markers[j].start
	})
	for _, current := range markers {
		label := "存疑"
		if current.body != "" && current.body != "存疑" {
			label += "：" + current.body
		}
		insert := []rune("〔" + label + "〕")
		runes = append(runes[:current.end], append(insert, runes[current.end:]...)...)
	}
	return string(runes)
}

func hasValidRange(annotation Annotation, text string) bool {
	return annotation.Status == "open" && annotation.Start >= 0 && annotation.End > annotation.Start && annotation.End <= len([]rune(text))
}

func formatAnnotationNote(label string, annotation Annotation) string {
	body := strings.TrimSpace(annotation.Body)
	if body == "" {
		body = label
	}
	status := annotationStatusLabel(annotation.Status)
	if annotation.AnchorType == "page_region" || annotation.AnchorType == "text_region_link" {
		anchorText := ""
		if strings.TrimSpace(annotation.AnchorText) != "" {
			anchorText = "，原文：" + strings.TrimSpace(annotation.AnchorText)
		}
		return fmt.Sprintf("%s（%s%s，x=%.0f, y=%.0f, w=%.0f, h=%.0f）：%s", label, status, anchorText, annotation.X, annotation.Y, annotation.Width, annotation.Height, body)
	}
	if strings.TrimSpace(annotation.AnchorText) != "" {
		return fmt.Sprintf("%s（%s，原文：%s）：%s", label, status, strings.TrimSpace(annotation.AnchorText), body)
	}
	return fmt.Sprintf("%s（%s）：%s", label, status, body)
}

func annotationStatusLabel(status string) string {
	switch status {
	case "resolved":
		return "已解决"
	case "ignored":
		return "已忽略"
	default:
		return "待处理"
	}
}

func renderTXT(pages []preparedPage, includePageNumbers bool) []byte {
	var buf bytes.Buffer
	for i, page := range pages {
		if i > 0 {
			buf.WriteString("\n\n")
		}
		if includePageNumbers {
			fmt.Fprintf(&buf, "[第 %d 页]\n\n", page.PageNo)
		}
		buf.WriteString(strings.TrimSpace(page.Text))
		writeTXTNotes(&buf, page.Notes)
	}
	buf.WriteString("\n")
	return buf.Bytes()
}

func writeTXTNotes(buf *bytes.Buffer, notes []string) {
	if len(notes) == 0 {
		return
	}
	buf.WriteString("\n\n[审校记录]\n")
	for _, note := range notes {
		buf.WriteString("- ")
		buf.WriteString(note)
		buf.WriteByte('\n')
	}
}

func renderMarkdown(title string, pages []preparedPage, includePageNumbers bool) []byte {
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
		if len(page.Notes) > 0 {
			buf.WriteString("\n\n### 审校记录\n\n")
			for _, note := range page.Notes {
				fmt.Fprintf(&buf, "- %s\n", note)
			}
		}
	}
	buf.WriteString("\n")
	return buf.Bytes()
}

type Anchor struct {
	Type   string
	Start  int
	End    int
	Text   string
	X      float64
	Y      float64
	Width  float64
	Height float64
}

// ParseAnnotationAnchor understands both text-range and page-region anchors.
// Invalid/legacy anchors simply become page-level notes.
func ParseAnnotationAnchor(raw string) Anchor {
	var anchor struct {
		Type   string  `json:"type"`
		Start  int     `json:"start"`
		End    int     `json:"end"`
		Text   string  `json:"text"`
		X      float64 `json:"x"`
		Y      float64 `json:"y"`
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
		Region struct {
			X      float64 `json:"x"`
			Y      float64 `json:"y"`
			Width  float64 `json:"width"`
			Height float64 `json:"height"`
		} `json:"region"`
	}
	if json.Unmarshal([]byte(raw), &anchor) != nil {
		return Anchor{Start: -1, End: -1}
	}
	switch anchor.Type {
	case "text_range":
		if anchor.End <= anchor.Start || anchor.Start < 0 {
			return Anchor{Start: -1, End: -1}
		}
		return Anchor{Type: anchor.Type, Start: anchor.Start, End: anchor.End, Text: anchor.Text}
	case "page_region":
		if anchor.Width <= 0 || anchor.Height <= 0 || anchor.X < 0 || anchor.Y < 0 {
			return Anchor{Start: -1, End: -1}
		}
		return Anchor{Type: anchor.Type, Start: -1, End: -1, X: anchor.X, Y: anchor.Y, Width: anchor.Width, Height: anchor.Height}
	case "text_region_link":
		if anchor.End <= anchor.Start || anchor.Start < 0 || anchor.Region.Width <= 0 || anchor.Region.Height <= 0 || anchor.Region.X < 0 || anchor.Region.Y < 0 {
			return Anchor{Start: -1, End: -1}
		}
		return Anchor{
			Type: anchor.Type, Start: anchor.Start, End: anchor.End, Text: anchor.Text,
			X: anchor.Region.X, Y: anchor.Region.Y, Width: anchor.Region.Width, Height: anchor.Region.Height,
		}
	default:
		return Anchor{Start: -1, End: -1}
	}
}

// ResolveTextRange converts the browser's UTF-16 textarea offsets to Go rune
// offsets and falls back to the captured anchor text if the page was edited
// after the annotation was created.
func ResolveTextRange(text string, startUTF16, endUTF16 int, anchorText string) (int, int) {
	runes := []rune(text)
	start := utf16OffsetToRuneIndex(runes, startUTF16)
	end := utf16OffsetToRuneIndex(runes, endUTF16)
	if start >= 0 && end > start && end <= len(runes) {
		if anchorText == "" || string(runes[start:end]) == anchorText {
			return start, end
		}
	}
	if anchorText != "" {
		if index := strings.Index(text, anchorText); index >= 0 {
			prefix := []rune(text[:index])
			return len(prefix), len(prefix) + len([]rune(anchorText))
		}
	}
	return -1, -1
}

func utf16OffsetToRuneIndex(runes []rune, offset int) int {
	if offset < 0 {
		return -1
	}
	units := 0
	for index, r := range runes {
		if units == offset {
			return index
		}
		units += len(utf16.Encode([]rune{r}))
		if units > offset {
			return -1
		}
	}
	if units == offset {
		return len(runes)
	}
	return -1
}
