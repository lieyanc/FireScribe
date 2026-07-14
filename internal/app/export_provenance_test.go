package app_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/lieyan/firescribe/internal/app"
	"github.com/lieyan/firescribe/internal/recognizer"
)

func TestExportProvenanceKeepsExactVersionsAndAnnotations(t *testing.T) {
	ctx := context.Background()
	application, _ := newTestApp(t, recognizer.MockRecognizer{})
	document, err := application.ImportDocument(ctx, app.ImportOptions{Title: "快照文档"},
		app.ImportFile{Name: "page.png", Reader: bytes.NewReader(distinctPNG(t, 121))})
	if err != nil {
		t.Fatal(err)
	}
	page := mustFirstPage(t, application, document.ID)
	version, err := application.SaveTextVersion(ctx, app.TextVersion{
		DocumentID: document.ID, PageID: page.ID, Kind: "final", Status: "verified", Text: "手稿正文",
	})
	if err != nil {
		t.Fatal(err)
	}
	annotation, err := application.CreateAnnotation(ctx, app.Annotation{
		DocumentID: document.ID, PageID: page.ID, TextVersionID: version.ID,
		Kind: "page_region", Body: "边角字迹", AnchorJSON: `{"type":"page_region","x":10,"y":20,"width":30,"height":40}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	started, err := application.StartExportWithOptions(ctx, document.ID, app.ExportOptions{
		Format: "md", TextScope: "final", IncludeAnnotations: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForJob(t, application, started.Job.ID)
	snapshots, err := application.Store.ListExportPageSnapshots(ctx, started.ID)
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("snapshots=%+v err=%v", snapshots, err)
	}
	if snapshots[0].TextVersionID != version.ID || snapshots[0].TextVersionKind != "final" ||
		len(snapshots[0].Annotations) != 1 || snapshots[0].Annotations[0].ID != annotation.ID || snapshots[0].Annotations[0].RenderedAs != "note" {
		t.Fatalf("snapshot=%+v", snapshots[0])
	}
	if _, err := application.SaveTextVersion(ctx, app.TextVersion{
		DocumentID: document.ID, PageID: page.ID, Kind: "manual", Status: "draft", Text: "后续修改",
	}); err != nil {
		t.Fatal(err)
	}
	again, err := application.Store.ListExportPageSnapshots(ctx, started.ID)
	if err != nil || again[0].TextVersionID != version.ID {
		t.Fatalf("snapshot changed after later edit: %+v err=%v", again, err)
	}
	history, err := application.Store.ListDocumentExports(ctx, document.ID)
	if err != nil || len(history) != 1 || history[0].ID != started.ID || !strings.Contains(history[0].DownloadURL, started.ID) {
		t.Fatalf("history=%+v err=%v", history, err)
	}

	project, err := application.CreateProject(ctx, "快照合集", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Store.AddProjectDocument(ctx, project.ID, document.ID, nil); err != nil {
		t.Fatal(err)
	}
	projectStart, err := application.StartProjectExportWithOptions(ctx, project.ID, app.ExportOptions{Format: "txt", TextScope: "current"})
	if err != nil {
		t.Fatal(err)
	}
	waitForJob(t, application, projectStart.Job.ID)
	projectSnapshots, err := application.Store.ListProjectExportPageSnapshots(ctx, projectStart.ID)
	if err != nil || len(projectSnapshots) != 1 || projectSnapshots[0].DocumentID != document.ID || projectSnapshots[0].DocumentPosition != 0 {
		t.Fatalf("project snapshots=%+v err=%v", projectSnapshots, err)
	}
	projectHistory, err := application.Store.ListProjectExports(ctx, project.ID)
	if err != nil || len(projectHistory) != 1 || projectHistory[0].ID != projectStart.ID {
		t.Fatalf("project history=%+v err=%v", projectHistory, err)
	}
}
