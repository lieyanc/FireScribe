package pageproc

import (
	"context"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/image/draw"
)

type ExtractedPage struct {
	Path string
	Ext  string
}

func ExtractPDFImages(ctx context.Context, pdfPath string) ([]ExtractedPage, error) {
	tempDir, err := os.MkdirTemp("", "firescribe-pdf-*")
	if err != nil {
		return nil, err
	}
	prefix := filepath.Join(tempDir, "page")
	cmd := exec.CommandContext(ctx, "pdfimages", "-all", pdfPath, prefix)
	output, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("pdfimages: %w: %s", err, strings.TrimSpace(string(output)))
	}

	matches, err := filepath.Glob(prefix + "-*")
	if err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, err
	}
	sort.Strings(matches)
	pages := make([]ExtractedPage, 0, len(matches))
	for _, match := range matches {
		info, err := os.Stat(match)
		if err == nil && !info.IsDir() && info.Size() > 0 {
			pages = append(pages, ExtractedPage{Path: match, Ext: filepath.Ext(match)})
		}
	}
	if len(pages) == 0 {
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("no extractable page images found")
	}
	return pages, nil
}

func CleanupExtractedPages(pages []ExtractedPage) {
	if len(pages) == 0 {
		return
	}
	_ = os.RemoveAll(filepath.Dir(pages[0].Path))
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

func scaleWithin(width, height, maxSize int) (int, int) {
	if width <= maxSize && height <= maxSize {
		return width, height
	}
	if width >= height {
		return maxSize, max(1, height*maxSize/width)
	}
	return max(1, width*maxSize/height), maxSize
}
