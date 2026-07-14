package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/lieyan/firescribe/internal/app"
	"github.com/lieyan/firescribe/internal/recognizer"
)

func TestAlignedCandidateMergePersistsUTF16SegmentLineage(t *testing.T) {
	ctx := context.Background()
	application, _ := newTestApp(t, recognizer.MockRecognizer{})
	doc, err := application.ImportDocument(ctx, app.ImportOptions{Title: "逐段对齐"},
		app.ImportFile{Name: "page.png", Reader: bytes.NewReader(distinctPNG(t, 88))})
	if err != nil {
		t.Fatal(err)
	}
	page := mustFirstPage(t, application, doc.ID)
	for range 2 {
		start, err := application.StartRecognition(ctx, doc.ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		waitForRun(t, application, start.Run.ID)
	}
	results, err := application.Store.ListRecognitionResults(ctx, page.ID)
	if err != nil || len(results) != 2 {
		t.Fatalf("results=%+v err=%v", results, err)
	}
	units := utf16.Encode([]rune(results[0].Text))
	cut := len(units)
	if cut > 4 {
		cut = 4
	}
	selected := string(utf16.Decode(units[:cut]))
	merged, err := application.MergeAlignedCandidates(ctx, page.ID, []app.AlignedCandidateSegmentInput{{
		SourceResultID: results[0].ID, SourceStart: 0, SourceEnd: cut, Text: selected,
	}})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := application.Store.GetCandidateMergeByTextVersion(ctx, merged.TextVersionID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Driver != "manual-alignment" || stored.TextVersion.Text != selected || len(stored.Sources) != 1 || len(stored.Segments) != 1 {
		t.Fatalf("stored aligned merge = %+v", stored)
	}
	segment := stored.Segments[0]
	if segment.SourceStart != 0 || segment.SourceEnd != cut || segment.OutputStart != 0 || segment.OutputEnd != cut || segment.Text != selected {
		t.Fatalf("segment lineage = %+v", segment)
	}
	_, err = application.MergeAlignedCandidates(ctx, page.ID, []app.AlignedCandidateSegmentInput{{
		SourceResultID: results[0].ID, SourceStart: 0, SourceEnd: cut, Text: selected + "扩写",
	}})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("mismatched source range error = %v", err)
	}
}

func TestTextRegionLinkAnnotationRemainsJSONCompatible(t *testing.T) {
	ctx := context.Background()
	application, _ := newTestApp(t, recognizer.MockRecognizer{})
	doc, err := application.ImportDocument(ctx, app.ImportOptions{Title: "双锚点"},
		app.ImportFile{Name: "page.png", Reader: bytes.NewReader(distinctPNG(t, 89))})
	if err != nil {
		t.Fatal(err)
	}
	page := mustFirstPage(t, application, doc.ID)
	anchor := `{"type":"text_region_link","start":0,"end":2,"text":"手稿","region":{"x":10,"y":20,"width":30,"height":40}}`
	created, err := application.CreateAnnotation(ctx, app.Annotation{DocumentID: doc.ID, PageID: page.ID, Kind: "page_region", Body: "联动", AnchorJSON: anchor})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := application.Store.GetAnnotation(ctx, created.ID)
	if err != nil || stored.AnchorJSON != anchor {
		t.Fatalf("stored annotation=%+v err=%v", stored, err)
	}
}

func TestRecognizerProfilePromptOverrideAndConservativeMergeAudit(t *testing.T) {
	ctx := context.Background()
	application, _ := newTestApp(t, recognizer.MockRecognizer{})
	doc, err := application.ImportDocument(ctx, app.ImportOptions{Title: "插件识别"},
		app.ImportFile{Name: "page.png", Reader: bytes.NewReader(distinctPNG(t, 44))})
	if err != nil {
		t.Fatal(err)
	}
	page := mustFirstPage(t, application, doc.ID)
	tooMany := make([]string, 9)
	for index := range tooMany {
		tooMany[index] = fmt.Sprintf("result-%d", index)
	}
	if _, err := application.MergeRecognitionCandidates(ctx, page.ID, tooMany, ""); err == nil || !strings.Contains(err.Error(), "at most eight") {
		t.Fatalf("candidate limit error = %v", err)
	}
	active, err := application.Store.EnsureActivePromptVersion(ctx, "transcribe-a", "仅转录可见文字 A", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	override, err := application.Store.CreatePromptVersion(ctx, "transcribe-b", "仅转录可见文字 B", strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	profile, err := application.SaveRecognizerProfile(ctx, app.RecognizerProfile{
		Name: "Mock A/B", Driver: recognizer.DriverMock, APIKey: "super-secret", ParamsJSON: `{}`,
		PromptVersionID: active.ID, IsDefault: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	serialized, _ := json.Marshal(profile)
	if strings.Contains(string(serialized), "super-secret") || strings.Contains(string(serialized), `"api_key"`) {
		t.Fatalf("profile response leaked API key: %s", serialized)
	}

	first, err := application.StartRecognitionWithOptions(ctx, doc.ID, app.RecognitionOptions{
		ProfileID: profile.ID, PromptVersionID: override.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	firstRun := waitForRun(t, application, first.Run.ID)
	if firstRun.ProfileID != profile.ID || firstRun.Driver != recognizer.DriverMock {
		t.Fatalf("run profile audit = %+v", firstRun)
	}
	if !strings.HasPrefix(firstRun.PromptVersion, "transcribe-b#") {
		t.Fatalf("run prompt = %q, want override B", firstRun.PromptVersion)
	}
	if strings.Contains(firstRun.ProfileSnapshotJSON, "super-secret") || !strings.Contains(firstRun.ProfileSnapshotJSON, override.ID) {
		t.Fatalf("unsafe/incomplete profile snapshot: %s", firstRun.ProfileSnapshotJSON)
	}

	second, err := application.StartRecognitionWithOptions(ctx, doc.ID, app.RecognitionOptions{ProfileID: profile.ID})
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, application, second.Run.ID)
	results, err := application.Store.ListRecognitionResults(ctx, page.ID)
	if err != nil || len(results) != 2 {
		t.Fatalf("results = %+v, err=%v", results, err)
	}
	merged, err := application.MergeRecognitionCandidates(ctx, page.ID, []string{results[0].ID, results[1].ID}, profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if merged.TextVersion.Kind != "candidate" || merged.PromptVersion != recognizer.ConservativeMergePromptVersion || len(merged.SourceResultIDs) != 2 {
		t.Fatalf("merge audit = %+v", merged)
	}
	stored, err := application.Store.GetCandidateMergeByTextVersion(ctx, merged.TextVersionID)
	if err != nil || len(stored.SourceResultIDs) != 2 || stored.PromptHash != recognizer.MergePromptHash() || len(stored.Segments) == 0 {
		t.Fatalf("stored merge = %+v, err=%v", stored, err)
	}
	versions, _ := application.Store.ListTextVersions(ctx, page.ID)
	found := false
	for _, version := range versions {
		found = found || version.ID == merged.TextVersionID
	}
	if !found {
		t.Fatal("merged candidate text version was not saved")
	}
}

type expandingMerger struct{ recognizer.MockRecognizer }

func (expandingMerger) MergeCandidates(context.Context, recognizer.CandidateMergeInput) (recognizer.CandidateMergeResult, error) {
	return recognizer.CandidateMergeResult{Text: "这是来源中不存在的扩写", RawResponse: []byte(`{"text":"expanded"}`)}, nil
}

func TestConservativeMergeRejectsExpansionWithoutCreatingVersion(t *testing.T) {
	ctx := context.Background()
	application, _ := newTestApp(t, expandingMerger{})
	doc, err := application.ImportDocument(ctx, app.ImportOptions{Title: "拒绝扩写"},
		app.ImportFile{Name: "page.png", Reader: bytes.NewReader(distinctPNG(t, 77))})
	if err != nil {
		t.Fatal(err)
	}
	page := mustFirstPage(t, application, doc.ID)
	for range 2 {
		start, err := application.StartRecognition(ctx, doc.ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		waitForRun(t, application, start.Run.ID)
	}
	results, _ := application.Store.ListRecognitionResults(ctx, page.ID)
	before, _ := application.Store.ListTextVersions(ctx, page.ID)
	_, err = application.MergeRecognitionCandidates(ctx, page.ID, []string{results[0].ID, results[1].ID}, "")
	if err == nil || !strings.Contains(err.Error(), "not present verbatim") {
		t.Fatalf("merge error = %v", err)
	}
	after, _ := application.Store.ListTextVersions(ctx, page.ID)
	if len(after) != len(before) {
		t.Fatalf("failed merge created a version: before=%d after=%d", len(before), len(after))
	}
}

func TestRecognizerProfileRejectsCredentialParams(t *testing.T) {
	ctx := context.Background()
	application, _ := newTestApp(t, recognizer.MockRecognizer{})
	_, err := application.SaveRecognizerProfile(ctx, app.RecognizerProfile{
		Name: "unsafe params", Driver: recognizer.DriverOpenAICompatible,
		BaseURL: "https://api.example.com/v1", APIKey: "secret", Model: "model",
		ParamsJSON: `{"max_tokens":128,"api_key":"must-not-enter-db"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported field") {
		t.Fatalf("error = %v", err)
	}
}

func mustFirstPage(t *testing.T, application *app.App, documentID string) app.Page {
	t.Helper()
	pages, err := application.Store.ListPages(context.Background(), documentID)
	if err != nil || len(pages) != 1 {
		t.Fatalf("pages = %+v, err=%v", pages, err)
	}
	return pages[0]
}
