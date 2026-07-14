package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/lieyan/firescribe/internal/app"
	"github.com/lieyan/firescribe/internal/recognizer"
)

func TestRetryRunUsesImmutableOpenAIProfileSnapshotAndCurrentSecret(t *testing.T) {
	ctx := context.Background()
	var mu sync.Mutex
	succeed := false
	lastAuthorization := ""
	lastModel := ""
	original := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		lastAuthorization = r.Header.Get("Authorization")
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		lastModel = body.Model
		if !succeed {
			http.Error(w, `{"error":"first run fails"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"快照重试成功"}}]}`))
	}))
	defer original.Close()

	application, _ := newTestApp(t, recognizer.MockRecognizer{})
	doc, err := application.ImportDocument(ctx, app.ImportOptions{Title: "OpenAI 快照重试"},
		app.ImportFile{Name: "page.png", Reader: bytes.NewReader(distinctPNG(t, 31))})
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := application.Store.EnsureActivePromptVersion(ctx, "retry-prompt", "仅转录图中可见文字", strings.Repeat("d", 64))
	if err != nil {
		t.Fatal(err)
	}
	profile, err := application.SaveRecognizerProfile(ctx, app.RecognizerProfile{
		Name: "Original OpenAI", Driver: recognizer.DriverOpenAICompatible, BaseURL: original.URL,
		APIKey: "old-secret", Model: "original-model", ParamsJSON: `{"temperature":0,"max_tokens":128,"retry_attempts":1,"timeout_seconds":10}`,
		PromptVersionID: prompt.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	started, err := application.StartRecognitionWithOptions(ctx, doc.ID, app.RecognitionOptions{ProfileID: profile.ID})
	if err != nil {
		t.Fatal(err)
	}
	failed := waitForRun(t, application, started.Run.ID)
	if failed.Status != "failed" {
		t.Fatalf("first run = %+v", failed)
	}
	if strings.Contains(failed.ProfileSnapshotJSON, "old-secret") {
		t.Fatalf("run snapshot leaked secret: %s", failed.ProfileSnapshotJSON)
	}

	if _, err := application.SaveRecognizerProfile(ctx, app.RecognizerProfile{
		ID: profile.ID, Name: profile.Name, Driver: recognizer.DriverOpenAICompatible,
		BaseURL: "http://changed.invalid", APIKey: "current-secret", Model: "changed-model",
		ParamsJSON:      `{"temperature":1,"max_tokens":999,"retry_attempts":1,"timeout_seconds":10}`,
		PromptVersionID: prompt.ID,
	}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	succeed = true
	mu.Unlock()
	retry, err := application.RetryRun(ctx, failed.ID)
	if err != nil {
		t.Fatal(err)
	}
	retried := waitForRun(t, application, retry.Run.ID)
	if retried.Status != "succeeded" || retried.Model != "original-model" {
		t.Fatalf("retry run = %+v", retried)
	}
	if retried.ProfileSnapshotJSON != failed.ProfileSnapshotJSON || retried.PromptVersion != failed.PromptVersion ||
		!strings.Contains(retried.ConfigJSON, `"max_tokens":128`) || strings.Contains(retried.ConfigJSON, `"max_tokens":999`) {
		t.Fatalf("retry did not preserve immutable snapshot/config: first=%+v retry=%+v", failed, retried)
	}
	mu.Lock()
	defer mu.Unlock()
	if lastAuthorization != "Bearer current-secret" || lastModel != "original-model" {
		t.Fatalf("retry auth/model = %q / %q", lastAuthorization, lastModel)
	}
}

func TestRetryRunFailsIfOriginalOpenAIProfileWasDeleted(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "failed", http.StatusBadRequest)
	}))
	defer server.Close()
	application, _ := newTestApp(t, recognizer.MockRecognizer{})
	doc, err := application.ImportDocument(ctx, app.ImportOptions{Title: "删除 Profile"},
		app.ImportFile{Name: "page.png", Reader: bytes.NewReader(distinctPNG(t, 32))})
	if err != nil {
		t.Fatal(err)
	}
	prompt, _ := application.Store.EnsureActivePromptVersion(ctx, "deleted-profile", "转录", strings.Repeat("e", 64))
	profile, err := application.SaveRecognizerProfile(ctx, app.RecognizerProfile{
		Name: "Will Delete", Driver: recognizer.DriverOpenAICompatible, BaseURL: server.URL,
		APIKey: "secret", Model: "model-a", ParamsJSON: `{"max_tokens":128,"retry_attempts":1,"timeout_seconds":10}`,
		PromptVersionID: prompt.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	started, err := application.StartRecognitionWithOptions(ctx, doc.ID, app.RecognitionOptions{ProfileID: profile.ID})
	if err != nil {
		t.Fatal(err)
	}
	failed := waitForRun(t, application, started.Run.ID)
	if err := application.Store.DeleteRecognizerProfile(ctx, profile.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := application.RetryRun(ctx, failed.ID); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("retry error = %v", err)
	}
}

func TestRetryMockRunRebuildsFromSnapshotAfterProfileDeletion(t *testing.T) {
	ctx := context.Background()
	application, conn := newTestApp(t, recognizer.MockRecognizer{})
	doc, err := application.ImportDocument(ctx, app.ImportOptions{Title: "Mock 快照"},
		app.ImportFile{Name: "page.png", Reader: bytes.NewReader(distinctPNG(t, 33))})
	if err != nil {
		t.Fatal(err)
	}
	prompt, _ := application.Store.EnsureActivePromptVersion(ctx, "mock-retry", "转录", strings.Repeat("f", 64))
	profile, err := application.SaveRecognizerProfile(ctx, app.RecognizerProfile{
		Name: "Disposable Mock", Driver: recognizer.DriverMock, ParamsJSON: `{}`, PromptVersionID: prompt.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	started, err := application.StartRecognitionWithOptions(ctx, doc.ID, app.RecognitionOptions{ProfileID: profile.ID})
	if err != nil {
		t.Fatal(err)
	}
	completed := waitForRun(t, application, started.Run.ID)
	if _, err := conn.ExecContext(ctx, `UPDATE run_pages SET status = 'failed' WHERE run_id = ?; UPDATE recognition_runs SET status = 'failed' WHERE id = ?`, completed.ID, completed.ID); err != nil {
		t.Fatal(err)
	}
	if err := application.Store.DeleteRecognizerProfile(ctx, profile.ID); err != nil {
		t.Fatal(err)
	}
	retry, err := application.RetryRun(ctx, completed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retried := waitForRun(t, application, retry.Run.ID); retried.Status != "succeeded" || retried.Driver != recognizer.DriverMock {
		t.Fatalf("retry = %+v", retried)
	}
}

func TestRecognitionExperimentRunsVariantsSequentiallyAndTracksWinnerAndEdits(t *testing.T) {
	ctx := context.Background()
	application, conn := newTestApp(t, recognizer.MockRecognizer{})
	doc, err := application.ImportDocument(ctx, app.ImportOptions{Title: "A/B 实验"},
		app.ImportFile{Name: "page.png", Reader: bytes.NewReader(distinctPNG(t, 34))})
	if err != nil {
		t.Fatal(err)
	}
	page := mustFirstPage(t, application, doc.ID)
	promptA, _ := application.Store.EnsureActivePromptVersion(ctx, "experiment-a", "Prompt A", strings.Repeat("1", 64))
	promptB, _ := application.Store.CreatePromptVersion(ctx, "experiment-b", "Prompt B", strings.Repeat("2", 64))
	profile, err := application.SaveRecognizerProfile(ctx, app.RecognizerProfile{
		Name: "Experiment Mock", Driver: recognizer.DriverMock, ParamsJSON: `{}`, PromptVersionID: promptA.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	started, err := application.StartRecognitionExperiment(ctx, doc.ID, app.RecognitionExperimentOptions{
		Name: "Prompt A vs B", PageIDs: []string{page.ID},
		Variants: []app.RecognitionExperimentVariant{
			{Name: "A", ProfileID: profile.ID, PromptVersionID: promptA.ID, ImageSource: "original"},
			{Name: "B", ProfileID: profile.ID, PromptVersionID: promptB.ID, ImageSource: "original"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	job := waitForTerminalJob(t, application, started.Job.ID)
	if job.Status != "succeeded" {
		t.Fatalf("experiment job = %+v", job)
	}
	experiment, err := application.GetRecognitionExperiment(ctx, started.Experiment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if experiment.Status != "succeeded" || len(experiment.Variants) != 2 {
		t.Fatalf("experiment = %+v", experiment)
	}
	for _, variant := range experiment.Variants {
		if variant.Status != "succeeded" || len(variant.RunIDs) != 1 || len(variant.CurrentRunIDs) != 1 ||
			variant.CurrentRunIDs[0] != variant.RunIDs[0] || variant.AverageConfidence == nil || *variant.AverageConfidence != 0.5 {
			t.Fatalf("variant = %+v", variant)
		}
	}
	firstResults, err := application.Store.RecognitionResultsForRuns(ctx, experiment.Variants[0].RunIDs)
	if err != nil || len(firstResults) != 1 {
		t.Fatalf("results = %+v, err=%v", firstResults, err)
	}
	if _, err := application.SaveTextVersion(ctx, app.TextVersion{
		DocumentID: doc.ID, PageID: page.ID, Kind: "manual", Status: "draft",
		SourceResultID: firstResults[0].ID, Text: firstResults[0].Text + "人工修订",
	}); err != nil {
		t.Fatal(err)
	}
	experiment, err = application.GetRecognitionExperiment(ctx, experiment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if experiment.Variants[0].ManualEditDistance == 0 {
		t.Fatalf("edit distance was not updated: %+v", experiment.Variants[0])
	}
	if err := application.Store.SelectRecognitionExperimentWinner(ctx, experiment.ID, experiment.Variants[1].ID); err != nil {
		t.Fatal(err)
	}
	experiment, _ = application.GetRecognitionExperiment(ctx, experiment.ID)
	if experiment.WinnerVariantID != experiment.Variants[1].ID || !experiment.Variants[1].SelectedWinner {
		t.Fatalf("winner = %+v", experiment)
	}
	if _, err := conn.ExecContext(ctx, `UPDATE jobs SET status = 'failed' WHERE id = ?`, started.Job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Store.RequeueRecognitionExperimentJob(ctx, started.Job.ID, experiment.ID); err != nil {
		t.Fatal(err)
	}
	experiment, err = application.GetRecognitionExperiment(ctx, experiment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(experiment.Variants[0].RunIDs) != 1 || len(experiment.Variants[0].CurrentRunIDs) != 0 {
		t.Fatalf("retry run tracking = %+v", experiment.Variants[0])
	}
	if experiment.Variants[0].ManualEditDistance != 0 {
		t.Fatalf("retry reused historical edit distance: %+v", experiment.Variants[0])
	}
}

func TestRecognitionExperimentRetryUsesCreationSnapshot(t *testing.T) {
	ctx := context.Background()
	var mu sync.Mutex
	models := []string{}
	authorizations := []string{}
	original := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		models = append(models, body.Model)
		authorizations = append(authorizations, r.Header.Get("Authorization"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"实验快照"}}]}`))
	}))
	defer original.Close()
	changed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "changed endpoint must not be used", http.StatusBadRequest)
	}))
	defer changed.Close()

	block := make(chan struct{})
	application, _ := newTestApp(t, &scriptedRecognizer{block: block, failPages: map[int]bool{}})
	doc, err := application.ImportDocument(ctx, app.ImportOptions{Title: "实验快照"}, app.ImportFile{Name: "page.png", Reader: bytes.NewReader(distinctPNG(t, 41))})
	if err != nil {
		t.Fatal(err)
	}
	page := mustFirstPage(t, application, doc.ID)
	active, err := application.StartRecognition(ctx, doc.ID, []string{page.ID})
	if err != nil {
		t.Fatal(err)
	}
	prompt, _ := application.Store.EnsureActivePromptVersion(ctx, "experiment-snapshot", "转录", strings.Repeat("9", 64))
	profile, err := application.SaveRecognizerProfile(ctx, app.RecognizerProfile{
		Name: "Experiment Snapshot", Driver: recognizer.DriverOpenAICompatible, BaseURL: original.URL,
		APIKey: "old-secret", Model: "original-model", ParamsJSON: `{"max_tokens":128,"retry_attempts":1,"timeout_seconds":10}`,
		PromptVersionID: prompt.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	started, err := application.StartRecognitionExperiment(ctx, doc.ID, app.RecognitionExperimentOptions{
		Name: "snapshot", PageIDs: []string{page.ID}, Variants: []app.RecognitionExperimentVariant{
			{Name: "A", ProfileID: profile.ID}, {Name: "B", ProfileID: profile.ID},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	firstJob := waitForTerminalJob(t, application, started.Job.ID)
	if firstJob.Status != "failed" {
		t.Fatalf("first experiment should fail behind active run: %+v", firstJob)
	}
	close(block)
	if run := waitForRun(t, application, active.Run.ID); run.Status != "succeeded" {
		t.Fatalf("blocking run = %+v", run)
	}
	if _, err := application.SaveRecognizerProfile(ctx, app.RecognizerProfile{
		ID: profile.ID, Name: profile.Name, Driver: recognizer.DriverOpenAICompatible,
		BaseURL: changed.URL, APIKey: "current-secret", Model: "changed-model",
		ParamsJSON: `{"max_tokens":999,"retry_attempts":1,"timeout_seconds":10}`, PromptVersionID: prompt.ID,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.RetryJob(ctx, firstJob.ID); err != nil {
		t.Fatal(err)
	}
	retried := waitForTerminalJob(t, application, firstJob.ID)
	if retried.Status != "succeeded" {
		t.Fatalf("retried experiment = %+v", retried)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(models) != 2 || models[0] != "original-model" || models[1] != "original-model" {
		t.Fatalf("models = %+v", models)
	}
	if authorizations[0] != "Bearer current-secret" || authorizations[1] != "Bearer current-secret" {
		t.Fatalf("authorizations = %+v", authorizations)
	}
}

func TestRecoverInterruptedFinalizesRecognitionExperiment(t *testing.T) {
	ctx := context.Background()
	application, conn := newTestApp(t, recognizer.MockRecognizer{})
	if _, err := conn.Exec(`
		INSERT INTO documents(id, title, status, created_at, updated_at) VALUES ('recover-doc', '恢复实验', 'ready', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
		INSERT INTO jobs(id, type, status, target_type, target_id, payload_json, max_attempts, created_at) VALUES ('recover-job', 'recognition_experiment', 'running', 'recognition_experiment', 'recover-exp', '{}', 2, '2026-01-01T00:00:00Z');
		INSERT INTO recognition_experiments(id, document_id, job_id, name, page_ids_json, status, created_at) VALUES ('recover-exp', 'recover-doc', 'recover-job', '恢复', '[]', 'running', '2026-01-01T00:00:00Z');
		INSERT INTO recognition_experiment_variants(id, experiment_id, name, image_source, position, status, run_ids_json, created_at) VALUES
		  ('recover-running', 'recover-exp', 'A', 'original', 0, 'running', '["recover-run"]', '2026-01-01T00:00:00Z'),
		  ('recover-queued', 'recover-exp', 'B', 'original', 1, 'queued', '[]', '2026-01-01T00:00:00Z');
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Store.RecoverInterrupted(ctx); err != nil {
		t.Fatal(err)
	}
	experiment, err := application.Store.GetRecognitionExperiment(ctx, "recover-exp")
	if err != nil {
		t.Fatal(err)
	}
	if experiment.Status != "failed" || len(experiment.Variants) != 2 || experiment.Variants[0].Status != "failed" || experiment.Variants[1].Status != "failed" || len(experiment.Variants[0].RunIDs) != 1 {
		t.Fatalf("recovered experiment = %+v", experiment)
	}
	events, err := application.Store.ListJobEvents(ctx, "recover-job")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Stage != "interrupted" {
		t.Fatalf("recovery events = %+v", events)
	}
	queued, err := application.Store.RequeueRecognitionExperimentJob(ctx, "recover-job", "recover-exp")
	if err != nil || queued.Status != "queued" {
		t.Fatalf("requeue = %+v, err=%v", queued, err)
	}
	experiment, err = application.Store.GetRecognitionExperiment(ctx, "recover-exp")
	if err != nil || len(experiment.Variants[0].RunIDs) != 1 || experiment.Variants[0].RunIDs[0] != "recover-run" {
		t.Fatalf("retry lost run history: %+v, err=%v", experiment, err)
	}
}

func TestRunSnapshotBoundsAuthorPromptContext(t *testing.T) {
	ctx := context.Background()
	application, _ := newTestApp(t, recognizer.MockRecognizer{})
	doc, err := application.ImportDocument(ctx, app.ImportOptions{Title: "作者上下文边界"},
		app.ImportFile{Name: "page.png", Reader: bytes.NewReader(distinctPNG(t, 35))})
	if err != nil {
		t.Fatal(err)
	}
	author, err := application.Store.CreateAuthorProfile(ctx, "长上下文作者", strings.Repeat("注", 40_000))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Store.SetDocumentAuthorProfile(ctx, doc.ID, author.ID); err != nil {
		t.Fatal(err)
	}
	prompt, _ := application.Store.EnsureActivePromptVersion(ctx, "bounded-author", "转录", strings.Repeat("3", 64))
	profile, err := application.SaveRecognizerProfile(ctx, app.RecognizerProfile{
		Name: "Bounded Mock", Driver: recognizer.DriverMock, ParamsJSON: `{}`, PromptVersionID: prompt.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	started, err := application.StartRecognitionWithOptions(ctx, doc.ID, app.RecognitionOptions{ProfileID: profile.ID})
	if err != nil {
		t.Fatal(err)
	}
	run := waitForRun(t, application, started.Run.ID)
	var snapshot struct {
		AuthorPromptContext  string `json:"author_prompt_context"`
		AuthorContextOmitted bool   `json:"author_context_omitted"`
	}
	if err := json.Unmarshal([]byte(run.ProfileSnapshotJSON), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len([]rune(snapshot.AuthorPromptContext)) > 16_384 || !snapshot.AuthorContextOmitted {
		t.Fatalf("author snapshot was not bounded: runes=%d omitted=%v", len([]rune(snapshot.AuthorPromptContext)), snapshot.AuthorContextOmitted)
	}
}
