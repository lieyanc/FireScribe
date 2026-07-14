package app_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/lieyan/firescribe/internal/app"
	"github.com/lieyan/firescribe/internal/db"
)

func TestProjectStoreCRUDAndDocumentOrdering(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open(filepath.Join(t.TempDir(), "firescribe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := db.Migrate(conn); err != nil {
		t.Fatal(err)
	}
	store := app.NewStore(conn)
	for _, document := range []app.Document{
		{ID: "doc_a", Title: "甲", Status: "ready", PageCount: 2, CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
		{ID: "doc_b", Title: "乙", Status: "ready", PageCount: 3, CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
	} {
		if err := store.CreateDocument(ctx, document); err != nil {
			t.Fatal(err)
		}
	}
	project := app.Project{ID: "prj_test", Name: "文集", Description: "说明", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"}
	if err := store.CreateProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddProjectDocument(ctx, project.ID, "doc_a", nil); err != nil {
		t.Fatal(err)
	}
	zero := 0
	documents, err := store.AddProjectDocument(ctx, project.ID, "doc_b", &zero)
	if err != nil {
		t.Fatal(err)
	}
	if len(documents) != 2 || documents[0].ID != "doc_b" || documents[0].Position != 0 || documents[1].ID != "doc_a" || documents[1].Position != 1 {
		t.Fatalf("unexpected inserted order: %+v", documents)
	}
	documents, err = store.ReorderProjectDocuments(ctx, project.ID, []string{"doc_a", "doc_b"})
	if err != nil {
		t.Fatal(err)
	}
	if documents[0].ID != "doc_a" || documents[1].ID != "doc_b" {
		t.Fatalf("unexpected reordered documents: %+v", documents)
	}
	detail, err := store.GetProjectDetail(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.DocumentCount != 2 || detail.PageCount != 5 || len(detail.Documents) != 2 {
		t.Fatalf("unexpected project aggregate: %+v", detail)
	}
	name := "新文集"
	patched, err := store.PatchProject(ctx, project.ID, &name, nil)
	if err != nil {
		t.Fatal(err)
	}
	if patched.Name != name {
		t.Fatalf("name = %q", patched.Name)
	}
	documents, err = store.RemoveProjectDocument(ctx, project.ID, "doc_a")
	if err != nil {
		t.Fatal(err)
	}
	if len(documents) != 1 || documents[0].ID != "doc_b" || documents[0].Position != 0 {
		t.Fatalf("unexpected documents after remove: %+v", documents)
	}
	if err := store.DeleteProject(ctx, project.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetDocument(ctx, "doc_b"); err != nil {
		t.Fatalf("deleting project deleted document: %v", err)
	}
}
