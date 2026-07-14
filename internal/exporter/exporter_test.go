package exporter

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTextExportsIncludeSelectedReviewMetadata(t *testing.T) {
	pages := []PageText{{
		PageNo: 1,
		Text:   "火光文字",
		Annotations: []Annotation{
			{Kind: "uncertain_text", Status: "open", Body: "核对首词", Start: 0, End: 2, AnchorText: "火光", AnchorType: "text_range"},
			{Kind: "page_note", Status: "resolved", Body: "已与原稿核对"},
			{Kind: "page_region", Status: "open", Body: "边角字迹", AnchorType: "page_region", X: 12, Y: 34, Width: 56, Height: 78},
		},
	}}
	for _, format := range []string{"txt", "md"} {
		raw, err := RenderWithOptions("测试文稿", pages, Options{
			Format: format, IncludePageNumbers: true, IncludeAnnotations: true, IncludeUncertain: true,
		})
		if err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		output := string(raw)
		for _, want := range []string{"火光〔存疑：核对首词〕文字", "已与原稿核对", "区域批注", "x=12", "w=56"} {
			if !strings.Contains(output, want) {
				t.Fatalf("%s export missing %q: %s", format, want, output)
			}
		}
	}
}

func TestRenderDOCXProducesValidOOXMLPackage(t *testing.T) {
	raw, err := RenderWithOptions("中文手稿", []PageText{{
		PageNo: 3, Text: "第一段\n第二段",
		Annotations: []Annotation{{Kind: "page_note", Status: "open", Body: "请复核"}},
	}}, Options{Format: "docx", IncludePageNumbers: true, IncludeAnnotations: true})
	if err != nil {
		t.Fatal(err)
	}
	writeQAArtifact(t, "review-sample.docx", raw)
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("open DOCX zip: %v", err)
	}
	wantParts := map[string]bool{
		"[Content_Types].xml": false, "_rels/.rels": false, "word/document.xml": false,
		"word/styles.xml": false, "word/_rels/document.xml.rels": false, "word/footer1.xml": false,
	}
	var documentXML string
	for _, file := range reader.File {
		if _, ok := wantParts[file.Name]; ok {
			wantParts[file.Name] = true
		}
		if !strings.HasSuffix(file.Name, ".xml") && !strings.HasSuffix(file.Name, ".rels") {
			continue
		}
		content, err := readZipFile(file)
		if err != nil {
			t.Fatal(err)
		}
		decoder := xml.NewDecoder(bytes.NewReader(content))
		for {
			if _, err := decoder.Token(); err != nil {
				if err.Error() == "EOF" {
					break
				}
				t.Fatalf("invalid XML in %s: %v", file.Name, err)
			}
		}
		if file.Name == "word/document.xml" {
			documentXML = string(content)
		}
	}
	for part, found := range wantParts {
		if !found {
			t.Errorf("DOCX missing %s", part)
		}
	}
	for _, want := range []string{"中文手稿", "第 3 页", "第一段", "审校记录", "请复核"} {
		if !strings.Contains(documentXML, want) {
			t.Errorf("document.xml missing %q", want)
		}
	}
}

func TestRenderPDFWithChineseText(t *testing.T) {
	if _, err := findPDFFont(); err != nil {
		t.Skip(err)
	}
	var imageBytes bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 32, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 32; x++ {
			img.Set(x, y, color.RGBA{R: uint8(120 + x), G: uint8(80 + y), B: 60, A: 255})
		}
	}
	if err := png.Encode(&imageBytes, img); err != nil {
		t.Fatal(err)
	}
	raw, err := RenderWithOptions("中文审校版 FireScribe 2026", []PageText{{PageNo: 1, Text: "火光文字已经人工确认。编号 A-2026。", Image: imageBytes.Bytes()}}, Options{
		Format: "pdf", IncludePageNumbers: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	writeQAArtifact(t, "review-sample.pdf", raw)
	if len(raw) < 1000 || !bytes.HasPrefix(raw, []byte("%PDF-")) || !bytes.Contains(raw, []byte("/ToUnicode")) {
		t.Fatalf("unexpected PDF output (%d bytes)", len(raw))
	}
	if pdftotext, err := exec.LookPath("pdftotext"); err == nil {
		path := filepath.Join(t.TempDir(), "chinese.pdf")
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			t.Fatal(err)
		}
		output, err := exec.Command(pdftotext, path, "-").CombinedOutput()
		if err != nil {
			t.Fatalf("pdftotext: %v: %s", err, output)
		}
		extracted := string(output)
		if !strings.Contains(extracted, "火光文字已经人工确认") ||
			!strings.Contains(extracted, "FireScribe 2026") ||
			!strings.Contains(extracted, "A-2026") ||
			!strings.Contains(extracted, "原件第 1 页") {
			t.Fatalf("Chinese text was not extractable from PDF: %q", output)
		}
	}
}

func TestResolveTextRangeUsesBrowserUTF16Offsets(t *testing.T) {
	start, end := ResolveTextRange("A😀火光", 3, 5, "火光")
	if start != 2 || end != 4 {
		t.Fatalf("resolved range = %d:%d, want 2:4", start, end)
	}
}

func TestParseTextRegionLinkAnchorPreservesBothSides(t *testing.T) {
	anchor := ParseAnnotationAnchor(`{"type":"text_region_link","start":1,"end":3,"text":"手稿","region":{"x":10,"y":20,"width":30,"height":40}}`)
	if anchor.Type != "text_region_link" || anchor.Start != 1 || anchor.End != 3 || anchor.Text != "手稿" ||
		anchor.X != 10 || anchor.Y != 20 || anchor.Width != 30 || anchor.Height != 40 {
		t.Fatalf("linked anchor = %+v", anchor)
	}
}

func readZipFile(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	var output bytes.Buffer
	_, err = output.ReadFrom(reader)
	return output.Bytes(), err
}

func writeQAArtifact(t *testing.T, name string, raw []byte) {
	t.Helper()
	dir := strings.TrimSpace(os.Getenv("FIRESCRIBE_EXPORT_QA_DIR"))
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}
