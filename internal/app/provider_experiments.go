package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lieyan/firescribe/internal/recognizer"
)

type RecognitionExperimentStart struct {
	Experiment RecognitionExperiment `json:"experiment"`
	Job        Job                   `json:"job"`
}

type RecognitionExperimentOptions struct {
	Name     string
	PageIDs  []string
	Variants []RecognitionExperimentVariant
}

type recognitionExperimentJobPayload struct {
	ExperimentID string `json:"experiment_id"`
}

func (a *App) StartRecognitionExperiment(ctx context.Context, documentID string, options RecognitionExperimentOptions) (RecognitionExperimentStart, error) {
	if _, err := a.Store.GetDocument(ctx, documentID); err != nil {
		return RecognitionExperimentStart{}, err
	}
	if len(options.PageIDs) == 0 {
		return RecognitionExperimentStart{}, errors.New("recognition experiment requires at least one page")
	}
	if len(options.PageIDs) > 100 {
		return RecognitionExperimentStart{}, errors.New("recognition experiment supports at most 100 pages")
	}
	if len(options.Variants) < 2 || len(options.Variants) > 8 {
		return RecognitionExperimentStart{}, errors.New("recognition experiment requires between 2 and 8 variants")
	}
	allPages, err := a.Store.ListPages(ctx, documentID)
	if err != nil {
		return RecognitionExperimentStart{}, err
	}
	validPages := make(map[string]bool, len(allPages))
	for _, page := range allPages {
		validPages[page.ID] = true
	}
	seenPages := make(map[string]bool, len(options.PageIDs))
	for _, pageID := range options.PageIDs {
		pageID = strings.TrimSpace(pageID)
		if pageID == "" || !validPages[pageID] {
			return RecognitionExperimentStart{}, fmt.Errorf("page %q does not belong to document", pageID)
		}
		if seenPages[pageID] {
			return RecognitionExperimentStart{}, fmt.Errorf("duplicate experiment page %q", pageID)
		}
		seenPages[pageID] = true
	}

	timestamp := now()
	experiment := RecognitionExperiment{
		ID: newID("ab"), DocumentID: documentID, JobID: newID("job"),
		Name: strings.TrimSpace(options.Name), PageIDs: append([]string(nil), options.PageIDs...),
		Status: "queued", CreatedAt: timestamp,
	}
	if experiment.Name == "" {
		experiment.Name = "Prompt / Profile A/B"
	}
	if len([]rune(experiment.Name)) > 128 {
		return RecognitionExperimentStart{}, errors.New("recognition experiment name must not exceed 128 characters")
	}
	variants := make([]RecognitionExperimentVariant, 0, len(options.Variants))
	for index, input := range options.Variants {
		input.ProfileID = strings.TrimSpace(input.ProfileID)
		input.ProviderAdapterID = strings.TrimSpace(input.ProviderAdapterID)
		input.PromptVersionID = strings.TrimSpace(input.PromptVersionID)
		if input.ProfileID != "" && input.ProviderAdapterID != "" {
			return RecognitionExperimentStart{}, fmt.Errorf("variant %d selects both a profile and provider adapter", index+1)
		}
		if input.ProfileID != "" {
			if _, err := a.Store.GetRecognizerProfile(ctx, input.ProfileID); err != nil {
				return RecognitionExperimentStart{}, fmt.Errorf("variant %d recognizer profile: %w", index+1, err)
			}
		}
		if input.ProviderAdapterID != "" {
			adapter, err := a.Store.GetProviderAdapter(ctx, input.ProviderAdapterID)
			if err != nil {
				return RecognitionExperimentStart{}, fmt.Errorf("variant %d provider adapter: %w", index+1, err)
			}
			if !adapter.IsEnabled {
				return RecognitionExperimentStart{}, fmt.Errorf("variant %d provider adapter is disabled", index+1)
			}
		}
		if input.PromptVersionID != "" {
			if _, err := a.Store.GetPromptVersion(ctx, input.PromptVersionID); err != nil {
				return RecognitionExperimentStart{}, fmt.Errorf("variant %d prompt version: %w", index+1, err)
			}
		}
		_, resolvedProfile, resolvedAdapter, resolvedPrompt, authorContext, snapshotErr := a.recognizerForRun(ctx, documentID, input.ProfileID, input.ProviderAdapterID, input.PromptVersionID)
		if snapshotErr != nil {
			return RecognitionExperimentStart{}, fmt.Errorf("variant %d snapshot: %w", index+1, snapshotErr)
		}
		input.SnapshotJSON = recognizerProfileSnapshot(resolvedProfile, resolvedAdapter, resolvedPrompt, authorContext)
		input.ProfileID = resolvedProfile.ID
		input.ProviderAdapterID = resolvedAdapter.ID
		input.PromptVersionID = resolvedPrompt.ID
		input.ImageSource, err = normalizeImageSource(input.ImageSource)
		if err != nil {
			return RecognitionExperimentStart{}, fmt.Errorf("variant %d: %w", index+1, err)
		}
		input.ID = newID("abv")
		input.ExperimentID = experiment.ID
		input.Name = strings.TrimSpace(input.Name)
		if input.Name == "" {
			input.Name = fmt.Sprintf("Variant %c", 'A'+index)
		}
		if len([]rune(input.Name)) > 128 {
			return RecognitionExperimentStart{}, fmt.Errorf("variant %d name must not exceed 128 characters", index+1)
		}
		input.Position = index
		input.Status = "queued"
		input.CreatedAt = timestamp
		variants = append(variants, input)
	}
	experiment.Variants = variants
	job := Job{
		ID: experiment.JobID, Type: "recognition_experiment", Status: "queued",
		TargetType: "recognition_experiment", TargetID: experiment.ID,
		PayloadJSON: mustJSON(recognitionExperimentJobPayload{ExperimentID: experiment.ID}),
		MaxAttempts: 2, ProgressTotal: len(variants), ProgressMessage: "等待 A/B 实验", CreatedAt: timestamp,
	}
	if err := a.Store.CreateRecognitionExperiment(ctx, experiment, variants, job); err != nil {
		return RecognitionExperimentStart{}, err
	}
	a.launchJob(job.ID)
	return RecognitionExperimentStart{Experiment: experiment, Job: job}, nil
}

