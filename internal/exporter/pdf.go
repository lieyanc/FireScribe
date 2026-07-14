package exporter

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/go-pdf/fpdf"
	_ "golang.org/x/image/webp"
)

const pdfFontEnv = "FIRESCRIBE_PDF_FONT"

func renderPDF(title string, pages []preparedPage, includePageNumbers bool) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetTitle(title, true)
	pdf.SetAuthor("FireScribe", true)
	pdf.SetCreator("FireScribe", true)
	pdf.SetMargins(15, 15, 15)
	pdf.SetAutoPageBreak(true, 16)
	pdf.SetCompression(true)

	fontFamily, err := configurePDFFont(pdf, title, pages)
	if err != nil {
		return nil, err
	}
	pdf.SetFooterFunc(func() {
		pdf.SetY(-12)
		pdf.SetTextColor(120, 128, 135)
		// The commonly available Droid CJK fallback font does not contain a
		// complete Latin/digit repertoire. Page counters are ASCII-only, so use
		// a PDF core font and avoid tofu boxes in otherwise valid Chinese PDFs.
		pdf.SetFont("Helvetica", "", 8)
		pdf.CellFormat(0, 5, fmt.Sprintf("%d", pdf.PageNo()), "", 0, "R", false, 0, "")
	})

	for pageIndex, page := range pages {
		pdf.AddPage()
		pdf.SetTextColor(31, 77, 120)
		pdf.SetFont(fontFamily, "", 16)
		heading := strings.TrimSpace(title)
		if heading == "" {
			heading = "FireScribe 审校版"
		}
		writePDFMixedText(pdf, fontFamily, 16, 8, heading)
		pdf.Ln(8)
		if includePageNumbers {
			pdf.SetTextColor(46, 116, 181)
			writePDFMixedText(pdf, fontFamily, 11, 7, fmt.Sprintf("原件第 %d 页", page.PageNo))
			pdf.Ln(7)
		}
		pdf.Ln(1)

		if len(page.Image) > 0 {
			imageBytes, width, height, imageErr := normalizePDFImage(page.Image)
			if imageErr == nil {
				name := fmt.Sprintf("source-page-%d-%d", pageIndex, page.PageNo)
				info := pdf.RegisterImageOptionsReader(name, fpdf.ImageOptions{ImageType: "JPG", ReadDpi: true}, bytes.NewReader(imageBytes))
				if info != nil && pdf.Error() == nil {
					maxW, maxH := 180.0, 100.0
					displayW, displayH := maxW, maxW*float64(height)/float64(width)
					if displayH > maxH {
						displayH = maxH
						displayW = maxH * float64(width) / float64(height)
					}
					x := (210.0 - displayW) / 2
					y := pdf.GetY()
					pdf.ImageOptions(name, x, y, displayW, displayH, false, fpdf.ImageOptions{ImageType: "JPG", ReadDpi: true}, 0, "")
					if len(page.Regions) > 0 {
						pdf.SetDrawColor(214, 105, 0)
						pdf.SetTextColor(160, 72, 0)
						pdf.SetLineWidth(0.6)
						for index, region := range page.Regions {
							rx := x + region.X/float64(width)*displayW
							ry := y + region.Y/float64(height)*displayH
							rw := region.Width / float64(width) * displayW
							rh := region.Height / float64(height) * displayH
							if rw <= 0 || rh <= 0 {
								continue
							}
							pdf.Rect(rx, ry, rw, rh, "D")
							pdf.SetXY(rx, maxFloat64(y, ry-4))
							pdf.SetFont("Helvetica", "B", 8)
							pdf.CellFormat(7, 4, fmt.Sprintf("#%d", index+1), "", 0, "L", false, 0, "")
						}
						pdf.SetLineWidth(0.2)
					}
					pdf.SetY(y + displayH + 5)
				}
			}
		}

		pdf.SetTextColor(46, 116, 181)
		pdf.SetFont(fontFamily, "", 11)
		pdf.CellFormat(0, 7, "转录文本", "", 1, "L", false, 0, "")
		pdf.SetTextColor(32, 36, 40)
		text := strings.TrimSpace(page.Text)
		if text == "" {
			text = "（本页暂无文本）"
		}
		writePDFMixedText(pdf, fontFamily, 10.5, 6.2, text)
		pdf.Ln(6.2)

		if len(page.Notes) > 0 {
			pdf.Ln(2)
			pdf.SetFillColor(255, 247, 214)
			pdf.SetTextColor(122, 90, 0)
			pdf.SetFont(fontFamily, "", 10.5)
			pdf.CellFormat(0, 7, "审校记录", "", 1, "L", true, 0, "")
			pdf.SetFillColor(255, 247, 214)
			for _, note := range page.Notes {
				writePDFMixedText(pdf, fontFamily, 9.5, 5.6, "• "+note)
				pdf.Ln(5.6)
			}
		}
	}
	if err := pdf.Error(); err != nil {
		return nil, fmt.Errorf("render PDF: %w", err)
	}
	var output bytes.Buffer
	if err := pdf.Output(&output); err != nil {
		return nil, fmt.Errorf("write PDF: %w", err)
	}
	return output.Bytes(), nil
}

