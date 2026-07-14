package app_test

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/lieyan/firescribe/internal/app"
	"github.com/lieyan/firescribe/internal/db"
	"github.com/lieyan/firescribe/internal/recognizer"
	"github.com/lieyan/firescribe/internal/storage"
)

func TestDocumentRenameAndDeleteKeepSearchIndexConsistent(t *testing.T) {
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
	doc, err := application.ImportDocument(ctx, app.ImportOptions{Title: "旧标题"}, app.ImportFile{
		Name: "page.png", Reader: bytes.NewReader(testPNG(t)),
	})
	if err != nil {
		t.Fatal(err)
	}
	pages, err := application.Store.ListPages(ctx, doc.ID)
	if err != nil || len(pages) != 1 {
		t.Fatalf("pages = %+v, err = %v", pages, err)
	}
	if _, err := application.SaveTextVersion(ctx, app.TextVersion{
		DocumentID: doc.ID, PageID: pages[0].ID, Kind: "candidate", Text: "正文内容", Status: "draft",
	}); err != nil {
		t.Fatal(err)
	}

	newTitle := "新标题检索"
	if _, err := application.Store.PatchDocument(ctx, doc.ID, &newTitle, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	results, err := application.Store.Search(ctx, "新标题")
	if err != nil || len(results) != 1 || results[0].DocumentID != doc.ID {
		t.Fatalf("renamed title search = %+v, err = %v", results, err)
	}
	if err := application.Store.DeleteDocument(ctx, doc.ID); err != nil {
		t.Fatal(err)
	}
	var indexed int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM text_search WHERE document_id = ?`, doc.ID).Scan(&indexed); err != nil {
		t.Fatal(err)
	}
	if indexed != 0 {
		t.Fatalf("deleted document left %d search rows", indexed)
	}
}
