package api_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lieyan/firescribe/internal/api"
	"github.com/lieyan/firescribe/internal/app"
	"github.com/lieyan/firescribe/internal/db"
	"github.com/lieyan/firescribe/internal/recognizer"
	"github.com/lieyan/firescribe/internal/storage"
)

func TestProjectsAPIAndMergedExport(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open(filepath.Join(t.TempDir(), "firescribe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := db.Migrate(conn); err != nil {
		t.Fatal(err)
	}
	files, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	application := app.New(app.NewStore(conn), files, recognizer.MockRecognizer{})
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		application.Shutdown(shutdownCtx)
	})
	handler := authedHandler(t, api.New(application, "", nil).Routes())

	docA := importProjectTestDocument(t, application, "甲文", "甲文正文")
	docB := importProjectTestDocument(t, application, "乙文", "乙文正文")

	create := projectAPIRequest(t, handler, http.MethodPost, "/api/projects", map[string]any{
		"name": "手稿合集", "description": "两篇文章",
	})
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", create.Code, create.Body.String())
	}
	var project app.Project
	decodeProjectAPI(t, create, &project)
	if project.ID == "" || project.Name != "手稿合集" {
		t.Fatalf("unexpected project: %+v", project)
	}
	list := projectAPIRequest(t, handler, http.MethodGet, "/api/projects", nil)
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", list.Code, list.Body.String())
	}
	var projects []app.Project
	decodeProjectAPI(t, list, &projects)
	if len(projects) != 1 || projects[0].ID != project.ID {
		t.Fatalf("unexpected projects: %+v", projects)
	}
	patch := projectAPIRequest(t, handler, http.MethodPatch, "/api/projects/"+project.ID, map[string]any{
		"name": "手稿总集", "description": "更新后的说明",
	})
	if patch.Code != http.StatusOK {
		t.Fatalf("patch status = %d, body = %s", patch.Code, patch.Body.String())
	}
	decodeProjectAPI(t, patch, &project)
	if project.Name != "手稿总集" || project.Description != "更新后的说明" {
		t.Fatalf("unexpected patched project: %+v", project)
	}

	addA := projectAPIRequest(t, handler, http.MethodPost, "/api/projects/"+project.ID+"/documents", map[string]any{"document_id": docA.ID})
	if addA.Code != http.StatusCreated {
		t.Fatalf("add A status = %d, body = %s", addA.Code, addA.Body.String())
	}
	addB := projectAPIRequest(t, handler, http.MethodPost, "/api/projects/"+project.ID+"/documents", map[string]any{"document_id": docB.ID, "position": 0})
	if addB.Code != http.StatusCreated {
		t.Fatalf("add B status = %d, body = %s", addB.Code, addB.Body.String())
	}
	var inserted []app.ProjectDocument
	decodeProjectAPI(t, addB, &inserted)
	if len(inserted) != 2 || inserted[0].ID != docB.ID || inserted[1].ID != docA.ID {
		t.Fatalf("unexpected insertion order: %+v", inserted)
	}

	reorder := projectAPIRequest(t, handler, http.MethodPut, "/api/projects/"+project.ID+"/documents/order", map[string]any{
		"document_ids": []string{docA.ID, docB.ID},
	})
	if reorder.Code != http.StatusOK {
		t.Fatalf("reorder status = %d, body = %s", reorder.Code, reorder.Body.String())
	}

	detailResponse := projectAPIRequest(t, handler, http.MethodGet, "/api/projects/"+project.ID, nil)
	if detailResponse.Code != http.StatusOK {
		t.Fatalf("detail status = %d, body = %s", detailResponse.Code, detailResponse.Body.String())
	}
	var detail app.ProjectDetail
	decodeProjectAPI(t, detailResponse, &detail)
	if detail.DocumentCount != 2 || detail.PageCount != 2 || len(detail.Documents) != 2 || detail.Documents[0].ID != docA.ID {
		t.Fatalf("unexpected detail: %+v", detail)
	}
	pagesA, err := application.Store.ListPages(ctx, docA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.CreateAnnotation(ctx, app.Annotation{
		DocumentID: docA.ID, PageID: pagesA[0].ID, Kind: "page_note", Status: "open", Body: "重点批注",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.CreateAnnotation(ctx, app.Annotation{
		DocumentID: docA.ID, PageID: pagesA[0].ID, Kind: "uncertain_text", Status: "open", Body: "待核",
		AnchorJSON: `{"type":"text_range","start":0,"end":1,"text":"甲"}`,
	}); err != nil {
		t.Fatal(err)
	}

	exportResponse := projectAPIRequest(t, handler, http.MethodPost, "/api/projects/"+project.ID+"/exports", map[string]any{
		"format": "md", "include_page_numbers": true, "text_scope": "final",
		"include_annotations": true, "include_uncertain": true,
	})
	if exportResponse.Code != http.StatusAccepted {
		t.Fatalf("export status = %d, body = %s", exportResponse.Code, exportResponse.Body.String())
	}
	var started app.ProjectExportStart
	decodeProjectAPI(t, exportResponse, &started)
	waitProjectExportJob(t, application, started.Job.ID)

	exportStatus := projectAPIRequest(t, handler, http.MethodGet, "/api/project-exports/"+started.ID, nil)
	if exportStatus.Code != http.StatusOK {
		t.Fatalf("export get status = %d, body = %s", exportStatus.Code, exportStatus.Body.String())
	}
	var exported app.ProjectExport
	decodeProjectAPI(t, exportStatus, &exported)
	if exported.Status != "succeeded" || exported.AssetID == "" || exported.DownloadURL == "" ||
		exported.TextScope != "final" || !exported.IncludeAnnotations || !exported.IncludeUncertain {
		t.Fatalf("unexpected exported record: %+v", exported)
	}

	download := projectAPIRequest(t, handler, http.MethodGet, exported.DownloadURL, nil)
	if download.Code != http.StatusOK {
		t.Fatalf("download status = %d, body = %s", download.Code, download.Body.String())
	}
	content := download.Body.String()
	if !strings.Contains(content, "# 手稿总集") || !strings.Contains(content, "**《甲文》**") || !strings.Contains(content, "**《乙文》**") ||
		!strings.Contains(content, "甲〔存疑：待核〕文正文") || !strings.Contains(content, "乙文正文") ||
		!strings.Contains(content, "## 第 1 页") || !strings.Contains(content, "重点批注") {
		t.Fatalf("unexpected merged export:\n%s", content)
	}
	if strings.Index(content, "**《甲文》**") > strings.Index(content, "**《乙文》**") {
		t.Fatalf("document order was not preserved:\n%s", content)
	}

	for _, binaryFormat := range []string{"docx", "pdf"} {
		t.Run("merged_"+binaryFormat, func(t *testing.T) {
			response := projectAPIRequest(t, handler, http.MethodPost, "/api/projects/"+project.ID+"/exports", map[string]any{
				"format": binaryFormat, "text_scope": "final", "include_annotations": true,
			})
			if response.Code != http.StatusAccepted {
				t.Fatalf("start status = %d, body = %s", response.Code, response.Body.String())
			}
			var binaryStart app.ProjectExportStart
			decodeProjectAPI(t, response, &binaryStart)
			waitProjectExportJob(t, application, binaryStart.Job.ID)
			binaryExport, err := application.Store.GetProjectExport(ctx, binaryStart.ID)
			if err != nil {
				t.Fatal(err)
			}
			binaryDownload := projectAPIRequest(t, handler, http.MethodGet, binaryExport.DownloadURL, nil)
			if binaryDownload.Code != http.StatusOK {
				t.Fatalf("download %s status = %d", binaryFormat, binaryDownload.Code)
			}
			raw := binaryDownload.Body.Bytes()
			if binaryFormat == "docx" {
				archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
				if err != nil {
					t.Fatalf("invalid merged DOCX zip: %v", err)
				}
				var documentXML string
				for _, file := range archive.File {
					if file.Name == "word/document.xml" {
						reader, err := file.Open()
						if err != nil {
							t.Fatal(err)
						}
						content, err := io.ReadAll(reader)
						reader.Close()
						if err != nil {
							t.Fatal(err)
						}
						documentXML = string(content)
					}
				}
				if !strings.Contains(documentXML, "甲文") || !strings.Contains(documentXML, "乙文") {
					t.Fatalf("merged DOCX does not contain both document sections")
				}
			} else if bytes.Count(raw, []byte("%PDF-")) != 1 || !bytes.Contains(raw, []byte("%%EOF")) {
				t.Fatalf("project PDF is not one complete PDF document")
			}
		})
	}

	remove := projectAPIRequest(t, handler, http.MethodDelete, "/api/projects/"+project.ID+"/documents/"+docA.ID, nil)
	if remove.Code != http.StatusOK {
		t.Fatalf("remove status = %d, body = %s", remove.Code, remove.Body.String())
	}
	deleteResponse := projectAPIRequest(t, handler, http.MethodDelete, "/api/projects/"+project.ID, nil)
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body = %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	if _, err := application.Store.GetDocument(ctx, docB.ID); err != nil {
		t.Fatalf("project deletion removed source document: %v", err)
	}
}

func importProjectTestDocument(t *testing.T, application *app.App, title, text string) app.Document {
	t.Helper()
	document, err := application.ImportDocument(context.Background(), app.ImportOptions{Title: title}, app.ImportFile{
		Name: title + ".png", Reader: bytes.NewReader(apiTestPNG(t)),
	})
	if err != nil {
		t.Fatal(err)
	}
	pages, err := application.Store.ListPages(context.Background(), document.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.SaveTextVersion(context.Background(), app.TextVersion{
		DocumentID: document.ID, PageID: pages[0].ID, Kind: "final", Status: "verified", Text: text,
	}); err != nil {
		t.Fatal(err)
	}
	return document
}

func projectAPIRequest(t *testing.T, handler http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func decodeProjectAPI(t *testing.T, response *httptest.ResponseRecorder, value any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), value); err != nil {
		t.Fatalf("decode response %q: %v", response.Body.String(), err)
	}
}

func waitProjectExportJob(t *testing.T, application *app.App, jobID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, err := application.Store.GetJob(context.Background(), jobID)
		if err != nil {
			t.Fatal(err)
		}
		switch job.Status {
		case "succeeded":
			return
		case "failed", "canceled":
			t.Fatalf("project export job ended as %s: %s", job.Status, job.LastError)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("project export job did not finish")
}
