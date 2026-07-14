package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"

	"github.com/lieyan/firescribe/internal/recognizer"
)

// ErrCrossCheckActive is returned when a document already has a queued or
// running cross-check; the API layer maps it to 409 Conflict.
var ErrCrossCheckActive = errors.New("cross check is already running for this document")

const crossCheckAnnotationPrefix = "[交叉核验]"

const (
	maxCrossCheckConflicts       = 100
	maxCrossCheckConflictRunes   = 200
	maxCrossCheckAnnotationLines = 10
)

type CrossCheckStart struct {
	CrossCheck CrossCheck `json:"cross_check"`
	Job        Job        `json:"job"`
}

type CrossCheckOptions struct {
	Name           string
	PageIDs        []string
	Variants       []CrossCheckVariant
	MergeProfileID string
}

type crossCheckJobPayload struct {
	CrossCheckID string `json:"cross_check_id"`
	DocumentID   string `json:"document_id"`
}

type CrossCheckAdoptionSkip struct {
	PageID string `json:"page_id"`
	PageNo int    `json:"page_no"`
	Reason string `json:"reason"`
}

type CrossCheckAdoption struct {
	AdoptedPageIDs []string                 `json:"adopted_page_ids"`
	Skipped        []CrossCheckAdoptionSkip `json:"skipped"`
	CrossCheck     CrossCheck               `json:"cross_check"`
}

// reserveCrossCheck claims the document for a cross-check for the whole job
// lifetime, so manual recognition runs cannot steal the per-document run slot
// between variants. It shares runMu with the recognition-run bookkeeping,
// which makes the exclusion between checks and runs atomic in-process.
func (a *App) reserveCrossCheck(documentID, checkID string) error {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	if _, active := a.activeRuns[documentID]; active {
		return ErrRecognitionActive
	}
	if current := a.activeCrossChecks[documentID]; current != "" && current != checkID {
		return ErrCrossCheckActive
	}
	a.activeCrossChecks[documentID] = checkID
	return nil
}

func (a *App) releaseCrossCheck(documentID, checkID string) {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	if a.activeCrossChecks[documentID] == checkID {
		delete(a.activeCrossChecks, documentID)
	}
}