func maxFloat64(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// writePDFMixedText switches ASCII/Latin runs to Helvetica while keeping CJK
// runs in the configured Unicode font. Some lightweight CJK fallback fonts
// intentionally omit Latin glyphs; using one font for the whole string would
// therefore turn page numbers, years and coordinate values into boxes.
func writePDFMixedText(pdf *fpdf.Fpdf, cjkFamily string, size, lineHeight float64, value string) {
	if cjkFamily == "Helvetica" {
		pdf.SetFont("Helvetica", "", size)
		pdf.Write(lineHeight, value)
		return
	}
	type textRun struct {
		core bool
		text string
	}
	runs := make([]textRun, 0, 8)
	var current strings.Builder
	currentCore := false
	hasCurrent := false
	flush := func() {
		if current.Len() == 0 {
			return
		}
		runs = append(runs, textRun{core: currentCore, text: current.String()})
		current.Reset()
	}
	for _, r := range value {
		core := r <= 255 && r != utf8.RuneError
		if hasCurrent && core != currentCore {
			flush()
		}
		currentCore = core
		hasCurrent = true
		current.WriteRune(r)
	}
	flush()
	for _, run := range runs {
		if run.core {
			pdf.SetFont("Helvetica", "", size)
		} else {
			pdf.SetFont(cjkFamily, "", size)
		}
		pdf.Write(lineHeight, run.text)
	}
}

func configurePDFFont(pdf *fpdf.Fpdf, title string, pages []preparedPage) (string, error) {
	requiresUnicode := !isPDFCoreText(title)
	for _, page := range pages {
		if !isPDFCoreText(page.Text) {
			requiresUnicode = true
		}
		for _, note := range page.Notes {
			if !isPDFCoreText(note) {
				requiresUnicode = true
			}
		}
	}
	fontPath, err := findPDFFont()
	if err != nil {
		if !requiresUnicode {
			return "Helvetica", nil
		}
		return "", err
	}
	raw, err := os.ReadFile(fontPath)
	if err != nil {
		return "", fmt.Errorf("read PDF font %s: %w", fontPath, err)
	}
	pdf.AddUTF8FontFromBytes("firescribe-cjk", "", raw)
	if err := pdf.Error(); err != nil {
		return "", fmt.Errorf("load PDF font %s: %w", fontPath, err)
	}
	return "firescribe-cjk", nil
}

func findPDFFont() (string, error) {
	if configured := strings.TrimSpace(os.Getenv(pdfFontEnv)); configured != "" {
		if info, err := os.Stat(configured); err == nil && !info.IsDir() {
			return configured, nil
		}
		return "", fmt.Errorf("%s points to an unreadable TrueType font: %s", pdfFontEnv, configured)
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		"/usr/share/fonts/truetype/droid/DroidSansFallbackFull.ttf",
		"/usr/share/fonts/truetype/noto/NotoSansSC-Regular.ttf",
		"/usr/share/fonts/truetype/arphic/ukai.ttf",
		"/usr/share/fonts/truetype/arphic/uming.ttf",
		filepath.Join(home, ".local/share/fonts/WinFonts/Deng.ttf"),
		filepath.Join(home, ".local/share/fonts/WinFonts/Dengl.ttf"),
		"/Library/Fonts/Arial Unicode.ttf",
	}
	if windir := os.Getenv("WINDIR"); windir != "" {
		candidates = append(candidates, filepath.Join(windir, "Fonts", "Deng.ttf"), filepath.Join(windir, "Fonts", "Dengl.ttf"))
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("Chinese PDF font not found; install Droid Sans Fallback/Noto Sans CJK or set %s to a Unicode .ttf file (server OS: %s)", pdfFontEnv, runtime.GOOS)
}

func isPDFCoreText(value string) bool {
	for _, r := range value {
		if r > 255 || r == utf8.RuneError {
			return false
		}
	}
	return true
}

func normalizePDFImage(raw []byte) ([]byte, int, int, error) {
	decoded, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, 0, 0, err
	}
	bounds := decoded.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return nil, 0, 0, fmt.Errorf("invalid image dimensions")
	}
	var output bytes.Buffer
	if err := jpeg.Encode(&output, decoded, &jpeg.Options{Quality: 86}); err != nil {
		return nil, 0, 0, err
	}
	return output.Bytes(), bounds.Dx(), bounds.Dy(), nil
}
