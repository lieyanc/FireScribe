package pageproc

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestEnhanceFileKeepsOriginalAndProducesPreviewMetadata(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.png")
	outputPath := filepath.Join(dir, "enhanced.png")
	img := image.NewGray(image.Rect(0, 0, 360, 260))
	for y := 0; y < 260; y++ {
		for x := 0; x < 360; x++ {
			// A broad illumination gradient simulates a photographed page shadow.
			value := uint8(205 + x*45/359)
			img.SetGray(x, y, color.Gray{Y: value})
		}
	}
	for _, y := range []int{75, 110, 145, 180} {
		for yy := y; yy < y+5; yy++ {
			for x := 65; x < 295; x++ {
				if (x/17)%5 != 4 {
					img.SetGray(x, yy, color.Gray{Y: 35})
				}
			}
		}
	}
	f, err := os.Create(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}

	metadata, err := EnhanceFile(context.Background(), sourcePath, outputPath, DefaultEnhanceConfig())
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("source page image was modified")
	}
	if metadata.Crop == nil {
		t.Fatalf("expected conservative crop metadata, got %+v", metadata)
	}
	if metadata.OutputWidth >= metadata.OriginalWidth || metadata.OutputHeight >= metadata.OriginalHeight {
		t.Fatalf("output dimensions = %dx%d, original = %dx%d", metadata.OutputWidth, metadata.OutputHeight, metadata.OriginalWidth, metadata.OriginalHeight)
	}
	if len(metadata.Segments) < 2 {
		t.Fatalf("segments = %+v, want text-band preview regions", metadata.Segments)
	}
	if info, err := os.Stat(outputPath); err != nil || info.Size() == 0 {
		t.Fatalf("enhanced output missing: info=%v err=%v", info, err)
	}
}

func TestEstimateDeskewAngleFindsSmallTilt(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 420, 260))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	// Lines rise by roughly 2 degrees from left to right. A negative rotation
	// should maximize their horizontal projection.
	for _, baseY := range []int{70, 110, 150, 190} {
		for x := 40; x < 380; x++ {
			y := baseY + int(float64(x-40)*0.035)
			for dy := 0; dy < 3; dy++ {
				img.SetGray(x, y+dy, color.Gray{Y: 25})
			}
		}
	}
	angle := estimateDeskewAngle(context.Background(), img, 4, 0.5)
	if angle > -1 || angle < -3 {
		t.Fatalf("deskew angle = %.2f, want correction near -2 degrees", angle)
	}
}
