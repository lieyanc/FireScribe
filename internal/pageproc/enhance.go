package pageproc

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

// EnhanceConfig contains conservative, reproducible page-image processing
// controls. The zero value is normalized by NormalizeEnhanceConfig.
type EnhanceConfig struct {
	AutoCrop            bool    `json:"auto_crop"`
	NormalizeBackground bool    `json:"normalize_background"`
	Deskew              bool    `json:"deskew"`
	EnhanceContrast     bool    `json:"enhance_contrast"`
	DetectSegments      bool    `json:"detect_segments"`
	CropPadding         int     `json:"crop_padding"`
	DeskewMaxAngle      float64 `json:"deskew_max_angle"`
	DeskewStep          float64 `json:"deskew_step"`
}

type Rect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Segment is an intentionally generic persisted region. The first detector
// produces horizontal text bands; future layout models can add other kinds
// without changing the processing result schema.
type Segment struct {
	Kind     string `json:"kind"`
	Position int    `json:"position"`
	Rect
}

type EnhanceMetadata struct {
	OriginalWidth  int       `json:"original_width"`
	OriginalHeight int       `json:"original_height"`
	OutputWidth    int       `json:"output_width"`
	OutputHeight   int       `json:"output_height"`
	Crop           *Rect     `json:"crop,omitempty"`
	DeskewAngle    float64   `json:"deskew_angle"`
	Segments       []Segment `json:"segments,omitempty"`
}

func DefaultEnhanceConfig() EnhanceConfig {
	return EnhanceConfig{
		AutoCrop: true, NormalizeBackground: true, Deskew: true,
		EnhanceContrast: true, DetectSegments: true,
		CropPadding: 24, DeskewMaxAngle: 3, DeskewStep: 0.5,
	}
}

func NormalizeEnhanceConfig(cfg EnhanceConfig) EnhanceConfig {
	if cfg.CropPadding < 0 {
		cfg.CropPadding = 0
	}
	if cfg.CropPadding > 256 {
		cfg.CropPadding = 256
	}
	if cfg.DeskewMaxAngle <= 0 {
		cfg.DeskewMaxAngle = 3
	}
	if cfg.DeskewMaxAngle > 8 {
		cfg.DeskewMaxAngle = 8
	}
	if cfg.DeskewStep <= 0 {
		cfg.DeskewStep = 0.5
	}
	if cfg.DeskewStep < 0.1 {
		cfg.DeskewStep = 0.1
	}
	if cfg.DeskewStep > cfg.DeskewMaxAngle {
		cfg.DeskewStep = cfg.DeskewMaxAngle
	}
	return cfg
}

// EnhanceFile decodes sourcePath, applies the configured transformations and
// writes a new PNG. It never modifies the source file.
func EnhanceFile(ctx context.Context, sourcePath, outputPath string, cfg EnhanceConfig) (EnhanceMetadata, error) {
	f, err := os.Open(sourcePath)
	if err != nil {
		return EnhanceMetadata{}, err
	}
	source, _, err := image.Decode(f)
	_ = f.Close()
	if err != nil {
		return EnhanceMetadata{}, fmt.Errorf("decode source image: %w", err)
	}

	cfg = NormalizeEnhanceConfig(cfg)
	gray := toGray(source)
	metadata := EnhanceMetadata{OriginalWidth: gray.Bounds().Dx(), OriginalHeight: gray.Bounds().Dy()}
	if err := ctx.Err(); err != nil {
		return EnhanceMetadata{}, err
	}
	if cfg.NormalizeBackground {
		gray = normalizeBackground(ctx, gray)
	}
	if cfg.AutoCrop {
		if cropped, rect, ok := autoCrop(gray, cfg.CropPadding); ok {
			gray = cropped
			metadata.Crop = &rect
		}
	}
	if cfg.Deskew {
		angle := estimateDeskewAngle(ctx, gray, cfg.DeskewMaxAngle, cfg.DeskewStep)
		if math.Abs(angle) >= cfg.DeskewStep*0.75 {
			gray = rotateGray(gray, angle)
			metadata.DeskewAngle = math.Round(angle*100) / 100
		}
	}
	if cfg.EnhanceContrast {
		gray = enhanceContrast(gray)
	}
	if cfg.DetectSegments {
		metadata.Segments = detectTextBands(gray)
	}
	metadata.OutputWidth = gray.Bounds().Dx()
	metadata.OutputHeight = gray.Bounds().Dy()
	if err := ctx.Err(); err != nil {
		return EnhanceMetadata{}, err
	}

	out, err := os.Create(outputPath)
	if err != nil {
		return EnhanceMetadata{}, err
	}
	err = png.Encode(out, gray)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return EnhanceMetadata{}, fmt.Errorf("encode enhanced image: %w", err)
	}
	return metadata, nil
}

