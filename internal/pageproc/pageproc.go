package pageproc

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	_ "golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

type ExtractedPage struct {
	Path           string
	Ext            string
	Method         string
	FallbackReason string
}

const (
	PDFPageMethodEmbedded   = "embedded_image"
	PDFPageMethodRasterized = "rasterized"
)

type pdfImageRecord struct {
	Page int
	Num  int
}

// normalizedFormats are stored page-image formats that vision APIs accept
// as-is; anything else gets re-encoded to JPEG at import time.
var normalizedFormats = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".webp": true,
}

// SupportedImageExt reports whether an uploaded image file can be imported,
// either directly or through JPEG re-encoding.
func SupportedImageExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".tif", ".tiff", ".bmp":
		return true
	default:
		return false
	}
}

// ExtractPDFPages preserves the original embedded image bytes when every PDF
// page contains exactly one primary image. PDFs that do not match that shape,
// or systems without pdfimages/pdfinfo, fall back to the compatible pdftoppm
// renderer so text pages and more complex PDFs remain importable.
func ExtractPDFPages(ctx context.Context, pdfPath string, dpi int) ([]ExtractedPage, error) {
	pages, directErr := extractEmbeddedPDFImages(ctx, pdfPath)
	if directErr == nil {
		return pages, nil
	}

	pages, rasterErr := RasterizePDF(ctx, pdfPath, dpi)
	if rasterErr != nil {
		return nil, fmt.Errorf("extract embedded PDF images: %v; rasterize fallback: %w", directErr, rasterErr)
	}
	for i := range pages {
		pages[i].FallbackReason = directErr.Error()
	}
	return pages, nil
}

// ProcessPDFPages extracts or renders one page at a time, reports Poppler
// progress, and only starts consume after a complete extraction strategy has
// succeeded. That preserves raster fallback without leaving partially
// imported pages when direct embedded-image extraction fails halfway.
func ProcessPDFPages(ctx context.Context, pdfPath string, dpi int, progress func(int, int, string), consume func(ExtractedPage) error) error {
	pageCount, records, directErr := inspectEmbeddedPDFImages(ctx, pdfPath)
	if directErr == nil {
		tempDir, err := os.MkdirTemp("", "firescribe-pdfimages-pages-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tempDir)
		pages := make([]ExtractedPage, 0, len(records))
		for index, record := range records {
			prefix := filepath.Join(tempDir, fmt.Sprintf("page-%06d", index+1))
			cmd := exec.CommandContext(ctx, "pdfimages", "-f", strconv.Itoa(record.Page), "-l", strconv.Itoa(record.Page), "-all", pdfPath, prefix)
			output, err := cmd.CombinedOutput()
			if err != nil {
				directErr = fmt.Errorf("pdfimages page %d: %w: %s", record.Page, err, strings.TrimSpace(string(output)))
				break
			}
			path, err := extractedImagePath(prefix, record.Num)
			if err != nil {
				// Some Poppler builds renumber outputs when -f/-l is used.
				path, err = firstExtractedImagePath(prefix)
			}
			if err != nil {
				directErr = fmt.Errorf("page %d embedded image: %w", record.Page, err)
				break
			}
			ext := strings.ToLower(filepath.Ext(path))
			if !normalizedFormats[ext] {
				directErr = fmt.Errorf("page %d embedded image format %q is not directly displayable", record.Page, ext)
				break
			}
			if _, _, err := ImageDimensions(path); err != nil {
				directErr = fmt.Errorf("page %d embedded image is not decodable: %w", record.Page, err)
				break
			}
			pages = append(pages, ExtractedPage{Path: path, Ext: ext, Method: PDFPageMethodEmbedded})
			if progress != nil {
				progress(index+1, len(records), fmt.Sprintf("已提取 PDF 第 %d/%d 页", index+1, len(records)))
			}
		}
		if directErr == nil {
			for _, page := range pages {
				if err := consume(page); err != nil {
					return err
				}
			}
			return nil
		}
	}

	if pageCount <= 0 {
		pageCount, _ = pdfPageCount(ctx, pdfPath)
	}
	if pageCount <= 0 {
		pages, err := RasterizePDF(ctx, pdfPath, dpi)
		if err != nil {
			return fmt.Errorf("extract embedded PDF images: %v; rasterize fallback: %w", directErr, err)
		}
		defer CleanupExtractedPages(pages)
		for index, page := range pages {
			page.FallbackReason = directErr.Error()
			if progress != nil {
				progress(index+1, len(pages), fmt.Sprintf("已光栅化 PDF 第 %d/%d 页", index+1, len(pages)))
			}
			if err := consume(page); err != nil {
				return err
			}
		}
		return nil
	}
	if dpi <= 0 {
		dpi = 200
	}
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		return fmt.Errorf("pdftoppm is not installed (install poppler-utils to import PDF files): %w", err)
	}
	tempDir, err := os.MkdirTemp("", "firescribe-pdf-pages-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)
	pages := make([]ExtractedPage, 0, pageCount)
	for pageNo := 1; pageNo <= pageCount; pageNo++ {
		prefix := filepath.Join(tempDir, fmt.Sprintf("page-%06d", pageNo))
		cmd := exec.CommandContext(ctx, "pdftoppm", "-f", strconv.Itoa(pageNo), "-l", strconv.Itoa(pageNo),
			"-singlefile", "-jpeg", "-jpegopt", "quality=85", "-r", strconv.Itoa(dpi), pdfPath, prefix)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("pdftoppm page %d: %w: %s", pageNo, err, strings.TrimSpace(string(output)))
		}
		path := prefix + ".jpg"
		if _, _, err := ImageDimensions(path); err != nil {
			return fmt.Errorf("rendered page %d is not decodable: %w", pageNo, err)
		}
		pages = append(pages, ExtractedPage{Path: path, Ext: ".jpg", Method: PDFPageMethodRasterized, FallbackReason: directErr.Error()})
		if progress != nil {
			progress(pageNo, pageCount, fmt.Sprintf("已光栅化 PDF 第 %d/%d 页", pageNo, pageCount))
		}
	}
	for _, page := range pages {
		if err := consume(page); err != nil {
			return err
		}
	}
	return nil
}

