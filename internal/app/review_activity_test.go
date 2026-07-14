package app_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/lieyan/firescribe/internal/app"
	"github.com/lieyan/firescribe/internal/db"
)

func TestRecordReviewActivityKeepsCumulativeMaximum(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open(filepath.Join(t.TempDir(), "firescribe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := db.Migrate(conn); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`
		INSERT INTO documents(id, title, status, created_at, updated_at) VALUES ('doc', '审校', 'ready', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
		INSERT INTO pages(id, document_id, page_no, status, created_at, updated_at) VALUES ('page', 'doc', 1, 'reviewing', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
	`); err != nil {
		t.Fatal(err)
	}
	store := app.NewStore(conn)
	if _, err := store.RecordReviewActivity(ctx, "page", "session", 12.5, false); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordReviewActivity(ctx, "page", "session", 8, false); err != nil {
		t.Fatal(err)
	}
	item, err := store.RecordReviewActivity(ctx, "page", "session", 20, true)
	if err != nil {
		t.Fatal(err)
	}
	if item.ActiveSeconds != 20 || item.FinishedAt == "" {
		t.Fatalf("activity = %+v", item)
	}
	item, err = store.RecordReviewActivity(ctx, "page", "session", 30, false)
	if err != nil {
		t.Fatal(err)
	}
	if item.ActiveSeconds != 20 {
		t.Fatalf("finished activity accepted a late update: %+v", item)
	}
}
