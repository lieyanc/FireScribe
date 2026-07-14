package pageproc

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
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

func writeSingleJPEGImagePDF(t *testing.T, path string, jpegData []byte, width, height int) {
	t.Helper()
	content := fmt.Sprintf("q\n%d 0 0 %d 0 0 cm\n/Im0 Do\nQ\n", width, height)
	objects := [][]byte{
		[]byte("<< /Type /Catalog /Pages 2 0 R >>"),
		[]byte("<< /Type /Pages /Kids [3 0 R] /Count 1 >>"),
		[]byte(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %d %d] /Resources << /XObject << /Im0 4 0 R >> >> /Contents 5 0 R >>", width, height)),
		append([]byte(fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n", width, height, len(jpegData))), append(jpegData, []byte("\nendstream")...)...),
		[]byte(fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content)),
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, object := range objects {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n", i+1)
		buf.Write(object)
		buf.WriteString("\nendobj\n")
	}
	xrefStart := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n", len(objects)+1)
	buf.WriteString("0000000000 65535 f \n")
	for _, offset := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xrefStart)
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func testJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 7), G: uint8(y * 11), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 91}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractPDFPagesPreservesSingleEmbeddedJPEG(t *testing.T) {
	for _, command := range []string{"pdfimages", "pdfinfo"} {
		if _, err := exec.LookPath(command); err != nil {
			t.Skipf("%s not installed", command)
		}
	}
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "scan.pdf")
	original := testJPEG(t, 24, 16)
	writeSingleJPEGImagePDF(t, pdfPath, original, 24, 16)

	pages, err := ExtractPDFPages(context.Background(), pdfPath, 100)
	if err != nil {
		t.Fatalf("ExtractPDFPages() error = %v", err)
	}
	defer CleanupExtractedPages(pages)
	if len(pages) != 1 {
		t.Fatalf("pages = %d, want 1", len(pages))
	}
	page := pages[0]
	if page.Method != PDFPageMethodEmbedded || page.FallbackReason != "" {
		t.Fatalf("method = %q fallback = %q, want direct embedded extraction", page.Method, page.FallbackReason)
	}
	if page.Ext != ".jpg" {
		t.Fatalf("ext = %q, want .jpg", page.Ext)
	}
	extracted, err := os.ReadFile(page.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(extracted, original) {
		t.Fatal("pdfimages did not preserve the original embedded JPEG bytes")
	}
}

func TestExtractPDFPagesFallsBackAndRendersEveryPageInOrder(t *testing.T) {
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		t.Skip("pdftoppm not installed")
	}
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "doc.pdf")
	writeTestPDF(t, pdfPath, 3)

	pages, err := ExtractPDFPages(context.Background(), pdfPath, 100)
	if err != nil {
		t.Fatalf("ExtractPDFPages() error = %v", err)
	}
	defer CleanupExtractedPages(pages)
	if len(pages) != 3 {
		t.Fatalf("pages = %d, want 3", len(pages))
	}
	for i, page := range pages {
		if page.Method != PDFPageMethodRasterized {
			t.Fatalf("page %d method = %q, want rasterized fallback", i, page.Method)
		}
		if page.FallbackReason == "" {
			t.Fatalf("page %d did not explain why direct extraction fell back", i)
		}
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

func TestPDFPageCountSupportsImportProgressPreflight(t *testing.T) {
	if _, err := exec.LookPath("pdfinfo"); err != nil {
		t.Skip("pdfinfo not installed")
	}
	pdfPath := filepath.Join(t.TempDir(), "progress.pdf")
	writeTestPDF(t, pdfPath, 7)
	count, err := PDFPageCount(context.Background(), pdfPath)
	if err != nil {
		t.Fatal(err)
	}
	if count != 7 {
		t.Fatalf("page count = %d, want 7", count)
	}
}

func TestProcessPDFPagesReportsEachRasterizedPageDuringWork(t *testing.T) {
	for _, command := range []string{"pdfinfo", "pdfimages", "pdftoppm"} {
		if _, err := exec.LookPath(command); err != nil {
			t.Skipf("%s not installed", command)
		}
	}
	pdfPath := filepath.Join(t.TempDir(), "incremental.pdf")
	writeTestPDF(t, pdfPath, 3)
	seen := 0
	progress := 0
	if err := ProcessPDFPages(context.Background(), pdfPath, 100, func(current, total int, _ string) {
		progress = current
		if total != 3 {
			t.Fatalf("progress total = %d", total)
		}
	}, func(page ExtractedPage) error {
		seen++
		if page.Method != PDFPageMethodRasterized || page.FallbackReason == "" {
			t.Fatalf("page %d = %+v", seen, page)
		}
		if _, _, err := ImageDimensions(page.Path); err != nil {
			t.Fatalf("page %d is unavailable during callback: %v", seen, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if seen != 3 {
		t.Fatalf("callbacks = %d, want 3", seen)
	}
	if progress != 3 {
		t.Fatalf("progress = %d, want 3", progress)
	}
}

func TestProcessPDFPagesPreservesEmbeddedImageDuringCallback(t *testing.T) {
	for _, command := range []string{"pdfinfo", "pdfimages"} {
		if _, err := exec.LookPath(command); err != nil {
			t.Skipf("%s not installed", command)
		}
	}
	original := testJPEG(t, 20, 12)
	pdfPath := filepath.Join(t.TempDir(), "embedded-incremental.pdf")
	writeSingleJPEGImagePDF(t, pdfPath, original, 20, 12)
	seen := 0
	if err := ProcessPDFPages(context.Background(), pdfPath, 100, nil, func(page ExtractedPage) error {
		seen++
		data, err := os.ReadFile(page.Path)
		if err != nil {
			return err
		}
		if page.Method != PDFPageMethodEmbedded || !bytes.Equal(data, original) {
			t.Fatalf("embedded page = %+v bytes_equal=%v", page, bytes.Equal(data, original))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if seen != 1 {
		t.Fatalf("callbacks = %d, want 1", seen)
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