func (m EnhanceMetadata) JSON() string {
	b, _ := json.Marshal(m)
	return string(b)
}

func toGray(source image.Image) *image.Gray {
	b := source.Bounds()
	dst := image.NewGray(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			dst.SetGray(x, y, color.GrayModel.Convert(source.At(b.Min.X+x, b.Min.Y+y)).(color.Gray))
		}
	}
	return dst
}

func normalizeBackground(ctx context.Context, source *image.Gray) *image.Gray {
	w, h := source.Bounds().Dx(), source.Bounds().Dy()
	radius := minInt(64, maxInt(12, minInt(w, h)/35))
	stride := w + 1
	integral := make([]uint64, (w+1)*(h+1))
	for y := 0; y < h; y++ {
		if y%64 == 0 && ctx.Err() != nil {
			return source
		}
		var row uint64
		for x := 0; x < w; x++ {
			row += uint64(source.GrayAt(x, y).Y)
			integral[(y+1)*stride+x+1] = integral[y*stride+x+1] + row
		}
	}
	dst := image.NewGray(source.Bounds())
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			x0, x1 := maxInt(0, x-radius), minInt(w, x+radius+1)
			y0, y1 := maxInt(0, y-radius), minInt(h, y+radius+1)
			sum := integral[y1*stride+x1] - integral[y0*stride+x1] - integral[y1*stride+x0] + integral[y0*stride+x0]
			background := int(sum / uint64((x1-x0)*(y1-y0)))
			ink := background - int(source.GrayAt(x, y).Y)
			value := 255 - int(float64(maxInt(0, ink))*1.15)
			dst.SetGray(x, y, color.Gray{Y: uint8(clampInt(value, 0, 255))})
		}
	}
	return dst
}

func autoCrop(source *image.Gray, padding int) (*image.Gray, Rect, bool) {
	w, h := source.Bounds().Dx(), source.Bounds().Dy()
	threshold := foregroundThreshold(source)
	minX, minY, maxX, maxY := w, h, -1, -1
	rowMinimum, colMinimum := maxInt(2, w/800), maxInt(2, h/800)
	for y := 0; y < h; y++ {
		count := 0
		for x := 0; x < w; x++ {
			if source.GrayAt(x, y).Y < threshold {
				count++
			}
		}
		if count >= rowMinimum {
			minY, maxY = minInt(minY, y), maxInt(maxY, y)
		}
	}
	for x := 0; x < w; x++ {
		count := 0
		for y := 0; y < h; y++ {
			if source.GrayAt(x, y).Y < threshold {
				count++
			}
		}
		if count >= colMinimum {
			minX, maxX = minInt(minX, x), maxInt(maxX, x)
		}
	}
	if maxX < minX || maxY < minY {
		return source, Rect{}, false
	}
	minX, minY = maxInt(0, minX-padding), maxInt(0, minY-padding)
	maxX, maxY = minInt(w, maxX+padding+1), minInt(h, maxY+padding+1)
	// Avoid destructive crops when noise or a sparse mark produces an
	// implausibly small content box.
	if maxX-minX < w/3 || maxY-minY < h/3 || (minX == 0 && minY == 0 && maxX == w && maxY == h) {
		return source, Rect{}, false
	}
	dst := image.NewGray(image.Rect(0, 0, maxX-minX, maxY-minY))
	for y := minY; y < maxY; y++ {
		copy(dst.Pix[(y-minY)*dst.Stride:(y-minY)*dst.Stride+dst.Bounds().Dx()], source.Pix[y*source.Stride+minX:y*source.Stride+maxX])
	}
	return dst, Rect{X: minX, Y: minY, Width: maxX - minX, Height: maxY - minY}, true
}

func foregroundThreshold(source *image.Gray) uint8 {
	var hist [256]int
	for _, value := range source.Pix {
		hist[value]++
	}
	total := len(source.Pix)
	cutoff := maxInt(1, total/20)
	acc := 0
	p5 := 128
	for i, count := range hist {
		acc += count
		if acc >= cutoff {
			p5 = i
			break
		}
	}
	return uint8(clampInt(p5+45, 150, 240))
}