// PDFPageCount returns the number of pages before extraction so callers can
// report page-level progress for a single large PDF.
func PDFPageCount(ctx context.Context, pdfPath string) (int, error) {
	return pdfPageCount(ctx, pdfPath)
}

// extractEmbeddedPDFImages uses Poppler's pdfimages -all mode, which copies
// JPEG/JP2 streams without re-encoding and preserves the closest native file
// representation for other image encodings.
func extractEmbeddedPDFImages(ctx context.Context, pdfPath string) ([]ExtractedPage, error) {
	_, records, err := inspectEmbeddedPDFImages(ctx, pdfPath)
	if err != nil {
		return nil, err
	}

	tempDir, err := os.MkdirTemp("", "firescribe-pdfimages-*")
	if err != nil {
		return nil, err
	}
	prefix := filepath.Join(tempDir, "image")
	extractCmd := exec.CommandContext(ctx, "pdfimages", "-all", pdfPath, prefix)
	extractOutput, err := extractCmd.CombinedOutput()
	if err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("pdfimages -all: %w: %s", err, strings.TrimSpace(string(extractOutput)))
	}

	pages := make([]ExtractedPage, 0, len(records))
	for _, record := range records {
		path, err := extractedImagePath(prefix, record.Num)
		if err != nil {
			_ = os.RemoveAll(tempDir)
			return nil, fmt.Errorf("page %d embedded image: %w", record.Page, err)
		}
		ext := strings.ToLower(filepath.Ext(path))
		if !normalizedFormats[ext] {
			_ = os.RemoveAll(tempDir)
			return nil, fmt.Errorf("page %d embedded image format %q is not directly displayable", record.Page, ext)
		}
		if _, _, err := ImageDimensions(path); err != nil {
			_ = os.RemoveAll(tempDir)
			return nil, fmt.Errorf("page %d embedded image is not decodable: %w", record.Page, err)
		}
		pages = append(pages, ExtractedPage{Path: path, Ext: ext, Method: PDFPageMethodEmbedded})
	}
	return pages, nil
}

func inspectEmbeddedPDFImages(ctx context.Context, pdfPath string) (int, []pdfImageRecord, error) {
	if _, err := exec.LookPath("pdfimages"); err != nil {
		return 0, nil, fmt.Errorf("pdfimages is not installed: %w", err)
	}
	if _, err := exec.LookPath("pdfinfo"); err != nil {
		return 0, nil, fmt.Errorf("pdfinfo is not installed: %w", err)
	}

	pageCount, err := pdfPageCount(ctx, pdfPath)
	if err != nil {
		return 0, nil, err
	}
	listCmd := exec.CommandContext(ctx, "pdfimages", "-list", pdfPath)
	listOutput, err := listCmd.CombinedOutput()
	if err != nil {
		return pageCount, nil, fmt.Errorf("pdfimages -list: %w: %s", err, strings.TrimSpace(string(listOutput)))
	}
	records, err := parsePDFImageList(string(listOutput), pageCount)
	if err != nil {
		return pageCount, nil, err
	}
	return pageCount, records, nil
}

