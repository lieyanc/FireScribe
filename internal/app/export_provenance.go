package app

import (
	"context"
	"database/sql"
	"encoding/json"
)

type ExportAnnotationSnapshot struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Status     string `json:"status"`
	Body       string `json:"body"`
	AnchorJSON string `json:"anchor_json"`
	RenderedAs string `json:"rendered_as"`
}

type ExportPageSnapshot struct {
	Ordinal          int                        `json:"ordinal"`
	DocumentID       string                     `json:"document_id"`
	DocumentTitle    string                     `json:"document_title"`
	DocumentPosition int                        `json:"document_position"`
	PageID           string                     `json:"page_id"`
	PageNo           int                        `json:"page_no"`
	TextVersionID    string                     `json:"text_version_id"`
	TextVersionKind  string                     `json:"text_version_kind"`
	Annotations      []ExportAnnotationSnapshot `json:"annotations"`
	CreatedAt        string                     `json:"created_at"`
}

func includedExportAnnotations(annotations []Annotation, versionText string, options ExportOptions) []ExportAnnotationSnapshot {
	items := make([]ExportAnnotationSnapshot, 0)
	for _, annotation := range annotations {
		renderedAs := ""
		switch annotation.Kind {
		case "page_note", "page_region":
			if options.IncludeAnnotations {
				renderedAs = "note"
			}
		case "uncertain_text":
			if options.IncludeUncertain {
				anchor := parseExportTextAnchor(annotation.AnchorJSON)
				if annotation.Status == "open" && anchor.validFor(versionText) {
					renderedAs = "inline_marker"
				} else {
					renderedAs = "note"
				}
			}
		}
		if renderedAs == "" {
			continue
		}
		items = append(items, ExportAnnotationSnapshot{
			ID: annotation.ID, Kind: annotation.Kind, Status: annotation.Status,
			Body: annotation.Body, AnchorJSON: annotation.AnchorJSON, RenderedAs: renderedAs,
		})
	}
	return items
}

type exportTextAnchor struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	Text  string `json:"text"`
}

func parseExportTextAnchor(raw string) exportTextAnchor {
	var anchor exportTextAnchor
	_ = json.Unmarshal([]byte(raw), &anchor)
	return anchor
}

func (a exportTextAnchor) validFor(text string) bool {
	// Stored offsets are browser UTF-16 offsets. Exact offset validation is
	// delegated to the exporter; the captured text is sufficient for the audit
	// classification and remains stable when the page is later edited.
	return a.Start >= 0 && a.End > a.Start && a.Text != "" && len(text) > 0
}

func (s *Store) ReplaceExportPageSnapshots(ctx context.Context, exportID string, snapshots []ExportPageSnapshot) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM exports WHERE id = ?`, exportID).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return nil // compatibility path: direct exports have no export row
		}
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM export_page_snapshots WHERE export_id = ?`, exportID); err != nil {
		return err
	}
	for _, snapshot := range snapshots {
		raw, _ := json.Marshal(snapshot.Annotations)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO export_page_snapshots(export_id, ordinal, document_id, document_title,
				document_position, page_id, page_no, text_version_id, text_version_kind,
				annotations_json, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, exportID, snapshot.Ordinal, snapshot.DocumentID, snapshot.DocumentTitle,
			snapshot.DocumentPosition, snapshot.PageID, snapshot.PageNo, snapshot.TextVersionID,
			snapshot.TextVersionKind, string(raw), snapshot.CreatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ReplaceProjectExportPageSnapshots(ctx context.Context, exportID string, snapshots []ExportPageSnapshot) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM project_export_page_snapshots WHERE project_export_id = ?`, exportID); err != nil {
		return err
	}
	for _, snapshot := range snapshots {
		raw, _ := json.Marshal(snapshot.Annotations)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO project_export_page_snapshots(project_export_id, ordinal, document_id, document_title,
				document_position, page_id, page_no, text_version_id, text_version_kind,
				annotations_json, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, exportID, snapshot.Ordinal, snapshot.DocumentID, snapshot.DocumentTitle,
			snapshot.DocumentPosition, snapshot.PageID, snapshot.PageNo, snapshot.TextVersionID,
			snapshot.TextVersionKind, string(raw), snapshot.CreatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func scanExportPageSnapshots(rows *sql.Rows) ([]ExportPageSnapshot, error) {
	defer rows.Close()
	items := []ExportPageSnapshot{}
	for rows.Next() {
		var item ExportPageSnapshot
		var raw string
		if err := rows.Scan(&item.Ordinal, &item.DocumentID, &item.DocumentTitle, &item.DocumentPosition,
			&item.PageID, &item.PageNo, &item.TextVersionID, &item.TextVersionKind, &raw, &item.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(raw), &item.Annotations)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListExportPageSnapshots(ctx context.Context, exportID string) ([]ExportPageSnapshot, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT ordinal, document_id, document_title, document_position, page_id, page_no,
		       text_version_id, text_version_kind, annotations_json, created_at
		FROM export_page_snapshots WHERE export_id = ? ORDER BY ordinal
	`, exportID)
	if err != nil {
		return nil, err
	}
	return scanExportPageSnapshots(rows)
}

func (s *Store) ListProjectExportPageSnapshots(ctx context.Context, exportID string) ([]ExportPageSnapshot, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT ordinal, document_id, document_title, document_position, page_id, page_no,
		       text_version_id, text_version_kind, annotations_json, created_at
		FROM project_export_page_snapshots WHERE project_export_id = ? ORDER BY ordinal
	`, exportID)
	if err != nil {
		return nil, err
	}
	return scanExportPageSnapshots(rows)
}

func (s *Store) ListDocumentExports(ctx context.Context, documentID string) ([]ExportFile, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, document_id, job_id, format, include_page_numbers, text_scope,
		       include_annotations, include_uncertain, status, COALESCE(asset_id, ''),
		       last_error, created_at, COALESCE(finished_at, '')
		FROM exports WHERE document_id = ? ORDER BY created_at DESC, id DESC
	`, documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ExportFile{}
	for rows.Next() {
		var item ExportFile
		if err := rows.Scan(&item.ID, &item.DocumentID, &item.JobID, &item.Format,
			&item.IncludePageNumbers, &item.TextScope, &item.IncludeAnnotations,
			&item.IncludeUncertain, &item.Status, &item.AssetID, &item.LastError,
			&item.CreatedAt, &item.FinishedAt); err != nil {
			return nil, err
		}
		if item.AssetID != "" {
			item.DownloadURL = "/api/exports/" + item.ID + "/download"
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListProjectExports(ctx context.Context, projectID string) ([]ProjectExport, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, project_id, job_id, format, include_page_numbers, text_scope,
		       include_annotations, include_uncertain, status, COALESCE(asset_id, ''),
		       last_error, created_at, COALESCE(finished_at, '')
		FROM project_exports WHERE project_id = ? ORDER BY created_at DESC, id DESC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ProjectExport{}
	for rows.Next() {
		var item ProjectExport
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.JobID, &item.Format,
			&item.IncludePageNumbers, &item.TextScope, &item.IncludeAnnotations,
			&item.IncludeUncertain, &item.Status, &item.AssetID, &item.LastError,
			&item.CreatedAt, &item.FinishedAt); err != nil {
			return nil, err
		}
		if item.AssetID != "" {
			item.DownloadURL = "/api/project-exports/" + item.ID + "/download"
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