// StartCrossCheck validates and queues a cross-check: every variant recognizes
// the same pages, outputs are compared page by page, and disagreements are
// conservatively merged and flagged for human review. Nothing is finalized
// automatically; adoption is a separate, explicit user action.
func (a *App) StartCrossCheck(ctx context.Context, documentID string, options CrossCheckOptions) (CrossCheckStart, error) {
	if _, err := a.Store.GetDocument(ctx, documentID); err != nil {
		return CrossCheckStart{}, err
	}
	if len(options.Variants) < 2 || len(options.Variants) > 8 {
		return CrossCheckStart{}, errors.New("cross check requires between 2 and 8 variants")
	}
	allPages, err := a.Store.ListPages(ctx, documentID)
	if err != nil {
		return CrossCheckStart{}, err
	}
	if len(allPages) == 0 {
		return CrossCheckStart{}, errors.New("document has no pages to cross check")
	}
	targets := allPages
	if len(options.PageIDs) > 0 {
		valid := make(map[string]Page, len(allPages))
		for _, page := range allPages {
			valid[page.ID] = page
		}
		seen := make(map[string]bool, len(options.PageIDs))
		targets = nil
		for _, pageID := range options.PageIDs {
			pageID = strings.TrimSpace(pageID)
			page, ok := valid[pageID]
			if pageID == "" || !ok {
				return CrossCheckStart{}, fmt.Errorf("page %q does not belong to document", pageID)
			}
			if seen[pageID] {
				return CrossCheckStart{}, fmt.Errorf("duplicate cross check page %q", pageID)
			}
			seen[pageID] = true
			targets = append(targets, page)
		}
		sort.Slice(targets, func(i, j int) bool { return targets[i].PageNo < targets[j].PageNo })
	}
	// Leftover active rows from a crashed process (belt; RecoverInterrupted
	// normally clears these at startup). In-process exclusion is enforced
	// atomically by reserveCrossCheck below.
	if active, err := a.Store.ActiveCrossCheckForDocument(ctx, documentID); err != nil {
		return CrossCheckStart{}, err
	} else if active {
		return CrossCheckStart{}, ErrCrossCheckActive
	}
	if _, running, err := a.Store.ActiveRecognitionRun(ctx, documentID); err != nil {
		return CrossCheckStart{}, err
	} else if running {
		return CrossCheckStart{}, ErrRecognitionActive
	}

	timestamp := now()
	check := CrossCheck{
		ID: newID("cc"), DocumentID: documentID, JobID: newID("job"),
		Name: strings.TrimSpace(options.Name), Status: "queued", CreatedAt: timestamp,
	}
	if check.Name == "" {
		check.Name = "交叉核验"
	}
	if len([]rune(check.Name)) > 128 {
		return CrossCheckStart{}, errors.New("cross check name must not exceed 128 characters")
	}
	check.PageIDs = make([]string, 0, len(targets))
	for _, page := range targets {
		check.PageIDs = append(check.PageIDs, page.ID)
	}

	variants := make([]CrossCheckVariant, 0, len(options.Variants))
	autoNamed := make([]bool, 0, len(options.Variants))
	for index, input := range options.Variants {
		input.ProfileID = strings.TrimSpace(input.ProfileID)
		input.ProviderAdapterID = strings.TrimSpace(input.ProviderAdapterID)
		input.PromptVersionID = strings.TrimSpace(input.PromptVersionID)
		if input.ProfileID != "" && input.ProviderAdapterID != "" {
			return CrossCheckStart{}, fmt.Errorf("variant %d selects both a profile and provider adapter", index+1)
		}
		_, resolvedProfile, resolvedAdapter, resolvedPrompt, authorContext, snapshotErr := a.recognizerForRun(ctx, documentID, input.ProfileID, input.ProviderAdapterID, input.PromptVersionID)
		if snapshotErr != nil {
			return CrossCheckStart{}, fmt.Errorf("variant %d: %w", index+1, snapshotErr)
		}
		input.SnapshotJSON = recognizerProfileSnapshot(resolvedProfile, resolvedAdapter, resolvedPrompt, authorContext)
		input.ProfileID = resolvedProfile.ID
		input.ProviderAdapterID = resolvedAdapter.ID
		input.PromptVersionID = resolvedPrompt.ID
		input.ImageSource, err = normalizeImageSource(input.ImageSource)
		if err != nil {
			return CrossCheckStart{}, fmt.Errorf("variant %d: %w", index+1, err)
		}
		input.ID = newID("ccv")
		input.CrossCheckID = check.ID
		input.Name = strings.TrimSpace(input.Name)
		autoNamed = append(autoNamed, input.Name == "")
		if input.Name == "" {
			input.Name = defaultCrossCheckVariantName(resolvedProfile, resolvedAdapter, index)
		}
		if len([]rune(input.Name)) > 128 {
			return CrossCheckStart{}, fmt.Errorf("variant %d name must not exceed 128 characters", index+1)
		}
		input.Position = index
		input.Status = "queued"
		input.CreatedAt = timestamp
		variants = append(variants, input)
	}
	disambiguateCrossCheckVariantNames(variants, autoNamed)
	if name := duplicateCrossCheckVariantName(variants); name != "" {
		return CrossCheckStart{}, fmt.Errorf("duplicate variant name %q; give variants distinct names", name)
	}

	mergeProfileID, err := a.validateCrossCheckMerger(ctx, options.MergeProfileID)
	if err != nil {
		return CrossCheckStart{}, err
	}
	check.MergeProfileID = mergeProfileID
	check.Variants = variants

	job := Job{
		ID: check.JobID, Type: "cross_check", Status: "queued",
		TargetType: "cross_check", TargetID: check.ID,
		PayloadJSON: mustJSON(crossCheckJobPayload{CrossCheckID: check.ID, DocumentID: documentID}),
		MaxAttempts: 2, ProgressTotal: len(variants) + len(targets),
		ProgressMessage: "等待交叉核验", CreatedAt: timestamp,
	}
	// Reserve the document before the rows exist so no recognition run (or
	// competing cross-check) can slip in between the 202 and the job start.
	if err := a.reserveCrossCheck(documentID, check.ID); err != nil {
		return CrossCheckStart{}, err
	}
	if err := a.Store.CreateCrossCheck(ctx, check, variants, targets, job); err != nil {
		a.releaseCrossCheck(documentID, check.ID)
		return CrossCheckStart{}, err
	}
	a.launchJob(job.ID)
	return CrossCheckStart{CrossCheck: check, Job: job}, nil
}

// defaultCrossCheckVariantName prefers the human-recognizable profile/adapter
// name over positional letters, since conflicts quote variant names.
func defaultCrossCheckVariantName(profile RecognizerProfile, adapter ProviderAdapter, index int) string {
	if adapter.ID != "" && strings.TrimSpace(adapter.Name) != "" {
		return adapter.Name
	}
	if strings.TrimSpace(profile.Name) != "" && profile.Name != "legacy-settings" {
		return profile.Name
	}
	return fmt.Sprintf("Variant %c", 'A'+index)
}