func pdfPageCount(ctx context.Context, pdfPath string) (int, error) {
	cmd := exec.CommandContext(ctx, "pdfinfo", pdfPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("pdfinfo: %w: %s", err, strings.TrimSpace(string(output)))
	}
	for _, line := range strings.Split(string(output), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok || !strings.EqualFold(strings.TrimSpace(key), "Pages") {
			continue
		}
		count, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || count <= 0 {
			return 0, fmt.Errorf("pdfinfo returned invalid page count %q", strings.TrimSpace(value))
		}
		return count, nil
	}
	return 0, fmt.Errorf("pdfinfo output did not include a page count")
}

func parsePDFImageList(output string, pageCount int) ([]pdfImageRecord, error) {
	byPage := make([][]pdfImageRecord, pageCount+1)
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[2] != "image" {
			continue
		}
		page, pageErr := strconv.Atoi(fields[0])
		num, numErr := strconv.Atoi(fields[1])
		if pageErr != nil || numErr != nil || page < 1 || page > pageCount {
			continue
		}
		byPage[page] = append(byPage[page], pdfImageRecord{Page: page, Num: num})
	}

	records := make([]pdfImageRecord, 0, pageCount)
	for page := 1; page <= pageCount; page++ {
		if len(byPage[page]) != 1 {
			return nil, fmt.Errorf("page %d has %d extractable embedded images (want exactly 1)", page, len(byPage[page]))
		}
		records = append(records, byPage[page][0])
	}
	return records, nil
}

func extractedImagePath(prefix string, num int) (string, error) {
	matches, err := filepath.Glob(fmt.Sprintf("%s-%03d.*", prefix, num))
	if err != nil {
		return "", err
	}
	for _, match := range matches {
		ext := strings.ToLower(filepath.Ext(match))
		if ext == ".params" || ext == ".jb2g" {
			continue
		}
		info, err := os.Stat(match)
		if err == nil && !info.IsDir() && info.Size() > 0 {
			return match, nil
		}
	}
	return "", fmt.Errorf("pdfimages did not produce image %03d", num)
}

func firstExtractedImagePath(prefix string) (string, error) {
	matches, err := filepath.Glob(prefix + "-*.*")
	if err != nil {
		return "", err
	}
	for _, match := range matches {
		ext := strings.ToLower(filepath.Ext(match))
		if ext == ".params" || ext == ".jb2g" {
			continue
		}
		info, err := os.Stat(match)
		if err == nil && !info.IsDir() && info.Size() > 0 {
			return match, nil
		}
	}
	return "", fmt.Errorf("pdfimages did not produce a displayable image")
}

// RasterizePDF renders each PDF page to a JPEG via pdftoppm. It is the
// compatibility fallback for text pages, multi-image pages, and PDFs whose
// embedded image layout cannot be mapped one-to-one to pages.
func RasterizePDF(ctx context.Context, pdfPath string, dpi int) ([]ExtractedPage, error) {
	if dpi <= 0 {
		dpi = 200
	}
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		return nil, fmt.Errorf("pdftoppm is not installed (install poppler-utils to import PDF files): %w", err)
	}
	tempDir, err := os.MkdirTemp("", "firescribe-pdf-*")
	if err != nil {
		return nil, err
	}
	prefix := filepath.Join(tempDir, "page")
	cmd := exec.CommandContext(ctx, "pdftoppm",
		"-jpeg", "-jpegopt", "quality=85",
		"-r", strconv.Itoa(dpi),
		pdfPath, prefix,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("pdftoppm: %w: %s", err, strings.TrimSpace(string(output)))
	}

	matches, err := filepath.Glob(prefix + "-*.jpg")
	if err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, err
	}
	sortByNumericSuffix(matches)
	pages := make([]ExtractedPage, 0, len(matches))
	for _, match := range matches {
		info, err := os.Stat(match)
		if err == nil && !info.IsDir() && info.Size() > 0 {
			pages = append(pages, ExtractedPage{Path: match, Ext: ".jpg", Method: PDFPageMethodRasterized})
		}
	}
	if len(pages) == 0 {
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("PDF rendered no pages")
	}
	return pages, nil
}

// sortByNumericSuffix orders pdftoppm outputs (prefix-1.jpg, prefix-10.jpg…)
// by page number; lexicographic order breaks once padding widths differ.
func sortByNumericSuffix(paths []string) {
	sort.Slice(paths, func(i, j int) bool {
		ni, oki := trailingNumber(paths[i])
		nj, okj := trailingNumber(paths[j])
		if oki && okj && ni != nj {
			return ni < nj
		}
		return paths[i] < paths[j]
	})
}

func trailingNumber(path string) (int, bool) {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	idx := strings.LastIndex(base, "-")
	if idx < 0 || idx+1 >= len(base) {
		return 0, false
	}
	n, err := strconv.Atoi(base[idx+1:])
	if err != nil {
		return 0, false
	}
	return n, true
}

func CleanupExtractedPages(pages []ExtractedPage) {
	if len(pages) == 0 {
		return
	}
	_ = os.RemoveAll(filepath.Dir(pages[0].Path))
}

