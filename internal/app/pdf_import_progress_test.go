package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lieyan/firescribe/internal/app"
	"github.com/lieyan/firescribe/internal/db"
	"github.com/lieyan/firescribe/internal/recognizer"
	"github.com/lieyan/firescribe/internal/storage"
)

func TestPDFImportProgressDoesNotRegressWhileRegisteringStagedPages(t *testing.T) {
	for _, command := range []string{"pdfinfo", "pdfimages", "pdftoppm"} {
		if _, err := exec.LookPath(command); err != nil {
			t.Skipf("%s not installed", command)
		}
	}

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

	started, err := application.StartImport(ctx, app.ImportOptions{Title: "PDF 进度"}, app.ImportFile{
		Name: "pages.pdf", Reader: bytes.NewReader(minimalPDF(3)),
	})
	if err != nil {
		t.Fatal(err)
	}
	job := waitForTerminalJob(t, application, started.Job.ID)
	if job.Status != "succeeded" {
		t.Fatalf("import job = %+v", job)
	}

	events, err := application.Store.ListJobEvents(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	last := 0
	sawExtractionComplete := false
	sawRegistration := false
	for _, event := range events {
		if event.Stage != "progress" {
			continue
		}
		var progress struct {
			Current int `json:"current"`
		}
		if err := json.Unmarshal([]byte(event.DataJSON), &progress); err != nil {
			t.Fatalf("decode progress event %+v: %v", event, err)
		}
		if progress.Current < last {
			t.Fatalf("progress regressed from %d to %d at %q", last, progress.Current, event.Message)
		}
		last = progress.Current
		if progress.Current == 3 && strings.Contains(event.Message, "已光栅化 PDF") {
			sawExtractionComplete = true
		}
		if strings.Contains(event.Message, "已登记 PDF") {
			sawRegistration = true
			if progress.Current != 3 {
				t.Fatalf("staged page registration reported %d after extraction reached 3", progress.Current)
			}
		}
	}
	if !sawExtractionComplete || !sawRegistration {
		t.Fatalf("missing extraction/registration progress events: %+v", events)
	}
}

// minimalPDF builds a valid image-free PDF so import exercises the safe
// direct-extraction-to-raster fallback before staged pages are registered.
func minimalPDF(pages int) []byte {
	var buf bytes.Buffer
	offsets := make([]int, 0, pages+2)
	writeObject := func(value string) {
		offsets = append(offsets, buf.Len())
		buf.WriteString(value)
	}
	buf.WriteString("%PDF-1.4\n")
	kids := make([]string, pages)
	for index := range kids {
		kids[index] = fmt.Sprintf("%d 0 R", index+3)
	}
	writeObject("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	writeObject(fmt.Sprintf("2 0 obj\n<< /Type /Pages /Kids [%s] /Count %d >>\nendobj\n", strings.Join(kids, " "), pages))
	for index := 0; index < pages; index++ {
		writeObject(fmt.Sprintf("%d 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 144 72] >>\nendobj\n", index+3))
	}
	xrefStart := buf.Len()
	objectCount := pages + 2
	fmt.Fprintf(&buf, "xref\n0 %d\n", objectCount+1)
	buf.WriteString("0000000000 65535 f \n")
	for _, offset := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", objectCount+1, xrefStart)
	return buf.Bytes()
}