// disambiguateCrossCheckVariantNames suffixes auto-generated duplicates (the
// same profile used twice, e.g. with different prompts) instead of rejecting;
// explicit user-supplied duplicates still fail the later uniqueness check.
func disambiguateCrossCheckVariantNames(variants []CrossCheckVariant, autoNamed []bool) {
	seen := make(map[string]bool, len(variants))
	for index := range variants {
		name := variants[index].Name
		if !seen[name] {
			seen[name] = true
			continue
		}
		if !autoNamed[index] {
			continue
		}
		for suffix := 2; ; suffix++ {
			candidate := fmt.Sprintf("%s #%d", name, suffix)
			if !seen[candidate] {
				variants[index].Name = candidate
				seen[candidate] = true
				break
			}
		}
	}
}

func duplicateCrossCheckVariantName(variants []CrossCheckVariant) string {
	seen := make(map[string]bool, len(variants))
	for _, variant := range variants {
		if seen[variant.Name] {
			return variant.Name
		}
		seen[variant.Name] = true
	}
	return ""
}

// validateCrossCheckMerger fails fast when the recognizer that would merge
// disagreements cannot merge at all, instead of surfacing the error per page
// mid-job. It returns the pinned profile ID ("" = legacy settings recognizer).
func (a *App) validateCrossCheckMerger(ctx context.Context, requestedProfileID string) (string, error) {
	profile, hasProfile, err := a.resolveRecognizerProfile(ctx, requestedProfileID)
	if err != nil {
		return "", fmt.Errorf("merge recognizer profile: %w", err)
	}
	var rec recognizer.Recognizer
	if hasProfile {
		prompt, err := a.resolveRunPrompt(ctx, "", profile.PromptVersionID)
		if err != nil {
			return "", fmt.Errorf("merge recognizer prompt: %w", err)
		}
		rec, err = a.registry.Build(profile.Driver, recognizer.ProfileConfig{
			BaseURL: profile.BaseURL, APIKey: profile.APIKey, Model: profile.Model, ParamsJSON: profile.ParamsJSON,
			PromptText: prompt.Content, PromptVersion: prompt.Version,
		})
		if err != nil {
			return "", fmt.Errorf("merge recognizer: %w", err)
		}
	} else {
		rec = a.Recognizer()
	}
	if _, ok := rec.(recognizer.CandidateMerger); !ok {
		driver := profile.Driver
		if driver == "" {
			driver = rec.Provider()
		}
		return "", fmt.Errorf("merge recognizer driver %q does not support candidate merging", driver)
	}
	if hasProfile {
		return profile.ID, nil
	}
	return "", nil
}