// NormalizeImage prepares an uploaded image for storage as a page image.
// jpg/png/webp files are validated and kept as-is; other supported formats
// (gif/tiff/bmp) are re-encoded to JPEG so recognition providers accept them.
// cleanup removes the temporary file when one was created.
func NormalizeImage(sourcePath, ext string) (path string, outExt string, cleanup func(), err error) {
	noop := func() {}
	ext = strings.ToLower(ext)
	if ext == "" {
		ext = strings.ToLower(filepath.Ext(sourcePath))
	}
	if normalizedFormats[ext] {
		if _, _, err := ImageDimensions(sourcePath); err != nil {
			return "", "", noop, fmt.Errorf("decode image: %w", err)
		}
		return sourcePath, ext, noop, nil
	}

	in, err := os.Open(sourcePath)
	if err != nil {
		return "", "", noop, err
	}
	defer in.Close()
	src, _, err := image.Decode(in)
	if err != nil {
		return "", "", noop, fmt.Errorf("decode image: %w", err)
	}

	out, err := os.CreateTemp("", "firescribe-page-*.jpg")
	if err != nil {
		return "", "", noop, err
	}
	tempPath := out.Name()
	cleanup = func() { _ = os.Remove(tempPath) }
	if err := jpeg.Encode(out, flattenOnWhite(src), &jpeg.Options{Quality: 90}); err != nil {
		_ = out.Close()
		cleanup()
		return "", "", noop, err
	}
	if err := out.Close(); err != nil {
		cleanup()
		return "", "", noop, err
	}
	return tempPath, ".jpg", cleanup, nil
}

// PrepareForUpload loads a page image and downscales it so its long edge is
// at most maxEdge pixels (0 disables scaling), returning the payload bytes and
// MIME type to send to the recognition provider.
func PrepareForUpload(path string, maxEdge int) (string, []byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
	}
	mimeType := mimeByExt(filepath.Ext(path))

	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		// Undecodable but present on disk: send as-is and let the provider decide.
		return mimeType, raw, nil
	}
	if maxEdge <= 0 || (cfg.Width <= maxEdge && cfg.Height <= maxEdge) {
		return mimeType, raw, nil
	}

	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return mimeType, raw, nil
	}
	targetW, targetH := scaleWithin(cfg.Width, cfg.Height, maxEdge)
	dst := image.NewRGBA(image.Rect(0, 0, targetW, targetH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, flattenOnWhite(dst), &jpeg.Options{Quality: 88}); err != nil {
		return mimeType, raw, nil
	}
	return "image/jpeg", buf.Bytes(), nil
}

func mimeByExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	default:
		return "application/octet-stream"
	}
}

func ImageDimensions(path string) (int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0, err
	}
	return cfg.Width, cfg.Height, nil
}

func GenerateThumbnail(sourcePath, targetPath string, maxSize int) (int, int, bool, error) {
	width, height, dimErr := ImageDimensions(sourcePath)

	in, err := os.Open(sourcePath)
	if err != nil {
		return width, height, false, err
	}
	defer in.Close()

	src, _, err := image.Decode(in)
	if err != nil {
		if dimErr != nil {
			return 0, 0, false, nil
		}
		return width, height, false, nil
	}
	bounds := src.Bounds()
	width = bounds.Dx()
	height = bounds.Dy()
	if width == 0 || height == 0 {
		return width, height, false, nil
	}

	targetW, targetH := scaleWithin(width, height, maxSize)
	dst := image.NewRGBA(image.Rect(0, 0, targetW, targetH))
	fillWhite(dst)
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, bounds, draw.Over, nil)

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return width, height, false, err
	}
	out, err := os.Create(targetPath)
	if err != nil {
		return width, height, false, err
	}
	if err := jpeg.Encode(out, dst, &jpeg.Options{Quality: 82}); err != nil {
		_ = out.Close()
		return width, height, false, err
	}
	if err := out.Close(); err != nil {
		return width, height, false, err
	}
	return width, height, true, nil
}

// flattenOnWhite composites an image over a white background so transparent
// regions do not turn black in JPEG output.
func flattenOnWhite(src image.Image) image.Image {
	if opaque, ok := src.(interface{ Opaque() bool }); ok && opaque.Opaque() {
		return src
	}
	bounds := src.Bounds()
	dst := image.NewRGBA(bounds)
	fillWhite(dst)
	draw.Draw(dst, bounds, src, bounds.Min, draw.Over)
	return dst
}

func fillWhite(dst *image.RGBA) {
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
}

func scaleWithin(width, height, maxSize int) (int, int) {
	if width <= maxSize && height <= maxSize {
		return width, height
	}
	if width >= height {
		return maxSize, max(1, height*maxSize/width)
	}
	return max(1, width*maxSize/height), maxSize
}
