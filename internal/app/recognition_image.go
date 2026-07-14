package app

import (
	"context"
	"encoding/json"
	"fmt"
)

type recognitionImageInput struct {
	Asset              Asset
	Source             string
	ProcessingResultID string
	Width              int
	Height             int
}

// resolveRecognitionImage centralizes original/enhanced selection so
// recognizer drivers never need to know about the processing schema.
func (a *App) resolveRecognitionImage(ctx context.Context, run RecognitionRun, page Page) (recognitionImageInput, error) {
	source, err := normalizeImageSource(run.InputSource)
	if err != nil {
		return recognitionImageInput{}, err
	}
	assetID, width, height := page.ImageAssetID, page.Width, page.Height
	processingResultID := ""
	if source == "enhanced" {
		result, err := a.Store.LatestEnhancedResult(ctx, page.ID)
		if err != nil {
			return recognitionImageInput{}, fmt.Errorf("page %d has no successful enhanced image: %w", page.PageNo, err)
		}
		assetID = result.OutputAssetID
		processingResultID = result.ID
		var metadata struct {
			OutputWidth  int `json:"output_width"`
			OutputHeight int `json:"output_height"`
		}
		if json.Unmarshal([]byte(result.MetadataJSON), &metadata) == nil {
			if metadata.OutputWidth > 0 {
				width = metadata.OutputWidth
			}
			if metadata.OutputHeight > 0 {
				height = metadata.OutputHeight
			}
		}
	}
	if assetID == "" {
		return recognitionImageInput{}, fmt.Errorf("page %d has no %s image asset", page.PageNo, source)
	}
	asset, err := a.Store.GetAsset(ctx, assetID)
	if err != nil {
		return recognitionImageInput{}, fmt.Errorf("load %s page asset: %w", source, err)
	}
	return recognitionImageInput{
		Asset: asset, Source: source, ProcessingResultID: processingResultID,
		Width: width, Height: height,
	}, nil
}