func (a *App) runCrossCheckJob(ctx context.Context, job Job) (CrossCheck, error) {
	dbCtx := context.Background()
	var payload crossCheckJobPayload
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		return CrossCheck{}, fmt.Errorf("decode cross check payload: %w", err)
	}
	// The reservation from StartCrossCheck must be released on EVERY exit —
	// including early failures below — or the document stays locked until
	// restart. releaseCrossCheck is a no-op when this check does not hold it.
	defer a.releaseCrossCheck(payload.DocumentID, payload.CrossCheckID)
	check, err := a.Store.GetCrossCheck(dbCtx, payload.CrossCheckID)
	if err != nil {
		return CrossCheck{}, err
	}
	// StartCrossCheck reserved the slot for the first execution; a retry via
	// RetryJob re-acquires it here. Idempotent when already held by this check.
	if err := a.reserveCrossCheck(check.DocumentID, check.ID); err != nil {
		_ = a.Store.FinishCrossCheck(dbCtx, check.ID, "failed", err.Error())
		return CrossCheck{}, err
	}
	if err := a.Store.MarkCrossCheckRunning(dbCtx, check.ID); err != nil {
		return CrossCheck{}, err
	}
	total := len(check.Variants) + len(check.Pages)
	done := 0
	var failures []string
	cancelOut := func() (CrossCheck, error) {
		_, _ = a.Store.CancelPendingCrossCheckPages(dbCtx, check.ID, cancelCause(ctx))
		_ = a.Store.CancelPendingCrossCheckVariants(dbCtx, check.ID, cancelCause(ctx))
		_, _ = a.Store.CancelCrossCheck(dbCtx, check.ID, cancelCause(ctx))
		return CrossCheck{}, ctx.Err()
	}

	type crossCheckRun struct {
		runID   string
		variant CrossCheckVariant
	}
	comparableRuns := make([]crossCheckRun, 0, len(check.Variants))
	for index, variant := range check.Variants {
		if ctx.Err() != nil {
			return cancelOut()
		}
		_ = a.Store.MarkCrossCheckVariantRunning(dbCtx, variant.ID)
		_ = a.Store.UpdateJobProgress(dbCtx, job.ID, done, total, "正在运行 "+variant.Name)
		rec, preparedRun, variantErr := a.recognizerForCrossCheckVariant(ctx, check.DocumentID, variant)
		var start RecognitionStart
		if variantErr == nil {
			start, variantErr = a.StartRecognitionWithOptions(ctx, check.DocumentID, RecognitionOptions{
				PageIDs: check.PageIDs, InputSource: variant.ImageSource,
				preparedRecognizer: rec, preparedSnapshotJSON: variant.SnapshotJSON, preparedRun: preparedRun,
				crossCheckID: check.ID,
			})
		}
		if variantErr != nil {
			failures = append(failures, variant.Name+": "+variantErr.Error())
			_ = a.Store.FinishCrossCheckVariant(dbCtx, variant.ID, "failed", "", variantErr.Error())
			done++
			continue
		}
		_ = a.Store.SetCrossCheckVariantRun(dbCtx, variant.ID, start.Run.ID)
		terminal, waitErr := a.waitRecognitionRun(ctx, start.Run.ID)
		if waitErr != nil {
			_ = a.CancelRun(dbCtx, start.Run.ID)
			if ctx.Err() != nil {
				_ = a.Store.FinishCrossCheckVariant(dbCtx, variant.ID, "canceled", start.Run.ID, cancelCause(ctx))
				return cancelOut()
			}
			_ = a.Store.FinishCrossCheckVariant(dbCtx, variant.ID, "failed", start.Run.ID, waitErr.Error())
			failures = append(failures, variant.Name+": "+waitErr.Error())
			done++
			continue
		}
		_ = a.waitRecognitionRunReleased(ctx, start.Run.ID)
		_ = a.Store.FinishCrossCheckVariant(dbCtx, variant.ID, terminal.Status, start.Run.ID, terminal.Error)
		if terminal.Status == "succeeded" || terminal.Status == "partial" {
			comparableRuns = append(comparableRuns, crossCheckRun{runID: start.Run.ID, variant: check.Variants[index]})
			if terminal.Status == "partial" {
				failures = append(failures, variant.Name+": "+terminal.Error)
			}
		} else {
			message := terminal.Error
			if message == "" {
				message = "识别运行状态 " + terminal.Status
			}
			failures = append(failures, variant.Name+": "+message)
		}
		done++
		_ = a.Store.UpdateJobProgress(dbCtx, job.ID, done, total, "已完成 "+variant.Name)
	}

	if len(comparableRuns) < 2 {
		message := "可比较的模型结果不足（至少需要两个成功的识别运行）"
		if len(failures) > 0 {
			message += ": " + summarizeErrors(failures)
		}
		for _, page := range check.Pages {
			page.Status = "failed"
			page.Error = "可比较的模型结果不足"
			_ = a.Store.UpdateCrossCheckPage(dbCtx, page)
		}
		_ = a.Store.FinishCrossCheck(dbCtx, check.ID, "failed", message)
		return CrossCheck{}, errors.New(message)
	}

	runIDs := make([]string, 0, len(comparableRuns))
	comparableNames := make([]string, 0, len(comparableRuns))
	variantNameByRun := make(map[string]string, len(comparableRuns))
	for _, run := range comparableRuns {
		runIDs = append(runIDs, run.runID)
		comparableNames = append(comparableNames, run.variant.Name)
		variantNameByRun[run.runID] = run.variant.Name
	}
	allResults, err := a.Store.RecognitionResultsForRuns(dbCtx, runIDs)
	if err != nil {
		_ = a.Store.FinishCrossCheck(dbCtx, check.ID, "failed", "加载识别结果失败: "+err.Error())
		return CrossCheck{}, err
	}
	runOrder := make(map[string]int, len(runIDs))
	for index, runID := range runIDs {
		runOrder[runID] = index
	}
	resultsByPage := make(map[string][]RecognitionResult)
	for _, result := range allResults {
		resultsByPage[result.PageID] = append(resultsByPage[result.PageID], result)
	}
	for pageID := range resultsByPage {
		entries := resultsByPage[pageID]
		sort.SliceStable(entries, func(i, j int) bool { return runOrder[entries[i].RunID] < runOrder[entries[j].RunID] })
		resultsByPage[pageID] = entries
	}

	for _, page := range check.Pages {
		if ctx.Err() != nil {
			return cancelOut()
		}
		outcome := a.crossCheckPageOutcome(ctx, check, page, resultsByPage[page.PageID], variantNameByRun, comparableNames)
		if err := a.Store.UpdateCrossCheckPage(dbCtx, outcome); err != nil {
			failures = append(failures, fmt.Sprintf("第 %d 页: 保存核验结果失败: %s", page.PageNo, err.Error()))
		} else if outcome.Status == "failed" {
			failures = append(failures, fmt.Sprintf("第 %d 页: %s", page.PageNo, outcome.Error))
		}
		if outcome.Status == "canceled" || ctx.Err() != nil {
			return cancelOut()
		}
		done++
		_ = a.Store.UpdateJobProgress(dbCtx, job.ID, done, total, fmt.Sprintf("已核验第 %d 页", page.PageNo))
	}

	status := "succeeded"
	message := ""
	if len(failures) > 0 {
		status = "partial"
		message = summarizeErrors(failures)
	}
	_ = a.Store.FinishCrossCheck(dbCtx, check.ID, status, message)
	refreshed, err := a.Store.GetCrossCheck(dbCtx, check.ID)
	if err != nil {
		return CrossCheck{}, err
	}
	if len(failures) > 0 {
		return refreshed, errors.New(message)
	}
	return refreshed, nil
}

