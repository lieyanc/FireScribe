package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lieyan/firescribe/internal/app"
)

func waitForCrossCheck(t *testing.T, application *app.App, checkID string) app.CrossCheck {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		check, err := application.Store.GetCrossCheck(context.Background(), checkID)
		if err != nil {
			t.Fatal(err)
		}
		switch check.Status {
		case "succeeded", "partial", "failed", "canceled":
			return check
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("cross check did not reach a terminal status")
	return app.CrossCheck{}
}

func mockProfile(t *testing.T, application *app.App, name string, isDefault bool) app.RecognizerProfile {
	t.Helper()
	profile, err := application.SaveRecognizerProfile(context.Background(), app.RecognizerProfile{
		Name: name, Driver: "mock", IsDefault: isDefault,
	})
	if err != nil {
		t.Fatal(err)
	}
	return profile
}

// activatePrompt mirrors production, where an active prompt version always
// exists; run snapshots need it to reconstruct openai-compatible recognizers.
func activatePrompt(t *testing.T, application *app.App) {
	t.Helper()
	if _, err := application.Store.EnsureActivePromptVersion(context.Background(), "cross-check-test", "请逐字转录页面内容。", "cc-test-sha"); err != nil {
		t.Fatal(err)
	}
}

// openAIStub serves an OpenAI-compatible chat completion that always returns
// the given page text, so two stubs give two models with controlled outputs.
func openAIStub(t *testing.T, text string) *httptest.Server {
	t.Helper()
	return openAIStubFailingFirst(t, text, 0)
}

// openAIStubFailingFirst fails the first failCount requests with HTTP 500 and
// then answers normally, producing a partial recognition run.
func openAIStubFailingFirst(t *testing.T, text string, failCount int) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	served := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		served++
		fail := served <= failCount
		mu.Unlock()
		if fail {
			http.Error(w, `{"error":{"message":"stub outage"}}`, http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"finish_reason": "stop", "message": map[string]any{"content": text}},
			},
		})
	}))
	t.Cleanup(server.Close)
	return server
}

