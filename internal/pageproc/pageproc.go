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
	Path string
	Ext  string
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

// RasterizePDF renders each PDF page to a JPEG via pdftoppm. Unlike image
// extraction, rasterization keeps page numbering correct for text pages,
// multi-image pages, and CCITT/JBIG2-encoded scans.
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
			pages = append(pages, ExtractedPage{Path: match, Ext: ".jpg"})
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