func (a *App) recognizerForCrossCheckVariant(ctx context.Context, documentID string, variant CrossCheckVariant) (recognizer.Recognizer, RecognitionRun, error) {
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
		return nil, RecognitionRun{}, fmt.Errorf("decode cross check variant snapshot: %w", err)
	}
	run := RecognitionRun{
		ProfileID: variant.ProfileID, ProviderAdapterID: variant.ProviderAdapterID,
		Driver: snapshot.Driver, ProfileSnapshotJSON: variant.SnapshotJSON,
	}
	rec, err := a.recognizerFromRunSnapshot(ctx, run)
	return rec, run, err
}

// crossCheckPageOutcome compares one page's model outputs. Full-coverage
// verbatim agreement is consensus; anything less — divergent text, or a
// variant that produced no result for this page — goes to human review with a
// conservative merge draft (when possible) and an open uncertain-text
// annotation that surfaces the page in the review queue.
func (a *App) crossCheckPageOutcome(ctx context.Context, check CrossCheck, page CrossCheckPage, results []RecognitionResult, variantNameByRun map[string]string, comparableNames []string) CrossCheckPage {
	dbCtx := context.Background()
	page.ResultIDs = make([]string, 0, len(results))
	participating := make([]string, 0, len(results))
	for _, result := range results {
		page.ResultIDs = append(page.ResultIDs, result.ID)
		participating = append(participating, variantNameByRun[result.RunID])
	}
	page.Conflicts = []CrossCheckConflict{}
	if len(results) < 2 {
		page.Status = "failed"
		page.Error = fmt.Sprintf("可比较的模型结果不足（%d/%d 个模型产出了该页结果）", len(results), len(comparableNames))
		return page
	}
	if ctx.Err() != nil {
		page.Status = "canceled"
		page.Error = cancelCause(ctx)
		return page
	}

	missing := missingVariants(comparableNames, participating)
	normalized := make([]string, 0, len(results))
	for _, result := range results {
		normalized = append(normalized, strings.Join(normalizeComparisonLines(result.Text), "\n"))
	}
	allEqual := true
	for index := 1; index < len(normalized); index++ {
		if normalized[index] != normalized[0] {
			allEqual = false
			break
		}
	}
	agreement := crossCheckAgreement(normalized)
	// A page whose texts differ must never display as 100% — rounding on
	// near-identical long pages (and the 50k-rune edit-distance cap) would
	// otherwise show full agreement on a disagreement row.
	if !allEqual && agreement >= 1 {
		agreement = 0.999
	}
	page.Agreement = &agreement

	if allEqual && len(missing) == 0 {
		page.Status = "consensus"
		// Machine notes from a previous check are superseded by this outcome.
		if err := a.Store.ResolveCrossCheckAnnotations(dbCtx, page.PageID); err != nil {
			log.Printf("cross check %s: resolve stale annotations for page %s: %v", check.ID, page.PageID, err)
		}
		last := results[len(results)-1]
		if versionID, err := a.Store.TextVersionIDBySourceResult(dbCtx, last.ID); err == nil {
			page.ConsensusVersionID = versionID
		}
		return page
	}

	page.Status = "disagreement"
	coverageNote := ""
	if len(missing) > 0 {
		coverageNote = fmt.Sprintf("缺少 %s 的识别结果（其运行部分失败），无法构成全体一致", strings.Join(missing, "、"))
		page.Error = coverageNote
	}

	mergedText := ""
	var mergeErr error
	totalConflicts := 0
	if !allEqual {
		var merge CandidateMerge
		merge, mergeErr = a.MergeRecognitionCandidates(ctx, page.PageID, page.ResultIDs, check.MergeProfileID)
		if ctx.Err() != nil {
			page.Status = "canceled"
			page.Error = cancelCause(ctx)
			page.Conflicts = []CrossCheckConflict{}
			return page
		}
		if mergeErr == nil {
			page.MergedVersionID = merge.TextVersionID
			mergedText = merge.TextVersion.Text
		} else {
			page.Error = strings.TrimPrefix(coverageNote+"; ", "; ") + "自动合并失败: " + mergeErr.Error()
		}
		page.Conflicts, totalConflicts = crossCheckConflicts(mergedText, participating, results)
		if totalConflicts == 0 {
			// Same line sets but different order or repetition counts: still a
			// real divergence, so never leave the report empty.
			page.Conflicts = append(page.Conflicts, CrossCheckConflict{
				Kind: "divergent", Text: "各模型输出的行集合一致，但行顺序或重复次数不同", PresentIn: participating,
			})
			totalConflicts = 1
		}
	}

	if err := a.Store.ResolveCrossCheckAnnotations(dbCtx, page.PageID); err != nil {
		log.Printf("cross check %s: resolve stale annotations for page %s: %v", check.ID, page.PageID, err)
	}
	annotation, annErr := a.CreateAnnotation(dbCtx, Annotation{
		DocumentID:    check.DocumentID,
		PageID:        page.PageID,
		TextVersionID: page.MergedVersionID,
		Kind:          "uncertain_text",
		Status:        "open",
		Body:          crossCheckAnnotationBody(len(results), agreement, mergedText != "", mergeErr, missing, allEqual, page.Conflicts, totalConflicts),
		AnchorJSON:    "{}",
	})
	if annErr != nil {
		if page.Error != "" {
			page.Error += "; "
		}
		page.Error += "创建存疑批注失败: " + annErr.Error()
	} else {
		page.AnnotationID = annotation.ID
	}
	return page
}