func openAIProfile(t *testing.T, application *app.App, name, baseURL string) app.RecognizerProfile {
	t.Helper()
	profile, err := application.SaveRecognizerProfile(context.Background(), app.RecognizerProfile{
		Name: name, Driver: "openai-compatible", BaseURL: baseURL, APIKey: "test-key", Model: "test-vlm",
		ParamsJSON: `{"temperature":0,"max_tokens":4096,"max_image_edge":0,"retry_attempts":1,"timeout_seconds":10}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	return profile
}

func TestCrossCheckConsensusAndAdoption(t *testing.T) {
	ctx := context.Background()
	application, _ := newTestApp(t, &scriptedRecognizer{})
	doc := importThreePages(t, application)
	profileA := mockProfile(t, application, "甲模型", false)
	profileB := mockProfile(t, application, "乙模型", false)
	merger := mockProfile(t, application, "合并模型", false)

	start, err := application.StartCrossCheck(ctx, doc.ID, app.CrossCheckOptions{
		Variants: []app.CrossCheckVariant{
			{ProfileID: profileA.ID},
			{ProfileID: profileB.ID},
		},
		MergeProfileID: merger.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if start.CrossCheck.MergeProfileID != merger.ID {
		t.Fatalf("merge profile = %q, want %q", start.CrossCheck.MergeProfileID, merger.ID)
	}
	check := waitForCrossCheck(t, application, start.CrossCheck.ID)
	if check.Status != "succeeded" {
		t.Fatalf("cross check status = %s (error=%s)", check.Status, check.Error)
	}
	if check.ConsensusPages != 3 || check.DisagreementPages != 0 || check.FailedPages != 0 {
		t.Fatalf("counts = consensus %d disagreement %d failed %d", check.ConsensusPages, check.DisagreementPages, check.FailedPages)
	}
	for _, page := range check.Pages {
		if page.Status != "consensus" {
			t.Fatalf("page %d status = %s, want consensus", page.PageNo, page.Status)
		}
		if page.Agreement == nil || *page.Agreement != 1 {
			t.Fatalf("page %d agreement = %v, want 1", page.PageNo, page.Agreement)
		}
		if page.ConsensusVersionID == "" {
			t.Fatalf("page %d has no consensus version", page.PageNo)
		}
		if page.AnnotationID != "" {
			t.Fatalf("consensus page %d must not carry an annotation", page.PageNo)
		}
	}
	if check.Variants[0].Name != "甲模型" || check.Variants[1].Name != "乙模型" {
		t.Fatalf("variant names = %q, %q", check.Variants[0].Name, check.Variants[1].Name)
	}
	// The check row turns terminal before the job row does (same ordering as
	// experiments), so the job status is polled rather than asserted directly.
	jobDeadline := time.Now().Add(5 * time.Second)
	for {
		job, err := application.Store.GetJob(ctx, check.JobID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == "succeeded" {
			break
		}
		if job.Status != "queued" && job.Status != "running" {
			t.Fatalf("job status = %s (%s)", job.Status, job.LastError)
		}
		if time.Now().After(jobDeadline) {
			t.Fatalf("job never succeeded (status=%s)", job.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// A page a human already touched is never overwritten by bulk adoption.
	pages, err := application.Store.ListPages(ctx, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.SaveTextVersion(ctx, app.TextVersion{
		DocumentID: doc.ID, PageID: pages[0].ID, Kind: "manual", Text: "人工修订", Status: "draft", CreatedBy: "test",
	}); err != nil {
		t.Fatal(err)
	}

	adoption, err := application.AdoptCrossCheckConsensus(ctx, check.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(adoption.AdoptedPageIDs) != 2 {
		t.Fatalf("adopted %d pages, want 2 (skipped=%+v)", len(adoption.AdoptedPageIDs), adoption.Skipped)
	}
	if len(adoption.Skipped) != 1 || !strings.Contains(adoption.Skipped[0].Reason, "人工") {
		t.Fatalf("skipped = %+v, want the manually edited page", adoption.Skipped)
	}
	for _, pageID := range adoption.AdoptedPageIDs {
		version, found, err := application.Store.LatestFinalTextForPage(ctx, pageID)
		if err != nil || !found {
			t.Fatalf("adopted page %s has no final version (err=%v)", pageID, err)
		}
		if version.CreatedBy != "cross-check" {
			t.Fatalf("final created_by = %s", version.CreatedBy)
		}
		page, err := application.Store.GetPage(ctx, pageID)
		if err != nil {
			t.Fatal(err)
		}
		if page.Status != "verified" {
			t.Fatalf("adopted page status = %s, want verified", page.Status)
		}
	}

	again, err := application.AdoptCrossCheckConsensus(ctx, check.ID, adoption.AdoptedPageIDs[:1])
	if err != nil {
		t.Fatal(err)
	}
	if len(again.AdoptedPageIDs) != 0 || len(again.Skipped) != 1 {
		t.Fatalf("re-adoption = %+v", again)
	}

	// Canceling after natural completion must not flip the terminal status.
	if changed, err := application.Store.CancelCrossCheck(ctx, check.ID, "late cancel"); err != nil || changed != 0 {
		t.Fatalf("late cancel changed=%d err=%v, want 0 rows", changed, err)
	}
	final, err := application.Store.GetCrossCheck(ctx, check.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != "succeeded" {
		t.Fatalf("check status after late cancel = %s, want succeeded", final.Status)
	}
}

func TestCrossCheckDisagreementFlagsPageForReview(t *testing.T) {
	ctx := context.Background()
	application, _ := newTestApp(t, &scriptedRecognizer{})
	doc := importThreePages(t, application)
	activatePrompt(t, application)

	serverA := openAIStub(t, "共同行\n甲本独有")
	serverB := openAIStub(t, "共同行\n乙本独有")
	profileA := openAIProfile(t, application, "甲模型", serverA.URL)
	profileB := openAIProfile(t, application, "乙模型", serverB.URL)
	merger := mockProfile(t, application, "合并模型", false)

	pages, err := application.Store.ListPages(ctx, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	start, err := application.StartCrossCheck(ctx, doc.ID, app.CrossCheckOptions{
		PageIDs: []string{pages[0].ID},
		Variants: []app.CrossCheckVariant{
			{ProfileID: profileA.ID},
			{ProfileID: profileB.ID},
		},
		MergeProfileID: merger.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	check := waitForCrossCheck(t, application, start.CrossCheck.ID)
	if check.Status != "succeeded" {
		t.Fatalf("cross check status = %s (error=%s)", check.Status, check.Error)
	}
	if check.DisagreementPages != 1 || check.ConsensusPages != 0 {
		t.Fatalf("counts = %+v", check)
	}
	page := check.Pages[0]
	if page.Status != "disagreement" {
		t.Fatalf("page status = %s", page.Status)
	}
	if page.Agreement == nil || *page.Agreement >= 1 || *page.Agreement <= 0 {
		t.Fatalf("agreement = %v, want in (0, 1)", page.Agreement)
	}
	if page.MergedVersionID == "" {
		t.Fatal("disagreement page has no merged version")
	}
	merged, err := application.Store.GetTextVersion(ctx, page.MergedVersionID)
	if err != nil {
		t.Fatal(err)
	}
	// The mock merger keeps the first candidate, so the merge equals model A.
	if merged.Text != "共同行\n甲本独有" {
		t.Fatalf("merged text = %q", merged.Text)
	}
	if merged.CreatedBy != "conservative-merge" || merged.Kind != "candidate" {
		t.Fatalf("merged version = %+v", merged)
	}

	wantConflicts := map[string]string{"甲本独有": "partial", "乙本独有": "omitted"}
	if len(page.Conflicts) != len(wantConflicts) {
		t.Fatalf("conflicts = %+v", page.Conflicts)
	}
	for _, conflict := range page.Conflicts {
		if wantConflicts[conflict.Text] != conflict.Kind {
			t.Fatalf("conflict %q kind = %s, want %s", conflict.Text, conflict.Kind, wantConflicts[conflict.Text])
		}
	}

	if page.AnnotationID == "" {
		t.Fatal("disagreement page has no annotation")
	}
	annotation, err := application.Store.GetAnnotation(ctx, page.AnnotationID)
	if err != nil {
		t.Fatal(err)
	}
	if annotation.Kind != "uncertain_text" || annotation.Status != "open" {
		t.Fatalf("annotation = %+v", annotation)
	}
	if !strings.HasPrefix(annotation.Body, "[交叉核验]") {
		t.Fatalf("annotation body = %q", annotation.Body)
	}
	if annotation.TextVersionID != page.MergedVersionID {
		t.Fatalf("annotation anchored to %q, want merged version", annotation.TextVersionID)
	}

	queue, err := application.Store.ListReviewQueue(ctx, 0.6, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range queue {
		if item.PageID == page.PageID && item.OpenUncertainCount == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("disagreement page missing from review queue: %+v", queue)
	}

	// Adoption is reserved for consensus pages; the reviewer decides here.
	adoption, err := application.AdoptCrossCheckConsensus(ctx, check.ID, []string{page.PageID})
	if err != nil {
		t.Fatal(err)
	}
	if len(adoption.AdoptedPageIDs) != 0 || len(adoption.Skipped) != 1 {
		t.Fatalf("disagreement adoption = %+v", adoption)
	}

	// A re-check resolves the superseded machine annotation instead of stacking.
	second, err := application.StartCrossCheck(ctx, doc.ID, app.CrossCheckOptions{
		PageIDs: []string{pages[0].ID},
		Variants: []app.CrossCheckVariant{
			{ProfileID: profileA.ID},
			{ProfileID: profileB.ID},
		},
		MergeProfileID: merger.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForCrossCheck(t, application, second.CrossCheck.ID)
	annotations, err := application.Store.ListAnnotations(ctx, doc.ID, pages[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	open := 0
	for _, item := range annotations {
		if item.Kind == "uncertain_text" && item.Status == "open" {
			open++
		}
	}
	if open != 1 {
		t.Fatalf("open uncertain annotations = %d, want 1 (%+v)", open, annotations)
	}

	// The reviewer's sign-off (saving a final) resolves the machine note, so
	// the decided page leaves the review queue instead of staying pinned.
	if _, err := application.SaveTextVersion(ctx, app.TextVersion{
		DocumentID: doc.ID, PageID: pages[0].ID, Kind: "final", Text: "共同行\n人工定稿", Status: "verified", CreatedBy: "reviewer",
	}); err != nil {
		t.Fatal(err)
	}
	annotations, err = application.Store.ListAnnotations(ctx, doc.ID, pages[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range annotations {
		if item.Kind == "uncertain_text" && item.Status == "open" {
			t.Fatalf("cross-check annotation still open after human finalization: %+v", item)
		}
	}
}

func TestStartCrossCheckValidation(t *testing.T) {
	ctx := context.Background()
	application, _ := newTestApp(t, &scriptedRecognizer{})
	doc := importThreePages(t, application)
	profile := mockProfile(t, application, "甲模型", false)

	if _, err := application.StartCrossCheck(ctx, doc.ID, app.CrossCheckOptions{
		Variants:       []app.CrossCheckVariant{{ProfileID: profile.ID}},
		MergeProfileID: profile.ID,
	}); err == nil || !strings.Contains(err.Error(), "between 2 and 8") {
		t.Fatalf("single variant error = %v", err)
	}

	if _, err := application.StartCrossCheck(ctx, doc.ID, app.CrossCheckOptions{
		PageIDs: []string{"pag_missing"},
		Variants: []app.CrossCheckVariant{
			{ProfileID: profile.ID}, {ProfileID: profile.ID, Name: "乙"},
		},
		MergeProfileID: profile.ID,
	}); err == nil || !strings.Contains(err.Error(), "does not belong") {
		t.Fatalf("unknown page error = %v", err)
	}

	// The legacy scripted recognizer cannot merge, so with neither an explicit
	// merge profile nor a default profile the check must fail fast.
	if _, err := application.StartCrossCheck(ctx, doc.ID, app.CrossCheckOptions{
		Variants: []app.CrossCheckVariant{
			{ProfileID: profile.ID}, {ProfileID: profile.ID, Name: "乙"},
		},
	}); err == nil || !strings.Contains(err.Error(), "candidate merging") {
		t.Fatalf("merger validation error = %v", err)
	}

	// Explicit duplicate names collide; auto-generated ones are suffixed instead.
	if _, err := application.StartCrossCheck(ctx, doc.ID, app.CrossCheckOptions{
		Variants: []app.CrossCheckVariant{
			{ProfileID: profile.ID, Name: "同名"}, {ProfileID: profile.ID, Name: "同名"},
		},
		MergeProfileID: profile.ID,
	}); err == nil || !strings.Contains(err.Error(), "distinct names") {
		t.Fatalf("duplicate name error = %v", err)
	}

	// The same profile twice with auto-generated names is a supported setup
	// (e.g. self-agreement sampling): names disambiguate as 甲模型 / 甲模型 #2.
	autoNamed, err := application.StartCrossCheck(ctx, doc.ID, app.CrossCheckOptions{
		Variants: []app.CrossCheckVariant{
			{ProfileID: profile.ID}, {ProfileID: profile.ID},
		},
		MergeProfileID: profile.ID,
	})
	if err != nil {
		t.Fatalf("auto-named duplicate profiles should start: %v", err)
	}
	if autoNamed.CrossCheck.Variants[0].Name != "甲模型" || autoNamed.CrossCheck.Variants[1].Name != "甲模型 #2" {
		t.Fatalf("auto-disambiguated names = %q, %q", autoNamed.CrossCheck.Variants[0].Name, autoNamed.CrossCheck.Variants[1].Name)
	}
	waitForCrossCheck(t, application, autoNamed.CrossCheck.ID)

	// While a cross-check is active, a second one is rejected.
	blocker := mockProfile(t, application, "乙模型", false)
	first, err := application.StartCrossCheck(ctx, doc.ID, app.CrossCheckOptions{
		Variants: []app.CrossCheckVariant{
			{ProfileID: profile.ID}, {ProfileID: blocker.ID},
		},
		MergeProfileID: profile.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = application.StartCrossCheck(ctx, doc.ID, app.CrossCheckOptions{
		Variants: []app.CrossCheckVariant{
			{ProfileID: profile.ID}, {ProfileID: blocker.ID},
		},
		MergeProfileID: profile.ID,
	})
	if !errors.Is(err, app.ErrCrossCheckActive) {
		t.Fatalf("concurrent cross check error = %v", err)
	}
	waitForCrossCheck(t, application, first.CrossCheck.ID)
}

func TestCrossCheckFailsWithoutComparableRuns(t *testing.T) {
	ctx := context.Background()
	application, _ := newTestApp(t, &scriptedRecognizer{})
	doc := importThreePages(t, application)
	activatePrompt(t, application)

	// Both variants point at a dead endpoint, so no run can succeed.
	dead := openAIStub(t, "")
	deadURL := dead.URL
	dead.Close()
	profileA := openAIProfile(t, application, "甲模型", deadURL)
	profileB := openAIProfile(t, application, "乙模型", deadURL)
	merger := mockProfile(t, application, "合并模型", false)

	start, err := application.StartCrossCheck(ctx, doc.ID, app.CrossCheckOptions{
		Variants: []app.CrossCheckVariant{
			{ProfileID: profileA.ID}, {ProfileID: profileB.ID},
		},
		MergeProfileID: merger.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	check := waitForCrossCheck(t, application, start.CrossCheck.ID)
	if check.Status != "failed" {
		t.Fatalf("cross check status = %s, want failed", check.Status)
	}
	if !strings.Contains(check.Error, "可比较的模型结果不足") {
		t.Fatalf("cross check error = %q", check.Error)
	}
	for _, page := range check.Pages {
		if page.Status != "failed" {
			t.Fatalf("page %d status = %s, want failed", page.PageNo, page.Status)
		}
	}
	job, err := application.Store.GetJob(ctx, check.JobID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "failed" {
		t.Fatalf("job status = %s", job.Status)
	}
}

func TestCrossCheckJobCancelStopsRuns(t *testing.T) {
	ctx := context.Background()
	release := make(chan struct{})
	var once sync.Once
	blocking := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"finish_reason": "stop", "message": map[string]any{"content": "文本"}}},
		})
	}))
	t.Cleanup(func() { once.Do(func() { close(release) }); blocking.Close() })

	application, _ := newTestApp(t, &scriptedRecognizer{})
	doc := importThreePages(t, application)
	activatePrompt(t, application)
	profileA := openAIProfile(t, application, "甲模型", blocking.URL)
	profileB := openAIProfile(t, application, "乙模型", blocking.URL)
	merger := mockProfile(t, application, "合并模型", false)

	start, err := application.StartCrossCheck(ctx, doc.ID, app.CrossCheckOptions{
		Variants: []app.CrossCheckVariant{
			{ProfileID: profileA.ID}, {ProfileID: profileB.ID},
		},
		MergeProfileID: merger.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Wait for the first variant's run to appear, then cancel the whole job.
	deadline := time.Now().Add(5 * time.Second)
	runID := ""
	for time.Now().Before(deadline) && runID == "" {
		check, err := application.Store.GetCrossCheck(ctx, start.CrossCheck.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, variant := range check.Variants {
			if variant.RunID != "" {
				runID = variant.RunID
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if runID == "" {
		t.Fatal("no variant run started")
	}
	// While the check holds the document reservation, manual recognition runs
	// are always rejected — by the run-slot while a variant run is live, and by
	// the cross-check reservation in the gaps between variants.
	if _, err := application.StartRecognition(ctx, doc.ID, nil); !errors.Is(err, app.ErrCrossCheckActive) && !errors.Is(err, app.ErrRecognitionActive) {
		t.Fatalf("manual run during cross check error = %v, want an exclusion error", err)
	}
	if err := application.CancelJob(ctx, start.Job.ID); err != nil {
		t.Fatal(err)
	}
	once.Do(func() { close(release) })

	check := waitForCrossCheck(t, application, start.CrossCheck.ID)
	if check.Status != "canceled" {
		t.Fatalf("cross check status = %s, want canceled", check.Status)
	}
	for _, variant := range check.Variants {
		if variant.Status == "queued" || variant.Status == "running" {
			t.Fatalf("variant %s left in %s inside a canceled check", variant.Name, variant.Status)
		}
	}
	run := waitForRun(t, application, runID)
	if run.Status != "canceled" {
		t.Fatalf("variant run status = %s, want canceled", run.Status)
	}
	// The document is free for new work immediately afterwards.
	waitDeadline := time.Now().Add(5 * time.Second)
	for {
		_, running, err := application.Store.ActiveRecognitionRun(ctx, doc.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !running {
			break
		}
		if time.Now().After(waitDeadline) {
			t.Fatal("recognition run still active after cancel")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestCrossCheckPartialCoverageIsNotConsensus(t *testing.T) {
	ctx := context.Background()
	application, _ := newTestApp(t, &scriptedRecognizer{})
	doc := importThreePages(t, application)
	activatePrompt(t, application)

	sameText := "共同行\n完全一致"
	serverA := openAIStub(t, sameText)
	serverB := openAIStub(t, sameText)
	// C fails exactly its first page request, so its run ends partial and one
	// page is missing C's result even though A and B agree on it.
	serverC := openAIStubFailingFirst(t, sameText, 1)
	profileA := openAIProfile(t, application, "甲模型", serverA.URL)
	profileB := openAIProfile(t, application, "乙模型", serverB.URL)
	profileC := openAIProfile(t, application, "丙模型", serverC.URL)
	merger := mockProfile(t, application, "合并模型", false)

	start, err := application.StartCrossCheck(ctx, doc.ID, app.CrossCheckOptions{
		Variants: []app.CrossCheckVariant{
			{ProfileID: profileA.ID}, {ProfileID: profileB.ID}, {ProfileID: profileC.ID},
		},
		MergeProfileID: merger.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	check := waitForCrossCheck(t, application, start.CrossCheck.ID)
	// The check itself is partial (variant C lost a page), and the
	// under-covered page must NOT be presented as full consensus.
	if check.Status != "partial" {
		t.Fatalf("check status = %s (error=%s), want partial", check.Status, check.Error)
	}
	if check.ConsensusPages != 2 || check.DisagreementPages != 1 {
		t.Fatalf("counts = consensus %d disagreement %d failed %d", check.ConsensusPages, check.DisagreementPages, check.FailedPages)
	}
	var underCovered app.CrossCheckPage
	for _, page := range check.Pages {
		if page.Status == "disagreement" {
			underCovered = page
		}
	}
	if underCovered.PageID == "" {
		t.Fatalf("no under-covered page found: %+v", check.Pages)
	}
	if len(underCovered.ResultIDs) != 2 {
		t.Fatalf("under-covered page results = %d, want 2", len(underCovered.ResultIDs))
	}
	if !strings.Contains(underCovered.Error, "丙模型") {
		t.Fatalf("under-covered page error = %q, want missing-variant note", underCovered.Error)
	}
	if underCovered.MergedVersionID != "" {
		t.Fatalf("identical texts must not be merged, got merged version %s", underCovered.MergedVersionID)
	}
	if underCovered.AnnotationID == "" {
		t.Fatal("under-covered page must carry an annotation for review")
	}
	annotation, err := application.Store.GetAnnotation(ctx, underCovered.AnnotationID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(annotation.Body, "覆盖不完整") {
		t.Fatalf("annotation body = %q", annotation.Body)
	}

	// Bulk adoption finalizes only the fully covered consensus pages.
	adoption, err := application.AdoptCrossCheckConsensus(ctx, check.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(adoption.AdoptedPageIDs) != 2 {
		t.Fatalf("adopted %d pages, want 2 (skipped=%+v)", len(adoption.AdoptedPageIDs), adoption.Skipped)
	}
	for _, pageID := range adoption.AdoptedPageIDs {
		if pageID == underCovered.PageID {
			t.Fatal("under-covered page was adopted")
		}
	}
}

func TestCrossCheckAdoptionSkipsPagesWithNewerCheck(t *testing.T) {
	ctx := context.Background()
	application, _ := newTestApp(t, &scriptedRecognizer{})
	doc := importThreePages(t, application)
	activatePrompt(t, application)
	pages, err := application.Store.ListPages(ctx, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	merger := mockProfile(t, application, "合并模型", false)
	profileA := mockProfile(t, application, "甲模型", false)
	profileB := mockProfile(t, application, "乙模型", false)

	first, err := application.StartCrossCheck(ctx, doc.ID, app.CrossCheckOptions{
		PageIDs:        []string{pages[0].ID},
		Variants:       []app.CrossCheckVariant{{ProfileID: profileA.ID}, {ProfileID: profileB.ID}},
		MergeProfileID: merger.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	firstCheck := waitForCrossCheck(t, application, first.CrossCheck.ID)
	if firstCheck.ConsensusPages != 1 {
		t.Fatalf("first check = %+v", firstCheck)
	}

	// A newer check on the same page finds disagreement (openai stubs differ).
	serverA := openAIStub(t, "新版甲")
	serverB := openAIStub(t, "新版乙")
	newerA := openAIProfile(t, application, "新甲", serverA.URL)
	newerB := openAIProfile(t, application, "新乙", serverB.URL)
	second, err := application.StartCrossCheck(ctx, doc.ID, app.CrossCheckOptions{
		PageIDs:        []string{pages[0].ID},
		Variants:       []app.CrossCheckVariant{{ProfileID: newerA.ID}, {ProfileID: newerB.ID}},
		MergeProfileID: merger.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForCrossCheck(t, application, second.CrossCheck.ID)

	// Adopting from the superseded first check must be refused.
	adoption, err := application.AdoptCrossCheckConsensus(ctx, firstCheck.ID, []string{pages[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(adoption.AdoptedPageIDs) != 0 || len(adoption.Skipped) != 1 || !strings.Contains(adoption.Skipped[0].Reason, "更新") {
		t.Fatalf("stale adoption = %+v", adoption)
	}
}

// TestCrossCheckReservationBlocksRunBetweenVariants pins the exact window the
// review flagged: after variant A's run releases and before variant B's run
// registers, no per-run slot is held — only the cross-check reservation stops
// a manual run from stealing the document and failing the remaining variants.
func TestCrossCheckReservationBlocksRunBetweenVariants(t *testing.T) {
	ctx := context.Background()
	gate := make(chan struct{})
	var mu sync.Mutex
	served := 0
	// The stub blocks the very first request (variant A, page 1) until the test
	// has probed the reservation, guaranteeing we observe the between-variants
	// invariant deterministically rather than by timing.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		served++
		first := served == 1
		mu.Unlock()
		if first {
			<-gate
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"finish_reason": "stop", "message": map[string]any{"content": "文本"}}},
		})
	}))
	defer server.Close()

	application, _ := newTestApp(t, &scriptedRecognizer{})
	doc := importThreePages(t, application)
	activatePrompt(t, application)
	profileA := openAIProfile(t, application, "甲模型", server.URL)
	profileB := openAIProfile(t, application, "乙模型", server.URL)
	merger := mockProfile(t, application, "合并模型", false)

	start, err := application.StartCrossCheck(ctx, doc.ID, app.CrossCheckOptions{
		Variants:       []app.CrossCheckVariant{{ProfileID: profileA.ID}, {ProfileID: profileB.ID}},
		MergeProfileID: merger.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	// The job is queued/running but its first provider call is parked, so a
	// manual run is rejected by the reservation.
	if _, err := application.StartRecognition(ctx, doc.ID, nil); !errors.Is(err, app.ErrCrossCheckActive) && !errors.Is(err, app.ErrRecognitionActive) {
		t.Fatalf("manual run during reserved check = %v, want an exclusion error", err)
	}
	close(gate)

	check := waitForCrossCheck(t, application, start.CrossCheck.ID)
	if check.Status != "succeeded" {
		t.Fatalf("check status = %s (error=%s), want succeeded", check.Status, check.Error)
	}
	// After the check finishes, the document is immediately free.
	if _, _, err := application.Store.ActiveRecognitionRun(ctx, doc.ID); err != nil {
		t.Fatal(err)
	}
	manual, err := application.StartRecognition(ctx, doc.ID, nil)
	if err != nil {
		t.Fatalf("manual run after check = %v", err)
	}
	waitForRun(t, application, manual.Run.ID)
}