func (a *App) runRecognitionExperimentJob(ctx context.Context, job Job) (RecognitionExperiment, error) {
	var payload recognitionExperimentJobPayload
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		return RecognitionExperiment{}, fmt.Errorf("decode recognition experiment payload: %w", err)
	}
	experiment, err := a.Store.GetRecognitionExperiment(context.Background(), payload.ExperimentID)
	if err != nil {
		return RecognitionExperiment{}, err
	}
	if err := a.Store.MarkRecognitionExperimentRunning(context.Background(), experiment.ID); err != nil {
		return RecognitionExperiment{}, err
	}
	failed := 0
	var failures []string
	for index, variant := range experiment.Variants {
		if err := ctx.Err(); err != nil {
			_ = a.Store.FinishRecognitionExperiment(context.Background(), experiment.ID, "canceled", cancelCause(ctx))
			return RecognitionExperiment{}, err
		}
		_ = a.Store.MarkExperimentVariantRunning(context.Background(), variant.ID)
		_ = a.Store.UpdateJobProgress(context.Background(), job.ID, index, len(experiment.Variants), "正在运行 "+variant.Name)
		started := time.Now()
		rec, preparedRun, snapshotErr := a.recognizerForExperimentVariant(ctx, experiment.DocumentID, variant)
		var start RecognitionStart
		var runErr error
		if snapshotErr != nil {
			runErr = snapshotErr
		} else {
			start, runErr = a.StartRecognitionWithOptions(ctx, experiment.DocumentID, RecognitionOptions{
				PageIDs: experiment.PageIDs, InputSource: variant.ImageSource,
				preparedRecognizer: rec, preparedSnapshotJSON: variant.SnapshotJSON, preparedRun: preparedRun,
				experimentVariantID: variant.ID,
			})
		}
		if runErr != nil {
			failed++
			failures = append(failures, variant.Name+": "+runErr.Error())
			_ = a.Store.FinishExperimentVariant(context.Background(), variant.ID, "failed", nil, nil, time.Since(started).Milliseconds(), runErr.Error())
			continue
		}
		terminal, runErr := a.waitRecognitionRun(ctx, start.Run.ID)
		duration := time.Since(started).Milliseconds()
		if runErr != nil {
			_ = a.CancelRun(context.Background(), start.Run.ID)
			failed++
			failures = append(failures, variant.Name+": "+runErr.Error())
			_ = a.Store.FinishExperimentVariant(context.Background(), variant.ID, "failed", []string{start.Run.ID}, nil, duration, runErr.Error())
			if ctx.Err() != nil {
				_ = a.Store.FinishRecognitionExperiment(context.Background(), experiment.ID, "canceled", cancelCause(ctx))
				return RecognitionExperiment{}, ctx.Err()
			}
			continue
		}
		results, resultErr := a.Store.RecognitionResultsForRuns(context.Background(), []string{start.Run.ID})
		confidence := averageResultConfidence(results)
		status := "succeeded"
		message := ""
		if terminal.Status != "succeeded" {
			status = "failed"
			message = terminal.Error
			failed++
			failures = append(failures, variant.Name+": "+message)
		}
		if resultErr != nil {
			status = "failed"
			message = resultErr.Error()
			failed++
			failures = append(failures, variant.Name+": "+message)
		}
		_ = a.Store.FinishExperimentVariant(context.Background(), variant.ID, status, []string{start.Run.ID}, confidence, duration, message)
		_ = a.waitRecognitionRunReleased(ctx, start.Run.ID)
		_ = a.Store.UpdateJobProgress(context.Background(), job.ID, index+1, len(experiment.Variants), "已完成 "+variant.Name)
	}
	status := "succeeded"
	message := ""
	if failed > 0 {
		status = "partial"
		message = summarizeErrors(failures)
	}
	_ = a.Store.FinishRecognitionExperiment(context.Background(), experiment.ID, status, message)
	experiment, _ = a.GetRecognitionExperiment(context.Background(), experiment.ID)
	if failed > 0 {
		return experiment, errors.New(message)
	}
	return experiment, nil
}

