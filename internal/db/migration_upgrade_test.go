package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestEffectiveVersionsMigrationRebuildsLegacyPageDetailsAndFTS(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "firescribe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if _, err := conn.Exec(`
		CREATE TABLE schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL
		)
	`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"0001_init.sql", "0002_annotations.sql", "0003_run_pages.sql"} {
		raw, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(string(raw)); err != nil {
			t.Fatalf("apply legacy migration %s: %v", name, err)
		}
		if _, err := conn.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)`, name, "2026-01-01T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := conn.Exec(`
		INSERT INTO documents(id, title, status, page_count, created_at, updated_at)
		VALUES ('doc', '迁移文档', 'finalized', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:02Z');
		INSERT INTO pages(id, document_id, page_no, status, created_at, updated_at)
		VALUES ('page', 'doc', 1, 'reviewing', '2026-01-01T00:00:00Z', '2026-01-01T00:00:02Z');
		INSERT INTO text_versions(id, document_id, page_id, kind, text, status, created_at)
		VALUES
		  ('old-final', 'doc', 'page', 'final', '旧定稿迁移词', 'verified', '2026-01-01T00:00:01Z'),
		  ('new-manual', 'doc', 'page', 'manual', '新草稿迁移词', 'draft', '2026-01-01T00:00:02Z');
		INSERT INTO text_search(document_id, page_id, text_version_id, title, body)
		VALUES ('doc', 'page', 'old-final', '迁移文档', '旧定稿迁移词');
	`); err != nil {
		t.Fatal(err)
	}

	if err := Migrate(conn); err != nil {
		t.Fatal(err)
	}

	var id, kind, text string
	if err := conn.QueryRow(`SELECT id, kind, text FROM effective_text_versions WHERE page_id = 'page'`).Scan(&id, &kind, &text); err != nil {
		t.Fatal(err)
	}
	if id != "new-manual" || kind != "manual" || text != "新草稿迁移词" {
		t.Fatalf("effective version = (%q, %q, %q)", id, kind, text)
	}

	var hasCandidate, hasManual, hasFinal int
	if err := conn.QueryRow(`SELECT has_candidate, has_manual, has_final FROM page_details WHERE page_id = 'page'`).Scan(&hasCandidate, &hasManual, &hasFinal); err != nil {
		t.Fatal(err)
	}
	if hasCandidate != 0 || hasManual != 1 || hasFinal != 0 {
		t.Fatalf("page flags = candidate:%d manual:%d final:%d", hasCandidate, hasManual, hasFinal)
	}
	var documentStatus string
	if err := conn.QueryRow(`SELECT status FROM documents WHERE id = 'doc'`).Scan(&documentStatus); err != nil {
		t.Fatal(err)
	}
	if documentStatus != "reviewing" {
		t.Fatalf("document status = %q, want reviewing", documentStatus)
	}

	var indexedVersion string
	if err := conn.QueryRow(`SELECT text_version_id FROM text_search WHERE text_search MATCH '"新草稿"'`).Scan(&indexedVersion); err != nil {
		t.Fatal(err)
	}
	if indexedVersion != "new-manual" {
		t.Fatalf("indexed version = %q, want new-manual", indexedVersion)
	}
	if err := conn.QueryRow(`SELECT text_version_id FROM text_search WHERE text_search MATCH '"旧定稿"'`).Scan(&indexedVersion); err != sql.ErrNoRows {
		t.Fatalf("stale final remained indexed: %v", err)
	}
}

func TestExportOptionsMigrationPreservesLegacyRows(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "firescribe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Exec(`CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"0001_init.sql", "0002_annotations.sql", "0003_run_pages.sql", "0004_effective_versions.sql",
		"0005_recognition_metadata.sql", "0006_job_progress.sql",
	} {
		raw, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(string(raw)); err != nil {
			t.Fatalf("apply legacy migration %s: %v", name, err)
		}
		if _, err := conn.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)`, name, "2026-01-01T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := conn.Exec(`
		INSERT INTO documents(id, title, status, created_at, updated_at) VALUES ('doc-export', '旧导出', 'ready', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
		INSERT INTO jobs(id, type, status, target_type, target_id, created_at) VALUES ('job-export', 'export_document', 'succeeded', 'document', 'doc-export', '2026-01-01T00:00:00Z');
		INSERT INTO exports(id, document_id, job_id, format, include_page_numbers, status, created_at)
		VALUES ('export-old', 'doc-export', 'job-export', 'md', 1, 'succeeded', '2026-01-01T00:00:00Z');
	`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(conn); err != nil {
		t.Fatal(err)
	}
	var scope string
	var includeAnnotations, includeUncertain int
	if err := conn.QueryRow(`SELECT text_scope, include_annotations, include_uncertain FROM exports WHERE id = 'export-old'`).Scan(&scope, &includeAnnotations, &includeUncertain); err != nil {
		t.Fatal(err)
	}
	if scope != "current" || includeAnnotations != 0 || includeUncertain != 0 {
		t.Fatalf("legacy export defaults = scope:%q annotations:%d uncertain:%d", scope, includeAnnotations, includeUncertain)
	}
}

func TestExperimentAttemptRunsMigrationBackfillsExistingHistory(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "firescribe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Exec(`
		CREATE TABLE recognition_experiment_variants (
			id TEXT PRIMARY KEY,
			run_ids_json TEXT NOT NULL DEFAULT '[]'
		);
		INSERT INTO recognition_experiment_variants(id, run_ids_json)
		VALUES ('variant', '["run-1","run-2"]');
	`); err != nil {
		t.Fatal(err)
	}
	raw, err := migrationFiles.ReadFile("migrations/0020_experiment_attempt_runs.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(string(raw)); err != nil {
		t.Fatal(err)
	}
	var currentRunIDs string
	if err := conn.QueryRow(`SELECT current_run_ids_json FROM recognition_experiment_variants WHERE id = 'variant'`).Scan(&currentRunIDs); err != nil {
		t.Fatal(err)
	}
	if currentRunIDs != `["run-1","run-2"]` {
		t.Fatalf("current run ids = %s", currentRunIDs)
	}
}