// normalizeComparisonLines mirrors the line semantics of the conservative
// merge validator: lines are trimmed and blank lines are ignored, so pure
// whitespace differences never count as model disagreement.
func normalizeComparisonLines(text string) []string {
	lines := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// crossCheckAgreement is the worst pairwise similarity across the normalized
// outputs: 1 means every model produced identical text. Inputs beyond the
// edit-distance rune cap are compared on their prefix; callers clamp the
// value below 1 whenever the texts are known to differ.
func crossCheckAgreement(normalized []string) float64 {
	agreement := 1.0
	for i := 0; i < len(normalized); i++ {
		for j := i + 1; j < len(normalized); j++ {
			similarity := textSimilarity(normalized[i], normalized[j])
			if similarity < agreement {
				agreement = similarity
			}
		}
	}
	return math.Round(agreement*1000) / 1000
}

func textSimilarity(left, right string) float64 {
	leftLen := len([]rune(left))
	rightLen := len([]rune(right))
	longest := leftLen
	if rightLen > longest {
		longest = rightLen
	}
	if longest == 0 {
		return 1
	}
	distance := runeEditDistance(left, right)
	similarity := 1 - float64(distance)/float64(longest)
	if similarity < 0 {
		return 0
	}
	return similarity
}

// crossCheckConflicts lists line-level divergences, in first-appearance order:
// lines the merge had to drop ("omitted"), merge picks that not every model
// produced ("partial"), and — when no merge exists — non-unanimous lines
// ("divergent"). It returns the stored (capped) list plus the true total so
// reports can say how much was truncated.
func crossCheckConflicts(mergedText string, variantNames []string, results []RecognitionResult) ([]CrossCheckConflict, int) {
	type lineInfo struct {
		presentIn []string
	}
	order := []string{}
	byLine := map[string]*lineInfo{}
	seenPerSource := make([]map[string]bool, len(results))
	for index, result := range results {
		seenPerSource[index] = map[string]bool{}
		for _, line := range normalizeComparisonLines(result.Text) {
			if seenPerSource[index][line] {
				continue
			}
			seenPerSource[index][line] = true
			info, ok := byLine[line]
			if !ok {
				info = &lineInfo{}
				byLine[line] = info
				order = append(order, line)
			}
			info.presentIn = append(info.presentIn, variantNames[index])
		}
	}
	mergeOK := strings.TrimSpace(mergedText) != ""
	outputSet := map[string]bool{}
	for _, line := range normalizeComparisonLines(mergedText) {
		outputSet[line] = true
	}

	conflicts := []CrossCheckConflict{}
	total := 0
	appendConflict := func(conflict CrossCheckConflict) {
		total++
		if len(conflicts) < maxCrossCheckConflicts {
			conflicts = append(conflicts, conflict)
		}
	}
	for _, line := range order {
		info := byLine[line]
		unanimous := len(info.presentIn) == len(results)
		switch {
		case mergeOK && !outputSet[line]:
			appendConflict(CrossCheckConflict{
				Text: boundedRunes(line, maxCrossCheckConflictRunes), Kind: "omitted", PresentIn: info.presentIn,
			})
		case mergeOK && !unanimous:
			appendConflict(CrossCheckConflict{
				Text: boundedRunes(line, maxCrossCheckConflictRunes), Kind: "partial",
				PresentIn: info.presentIn, AbsentFrom: missingVariants(variantNames, info.presentIn),
			})
		case !mergeOK && !unanimous:
			appendConflict(CrossCheckConflict{
				Text: boundedRunes(line, maxCrossCheckConflictRunes), Kind: "divergent",
				PresentIn: info.presentIn, AbsentFrom: missingVariants(variantNames, info.presentIn),
			})
		}
	}
	return conflicts, total
}

func missingVariants(all, present []string) []string {
	presentSet := make(map[string]bool, len(present))
	for _, name := range present {
		presentSet[name] = true
	}
	missing := []string{}
	for _, name := range all {
		if !presentSet[name] {
			missing = append(missing, name)
		}
	}
	return missing
}

func crossCheckAnnotationBody(modelCount int, agreement float64, merged bool, mergeErr error, missing []string, allEqual bool, conflicts []CrossCheckConflict, totalConflicts int) string {
	var body strings.Builder
	fmt.Fprintf(&body, "%s %d 个模型的识别结果存在分歧（一致度 %.0f%%）。\n", crossCheckAnnotationPrefix, modelCount, agreement*100)
	if len(missing) > 0 {
		fmt.Fprintf(&body, "注意：缺少 %s 的识别结果（其运行部分失败）。\n", strings.Join(missing, "、"))
	}
	switch {
	case allEqual:
		body.WriteString("已产出结果的模型输出完全一致，但覆盖不完整，请人工确认后定稿。\n")
	case merged:
		body.WriteString("已生成保守合并稿（无法判定的行被省略），请人工核对以下分歧后拍板：\n")
	default:
		message := "未知错误"
		if mergeErr != nil {
			message = mergeErr.Error()
		}
		fmt.Fprintf(&body, "自动合并失败（%s），请对照各模型识别结果人工定稿：\n", boundedRunes(message, 200))
	}
	shown := 0
	for _, conflict := range conflicts {
		if shown >= maxCrossCheckAnnotationLines {
			break
		}
		line := boundedRunes(conflict.Text, 80)
		switch conflict.Kind {
		case "omitted":
			fmt.Fprintf(&body, "· 「%s」— 仅见于 %s，合并稿未收录\n", line, strings.Join(conflict.PresentIn, "、"))
		case "partial":
			fmt.Fprintf(&body, "· 「%s」— %s 缺失，合并稿已采用\n", line, strings.Join(conflict.AbsentFrom, "、"))
		default:
			fmt.Fprintf(&body, "· 「%s」— 见于 %s，%s 无此行\n", line, strings.Join(conflict.PresentIn, "、"), strings.Join(conflict.AbsentFrom, "、"))
		}
		shown++
	}
	if remaining := totalConflicts - shown; remaining > 0 {
		fmt.Fprintf(&body, "……另有 %d 处分歧，详见交叉核验报告。\n", remaining)
	}
	if totalConflicts > maxCrossCheckConflicts {
		fmt.Fprintf(&body, "（分歧超过 %d 处，报告中的分歧列表已截断。）\n", maxCrossCheckConflicts)
	}
	return strings.TrimSpace(body.String())
}

// AdoptCrossCheckConsensus is the user's sign-off: consensus pages (all models
// agreed verbatim) become verified finals. Pages a human already edited are
// skipped — adoption never overwrites human work — and pages with a newer
// cross-check outcome must be decided from that newer check. Disagreement
// pages must be finalized through the review page instead.
func (a *App) AdoptCrossCheckConsensus(ctx context.Context, checkID string, pageIDs []string) (CrossCheckAdoption, error) {
	check, err := a.Store.GetCrossCheck(ctx, checkID)
	if err != nil {
		return CrossCheckAdoption{}, err
	}
	byPage := make(map[string]CrossCheckPage, len(check.Pages))
	for _, page := range check.Pages {
		byPage[page.PageID] = page
	}
	explicit := len(pageIDs) > 0
	targets := make([]CrossCheckPage, 0, len(check.Pages))
	if explicit {
		seen := map[string]bool{}
		for _, pageID := range pageIDs {
			pageID = strings.TrimSpace(pageID)
			page, ok := byPage[pageID]
			if pageID == "" || !ok {
				return CrossCheckAdoption{}, fmt.Errorf("page %q does not belong to cross check", pageID)
			}
			if seen[pageID] {
				continue
			}
			seen[pageID] = true
			targets = append(targets, page)
		}
	} else {
		targets = check.Pages
	}

	adoption := CrossCheckAdoption{AdoptedPageIDs: []string{}, Skipped: []CrossCheckAdoptionSkip{}}
	skip := func(page CrossCheckPage, reason string) {
		adoption.Skipped = append(adoption.Skipped, CrossCheckAdoptionSkip{PageID: page.PageID, PageNo: page.PageNo, Reason: reason})
	}
	for _, page := range targets {
		if page.Status != "consensus" {
			if explicit {
				skip(page, "仅全体一致的页可以直接采纳；分歧页请在审校页人工定稿")
			}
			continue
		}
		if page.AdoptedVersionID != "" {
			skip(page, "该页已采纳过")
			continue
		}
		if latestCheck, _, latestErr := a.Store.LatestCrossCheckForPage(ctx, page.PageID); latestErr == nil && latestCheck.ID != checkID {
			skip(page, "该页存在更新的交叉核验结果，请以最新核验为准")
			continue
		}
		text, sourceResultID, textErr := a.crossCheckConsensusText(ctx, page)
		if textErr != nil {
			skip(page, "读取一致文本失败: "+textErr.Error())
			continue
		}
		version := TextVersion{
			ID: newID("txt"), DocumentID: check.DocumentID, PageID: page.PageID, Kind: "final",
			BaseVersionID: page.ConsensusVersionID, SourceResultID: sourceResultID,
			Text: text, Status: "verified", CreatedBy: "cross-check", CreatedAt: now(),
		}
		// Guard + insert + claim are one transaction: concurrent adoptions and
		// concurrent human edits are serialized by the store.
		if err := a.Store.AdoptCrossCheckPage(ctx, checkID, version); err != nil {
			switch {
			case errors.Is(err, ErrCrossCheckHumanVersion):
				skip(page, "该页已有人工版本，请在审校页确认")
			case errors.Is(err, ErrCrossCheckPageDecided):
				skip(page, "该页已采纳过或状态已变化")
			default:
				skip(page, "保存定稿失败: "+err.Error())
			}
			continue
		}
		// Post-adoption bookkeeping, mirroring SaveTextVersion for kind=final.
		if err := a.Store.RecordAuthorCorrection(ctx, version); err != nil {
			log.Printf("record author correction for text version %s: %v", version.ID, err)
		}
		_ = a.Store.UpdatePageStatus(ctx, page.PageID, "verified")
		if err := a.Store.ResolveCrossCheckAnnotations(ctx, page.PageID); err != nil {
			log.Printf("resolve cross-check annotations for page %s: %v", page.PageID, err)
		}
		_ = a.Store.RecomputeDocumentStatus(ctx, check.DocumentID)
		adoption.AdoptedPageIDs = append(adoption.AdoptedPageIDs, page.PageID)
	}
	adoption.CrossCheck, err = a.Store.GetCrossCheck(ctx, checkID)
	if err != nil {
		return CrossCheckAdoption{}, err
	}
	return adoption, nil
}

func (a *App) crossCheckConsensusText(ctx context.Context, page CrossCheckPage) (string, string, error) {
	if page.ConsensusVersionID != "" {
		if version, err := a.Store.GetTextVersion(ctx, page.ConsensusVersionID); err == nil {
			return version.Text, version.SourceResultID, nil
		}
	}
	if len(page.ResultIDs) == 0 {
		return "", "", errors.New("no recognition results recorded for this page")
	}
	lastID := page.ResultIDs[len(page.ResultIDs)-1]
	results, err := a.Store.ListRecognitionResults(ctx, page.PageID)
	if err != nil {
		return "", "", err
	}
	for _, result := range results {
		if result.ID == lastID {
			return result.Text, result.ID, nil
		}
	}
	return "", "", errors.New("recognition result is no longer available")
}