func (a *App) recognizerForExperimentVariant(ctx context.Context, documentID string, variant RecognitionExperimentVariant) (recognizer.Recognizer, RecognitionRun, error) {
	if strings.TrimSpace(variant.SnapshotJSON) == "" || strings.TrimSpace(variant.SnapshotJSON) == "{}" {
		rec, profile, adapter, prompt, authorContext, err := a.recognizerForRun(ctx, documentID, variant.ProfileID, variant.ProviderAdapterID, variant.PromptVersionID)
		if err != nil {
			return nil, RecognitionRun{}, err
		}
		snapshot := recognizerProfileSnapshot(profile, adapter, prompt, authorContext)
		driver := profile.Driver
		if adapter.ID != "" {
			driver = adapter.Engine
		}
		return rec, RecognitionRun{ProfileID: profile.ID, ProviderAdapterID: adapter.ID, Driver: driver, ProfileSnapshotJSON: snapshot}, nil
	}
	var snapshot recognizerRunSnapshot
	if err := json.Unmarshal([]byte(variant.SnapshotJSON), &snapshot); err != nil {
		return nil, RecognitionRun{}, fmt.Errorf("decode experiment variant snapshot: %w", err)
	}
	run := RecognitionRun{
		ProfileID: variant.ProfileID, ProviderAdapterID: variant.ProviderAdapterID,
		Driver: snapshot.Driver, ProfileSnapshotJSON: variant.SnapshotJSON,
	}
	rec, err := a.recognizerFromRunSnapshot(ctx, run)
	return rec, run, err
}

func (a *App) waitRecognitionRun(ctx context.Context, runID string) (RecognitionRun, error) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		run, err := a.Store.GetRecognitionRun(context.Background(), runID)
		if err != nil {
			return RecognitionRun{}, err
		}
		switch run.Status {
		case "succeeded", "partial", "failed", "canceled":
			return run, nil
		}
		select {
		case <-ctx.Done():
			return RecognitionRun{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (a *App) waitRecognitionRunReleased(ctx context.Context, runID string) error {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		a.runMu.Lock()
		active := a.runsByID[runID] != nil
		a.runMu.Unlock()
		if !active {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func averageResultConfidence(results []RecognitionResult) *float64 {
	var total float64
	count := 0
	for _, result := range results {
		if result.Confidence != nil {
			total += *result.Confidence
			count++
		}
	}
	if count == 0 {
		return nil
	}
	average := total / float64(count)
	return &average
}

func (a *App) GetRecognitionExperiment(ctx context.Context, id string) (RecognitionExperiment, error) {
	experiment, err := a.Store.GetRecognitionExperiment(ctx, id)
	if err != nil {
		return RecognitionExperiment{}, err
	}
	return a.hydrateExperimentEditDistances(ctx, experiment), nil
}

func (a *App) ListRecognitionExperiments(ctx context.Context, documentID string) ([]RecognitionExperiment, error) {
	items, err := a.Store.ListRecognitionExperiments(ctx, documentID)
	if err != nil {
		return nil, err
	}
	for index := range items {
		items[index] = a.hydrateExperimentEditDistances(ctx, items[index])
	}
	return items, nil
}

func (a *App) hydrateExperimentEditDistances(ctx context.Context, experiment RecognitionExperiment) RecognitionExperiment {
	for index := range experiment.Variants {
		variant := &experiment.Variants[index]
		results, err := a.Store.RecognitionResultsForRuns(ctx, variant.CurrentRunIDs)
		if err != nil {
			continue
		}
		distance := 0
		for _, result := range results {
			if human, ok, err := a.Store.LatestHumanTextAfter(ctx, result.PageID, result.CreatedAt); err == nil && ok {
				distance += runeEditDistance(result.Text, human)
			}
		}
		variant.ManualEditDistance = distance
		_ = a.Store.SetExperimentVariantEditDistance(context.Background(), variant.ID, distance)
	}
	return experiment
}

func runeEditDistance(left, right string) int {
	a := []rune(left)
	b := []rune(right)
	if len(a) > 50_000 {
		a = a[:50_000]
	}
	if len(b) > 50_000 {
		b = b[:50_000]
	}
	if len(a) > len(b) {
		a, b = b, a
	}
	previous := make([]int, len(a)+1)
	current := make([]int, len(a)+1)
	for index := range previous {
		previous[index] = index
	}
	for row, rightRune := range b {
		current[0] = row + 1
		for column, leftRune := range a {
			cost := 1
			if leftRune == rightRune {
				cost = 0
			}
			deletion := previous[column+1] + 1
			insertion := current[column] + 1
			replacement := previous[column] + cost
			current[column+1] = min(deletion, insertion, replacement)
		}
		previous, current = current, previous
	}
	return previous[len(a)]
}