func estimateDeskewAngle(ctx context.Context, source *image.Gray, maxAngle, step float64) float64 {
	w, h := source.Bounds().Dx(), source.Bounds().Dy()
	scale := math.Min(1, 900/math.Max(float64(w), float64(h)))
	stepPixels := maxInt(1, int(math.Round(1/scale)))
	type point struct{ x, y float64 }
	points := make([]point, 0, w*h/(stepPixels*stepPixels*12))
	threshold := foregroundThreshold(source)
	for y := 0; y < h; y += stepPixels {
		for x := 0; x < w; x += stepPixels {
			if source.GrayAt(x, y).Y < threshold {
				points = append(points, point{float64(x) * scale, float64(y) * scale})
			}
		}
	}
	if len(points) < 40 {
		return 0
	}
	cx, cy := float64(w)*scale/2, float64(h)*scale/2
	rows := maxInt(1, int(math.Ceil(float64(h)*scale))+20)
	score := func(angle float64) float64 {
		radians := angle * math.Pi / 180
		sin, cos := math.Sin(radians), math.Cos(radians)
		hist := make([]int, rows)
		for _, p := range points {
			ry := (p.x-cx)*sin + (p.y-cy)*cos + cy
			idx := int(math.Round(ry)) + 10
			if idx >= 0 && idx < len(hist) {
				hist[idx]++
			}
		}
		var sum float64
		for _, count := range hist {
			sum += float64(count * count)
		}
		return sum
	}
	baseline := score(0)
	bestAngle, bestScore := 0.0, baseline
	for angle := -maxAngle; angle <= maxAngle+step/2; angle += step {
		if ctx.Err() != nil {
			return 0
		}
		candidate := score(angle)
		if candidate > bestScore {
			bestAngle, bestScore = angle, candidate
		}
	}
	if bestScore < baseline*1.015 {
		return 0
	}
	return bestAngle
}

func rotateGray(source *image.Gray, angle float64) *image.Gray {
	radians := angle * math.Pi / 180
	sin, cos := math.Sin(radians), math.Cos(radians)
	w, h := float64(source.Bounds().Dx()), float64(source.Bounds().Dy())
	outW := int(math.Ceil(math.Abs(w*cos) + math.Abs(h*sin)))
	outH := int(math.Ceil(math.Abs(w*sin) + math.Abs(h*cos)))
	dst := image.NewGray(image.Rect(0, 0, outW, outH))
	for i := range dst.Pix {
		dst.Pix[i] = 255
	}
	scx, scy := (w-1)/2, (h-1)/2
	dcx, dcy := float64(outW-1)/2, float64(outH-1)/2
	for y := 0; y < outH; y++ {
		for x := 0; x < outW; x++ {
			dx, dy := float64(x)-dcx, float64(y)-dcy
			sx := dx*cos + dy*sin + scx
			sy := -dx*sin + dy*cos + scy
			ix, iy := int(math.Round(sx)), int(math.Round(sy))
			if ix >= 0 && iy >= 0 && ix < int(w) && iy < int(h) {
				dst.SetGray(x, y, source.GrayAt(ix, iy))
			}
		}
	}
	return dst
}

func enhanceContrast(source *image.Gray) *image.Gray {
	var hist [256]int
	for _, value := range source.Pix {
		hist[value]++
	}
	total := len(source.Pix)
	lowTarget, highTarget := total/100, total-total/200
	acc, low, high := 0, 0, 255
	for value, count := range hist {
		acc += count
		if acc >= lowTarget {
			low = value
			break
		}
	}
	acc = 0
	for value, count := range hist {
		acc += count
		if acc >= highTarget {
			high = value
			break
		}
	}
	if high-low < 24 {
		return source
	}
	dst := image.NewGray(source.Bounds())
	for i, value := range source.Pix {
		dst.Pix[i] = uint8(clampInt((int(value)-low)*255/(high-low), 0, 255))
	}
	return dst
}

func detectTextBands(source *image.Gray) []Segment {
	w, h := source.Bounds().Dx(), source.Bounds().Dy()
	threshold := foregroundThreshold(source)
	active := make([]bool, h)
	minimum := maxInt(3, w/180)
	for y := 0; y < h; y++ {
		count := 0
		for x := 0; x < w; x++ {
			if source.GrayAt(x, y).Y < threshold {
				count++
			}
		}
		active[y] = count >= minimum
	}
	segments := make([]Segment, 0)
	for y := 0; y < h; {
		for y < h && !active[y] {
			y++
		}
		if y >= h {
			break
		}
		start := y
		lastActive := y
		for y < h {
			if active[y] {
				lastActive = y
			}
			if y-lastActive > maxInt(3, h/250) {
				break
			}
			y++
		}
		end := lastActive + 1
		minX, maxX := w, -1
		for yy := start; yy < end; yy++ {
			for x := 0; x < w; x++ {
				if source.GrayAt(x, yy).Y < threshold {
					minX, maxX = minInt(minX, x), maxInt(maxX, x)
				}
			}
		}
		if maxX >= minX && end-start >= 2 {
			pad := 4
			segments = append(segments, Segment{Kind: "text_band", Position: len(segments), Rect: Rect{
				X: maxInt(0, minX-pad), Y: maxInt(0, start-pad),
				Width:  minInt(w, maxX+pad+1) - maxInt(0, minX-pad),
				Height: minInt(h, end+pad) - maxInt(0, start-pad),
			}})
		}
	}
	return segments
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func clampInt(value, low, high int) int { return minInt(high, maxInt(low, value)) }
