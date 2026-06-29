package db_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/lieyan/firescribe/internal/db"
)

func TestMigrateCreatesFTSTrigramSearch(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "firescribe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := db.Migrate(conn); err != nil {
		t.Fatal(err)
	}

	if _, err := conn.Exec(`INSERT INTO text_search(document_id, page_id, text_version_id, title, body) VALUES ('doc', 'page', 'txt', '题名', '清晰扫描稿火光文字')`); err != nil {
		t.Fatal(err)
	}
	var pageID string
	err = conn.QueryRow(`SELECT page_id FROM text_search WHERE text_search MATCH ?`, `"火光文"`).Scan(&pageID)
	if err != nil {
		t.Fatal(err)
	}
	if pageID != "page" {
		t.Fatalf("pageID = %q", pageID)
	}

	err = conn.QueryRow(`SELECT page_id FROM text_search WHERE text_search MATCH ?`, `"不存在"`).Scan(&pageID)
	if err != sql.ErrNoRows {
		t.Fatalf("expected no rows, got %v", err)
	}
}
