package pageproc

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/image/bmp"
)

func writeTestImage(t *testing.T, path string, width, height int, encode func(*os.File, image.Image) error) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func pngEncoder(f *os.File, img image.Image) error { return png.Encode(f, img) }
func bmpEncoder(f *os.File, img image.Image) error { return bmp.Encode(f, img) }

// writeTestPDF builds a minimal valid multi-page PDF with a correct xref table.
func writeTestPDF(t *testing.T, path string, pages int) {
	t.Helper()
	var buf bytes.Buffer
	offsets := []int{}
	write := func(s string) {
		offsets = append(offsets, buf.Len())
		buf.WriteString(s)
	}
	buf.WriteString("%PDF-1.4\n")

	kids := ""
	for i := 0; i < pages; i++ {
		if i > 0 {
			kids += " "
		}
		kids += fmt.Sprintf("%d 0 R", 3+i)
	}
	write("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	write(fmt.Sprintf("2 0 obj\n<< /Type /Pages /Kids [%s] /Count %d >>\nendobj\n", kids, pages))
	for i := 0; i < pages; i++ {
		write(fmt.Sprintf("%d 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 144 72] >>\nendobj\n", 3+i))
	}

	xrefStart := buf.Len()
	total := 2 + pages
	buf.WriteString(fmt.Sprintf("xref\n0 %d\n", total+1))
	buf.WriteString("0000000000 65535 f \n")
	for _, off := range offsets {
		buf.WriteString(fmt.Sprintf("%010d 00000 n \n", off))
	}
	buf.WriteString(fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", total+1, xrefStart))

	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRasterizePDFRendersEveryPageInOrder(t *testing.T) {
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		t.Skip("pdftoppm not installed")
	}
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "doc.pdf")
	writeTestPDF(t, pdfPath, 3)

	pages, err := RasterizePDF(context.Background(), pdfPath, 100)
	if err != nil {
		t.Fatalf("RasterizePDF() error = %v", err)
	}
	defer CleanupExtractedPages(pages)
	if len(pages) != 3 {
		t.Fatalf("pages = %d, want 3", len(pages))
	}
	for i, page := range pages {
		if page.Ext != ".jpg" {
			t.Fatalf("page %d ext = %q", i, page.Ext)
		}
		width, height, err := ImageDimensions(page.Path)
		if err != nil {
			t.Fatalf("page %d dimensions: %v", i, err)
		}
		if width == 0 || height == 0 {
			t.Fatalf("page %d has zero dimensions", i)
		}
		if no, ok := trailingNumber(page.Path); !ok || no != i+1 {
			t.Fatalf("page %d numeric suffix = %d (ok=%v)", i, no, ok)
		}
	}
}

func TestSortByNumericSuffixHandlesMixedPadding(t *testing.T) {
	paths := []string{"p/page-10.jpg", "p/page-2.jpg", "p/page-1.jpg", "p/page-999.jpg", "p/page-1000.jpg"}
	sortByNumericSuffix(paths)
	want := []string{"p/page-1.jpg", "p/page-2.jpg", "p/page-10.jpg", "p/page-999.jpg", "p/page-1000.jpg"}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("paths[%d] = %s, want %s (full: %v)", i, paths[i], want[i], paths)
		}
	}
}

func TestNormalizeImageKeepsProviderFriendlyFormats(t *testing.T) {
	dir := t.TempDir()
	pngPath := filepath.Join(dir, "page.png")
	writeTestImage(t, pngPath, 30, 30, pngEncoder)

	path, ext, cleanup, err := NormalizeImage(pngPath, ".png")
	defer cleanup()
	if err != nil {
		t.Fatalf("NormalizeImage() error = %v", err)
	}
	if path != pngPath || ext != ".png" {
		t.Fatalf("path = %s ext = %s, want original kept", path, ext)
	}
}

func TestNormalizeImageReencodesBMPToJPEG(t *testing.T) {
	dir := t.TempDir()
	bmpPath := filepath.Join(dir, "page.bmp")
	writeTestImage(t, bmpPath, 30, 30, bmpEncoder)

	path, ext, cleanup, err := NormalizeImage(bmpPath, ".bmp")
	defer cleanup()
	if err != nil {
		t.Fatalf("NormalizeImage() error = %v", err)
	}
	if ext != ".jpg" {
		t.Fatalf("ext = %s, want .jpg", ext)
	}
	if path == bmpPath {
		t.Fatal("expected a re-encoded temp file")
	}
	width, height, err := ImageDimensions(path)
	if err != nil || width != 30 || height != 30 {
		t.Fatalf("re-encoded dims = %dx%d err=%v", width, height, err)
	}
}

func TestNormalizeImageRejectsCorruptFiles(t *testing.T) {
	dir := t.TempDir()
	badPath := filepath.Join(dir, "broken.png")
	if err := os.WriteFile(badPath, []byte("not an image"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, cleanup, err := NormalizeImage(badPath, ".png")
	defer cleanup()
	if err == nil {
		t.Fatal("NormalizeImage() succeeded on corrupt file")
	}
}

func TestPrepareForUploadDownscalesLongEdge(t *testing.T) {
	dir := t.TempDir()
	bigPath := filepath.Join(dir, "big.png")
	writeTestImage(t, bigPath, 1600, 400, pngEncoder)

	mimeType, data, err := PrepareForUpload(bigPath, 800)
	if err != nil {
		t.Fatalf("PrepareForUpload() error = %v", err)
	}
	if mimeType != "image/jpeg" {
		t.Fatalf("mime = %s, want image/jpeg", mimeType)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode downscaled payload: %v", err)
	}
	if cfg.Width != 800 || cfg.Height != 200 {
		t.Fatalf("downscaled dims = %dx%d, want 800x200", cfg.Width, cfg.Height)
	}
}

func TestPrepareForUploadKeepsSmallImagesUntouched(t *testing.T) {
	dir := t.TempDir()
	smallPath := filepath.Join(dir, "small.png")
	writeTestImage(t, smallPath, 200, 100, pngEncoder)
	original, err := os.ReadFile(smallPath)
	if err != nil {
		t.Fatal(err)
	}

	for _, maxEdge := range []int{0, 2048} {
		mimeType, data, err := PrepareForUpload(smallPath, maxEdge)
		if err != nil {
			t.Fatalf("PrepareForUpload(maxEdge=%d) error = %v", maxEdge, err)
		}
		if mimeType != "image/png" {
			t.Fatalf("mime = %s, want image/png", mimeType)
		}
		if !bytes.Equal(data, original) {
			t.Fatalf("payload modified for maxEdge=%d", maxEdge)
		}
	}
}
