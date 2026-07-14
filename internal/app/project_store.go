package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func (s *Store) CreateProject(ctx context.Context, project Project) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO projects(id, name, description, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
	`, project.ID, project.Name, project.Description, project.CreatedAt, project.UpdatedAt)
	return err
}

func (s *Store) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.name, p.description,
		       COUNT(pd.document_id), COALESCE(SUM(d.page_count), 0),
		       p.created_at, p.updated_at
		FROM projects p
		LEFT JOIN project_documents pd ON pd.project_id = p.id
		LEFT JOIN documents d ON d.id = pd.document_id
		GROUP BY p.id, p.name, p.description, p.created_at, p.updated_at
		ORDER BY p.updated_at DESC, p.created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	projects := []Project{}
	for rows.Next() {
		var project Project
		if err := rows.Scan(&project.ID, &project.Name, &project.Description, &project.DocumentCount, &project.PageCount, &project.CreatedAt, &project.UpdatedAt); err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	return projects, rows.Err()
}

func (s *Store) GetProject(ctx context.Context, id string) (Project, error) {
	var project Project
	err := s.db.QueryRowContext(ctx, `
		SELECT p.id, p.name, p.description,
		       COUNT(pd.document_id), COALESCE(SUM(d.page_count), 0),
		       p.created_at, p.updated_at
		FROM projects p
		LEFT JOIN project_documents pd ON pd.project_id = p.id
		LEFT JOIN documents d ON d.id = pd.document_id
		WHERE p.id = ?
		GROUP BY p.id, p.name, p.description, p.created_at, p.updated_at
	`, id).Scan(&project.ID, &project.Name, &project.Description, &project.DocumentCount, &project.PageCount, &project.CreatedAt, &project.UpdatedAt)
	return project, err
}

func (s *Store) GetProjectDetail(ctx context.Context, id string) (ProjectDetail, error) {
	project, err := s.GetProject(ctx, id)
	if err != nil {
		return ProjectDetail{}, err
	}
	documents, err := s.ListProjectDocuments(ctx, id)
	if err != nil {
		return ProjectDetail{}, err
	}
	return ProjectDetail{Project: project, Documents: documents}, nil
}

func (s *Store) PatchProject(ctx context.Context, id string, name, description *string) (Project, error) {
	project, err := s.GetProject(ctx, id)
	if err != nil {
		return Project{}, err
	}
	if name != nil {
		project.Name = strings.TrimSpace(*name)
		if project.Name == "" {
			return Project{}, errors.New("project name is required")
		}
	}
	if description != nil {
		project.Description = *description
	}
	project.UpdatedAt = now()
	result, err := s.db.ExecContext(ctx, `
		UPDATE projects SET name = ?, description = ?, updated_at = ? WHERE id = ?
	`, project.Name, project.Description, project.UpdatedAt, id)
	if err != nil {
		return Project{}, err
	}
	if changed, err := result.RowsAffected(); err != nil {
		return Project{}, err
	} else if changed == 0 {
		return Project{}, sql.ErrNoRows
	}
	return s.GetProject(ctx, id)
}

func (s *Store) DeleteProject(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil {
		return err
	} else if changed == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) ListActiveJobsForTarget(ctx context.Context, targetType, targetID string) ([]Job, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, status, target_type, target_id, payload_json, attempts, max_attempts,
		       last_error, progress_current, progress_total, progress_message, result_json,
		       created_at, started_at, finished_at
		FROM jobs
		WHERE target_type = ? AND target_id = ? AND status IN ('queued', 'running')
		ORDER BY created_at
	`, targetType, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := []Job{}
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *Store) ListProjectDocuments(ctx context.Context, projectID string) ([]ProjectDocument, error) {
	if _, err := s.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT d.id, d.title, d.description, d.author, d.source, d.status,
		       d.page_count, d.created_at, d.updated_at, pd.position, pd.added_at
		FROM project_documents pd
		JOIN documents d ON d.id = pd.document_id
		WHERE pd.project_id = ?
		ORDER BY pd.position, pd.added_at, d.id
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	documents := []ProjectDocument{}
	for rows.Next() {
		var document ProjectDocument
		if err := rows.Scan(&document.ID, &document.Title, &document.Description, &document.Author, &document.Source,
			&document.Status, &document.PageCount, &document.CreatedAt, &document.UpdatedAt, &document.Position, &document.AddedAt); err != nil {
			return nil, err
		}
		documents = append(documents, document)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for index := range documents {
		tags, err := s.ListDocumentTags(ctx, documents[index].ID)
		if err != nil {
			return nil, err
		}
		documents[index].Tags = tags
	}
	return documents, nil
}

func (s *Store) AddProjectDocument(ctx context.Context, projectID, documentID string, requestedPosition *int) ([]ProjectDocument, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := requireRow(ctx, tx, `SELECT 1 FROM projects WHERE id = ?`, projectID); err != nil {
		return nil, err
	}
	if err := requireRow(ctx, tx, `SELECT 1 FROM documents WHERE id = ?`, documentID); err != nil {
		return nil, err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM project_documents WHERE project_id = ?`, projectID).Scan(&count); err != nil {
		return nil, err
	}
	position := count
	if requestedPosition != nil {
		position = *requestedPosition
		if position < 0 {
			position = 0
		}
		if position > count {
			position = count
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE project_documents SET position = position + 1
		WHERE project_id = ? AND position >= ?
	`, projectID, position); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO project_documents(project_id, document_id, position, added_at)
		VALUES (?, ?, ?, ?)
	`, projectID, documentID, position, now()); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, fmt.Errorf("document already belongs to project")
		}
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE projects SET updated_at = ? WHERE id = ?`, now(), projectID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.ListProjectDocuments(ctx, projectID)
}

func (s *Store) RemoveProjectDocument(ctx context.Context, projectID, documentID string) ([]ProjectDocument, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var position int
	if err := tx.QueryRowContext(ctx, `
		SELECT position FROM project_documents WHERE project_id = ? AND document_id = ?
	`, projectID, documentID).Scan(&position); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM project_documents WHERE project_id = ? AND document_id = ?`, projectID, documentID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE project_documents SET position = position - 1
		WHERE project_id = ? AND position > ?
	`, projectID, position); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE projects SET updated_at = ? WHERE id = ?`, now(), projectID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.ListProjectDocuments(ctx, projectID)
}

func (s *Store) ReorderProjectDocuments(ctx context.Context, projectID string, documentIDs []string) ([]ProjectDocument, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT document_id FROM project_documents WHERE project_id = ?`, projectID)
	if err != nil {
		return nil, err
	}
	existing := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		existing[id] = true
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(existing) != len(documentIDs) {
		return nil, errors.New("document_ids are required to contain every project document exactly once")
	}
	seen := map[string]bool{}
	for _, id := range documentIDs {
		if !existing[id] || seen[id] {
			return nil, errors.New("document_ids are required to contain every project document exactly once")
		}
		seen[id] = true
	}
	for position, id := range documentIDs {
		if _, err := tx.ExecContext(ctx, `
			UPDATE project_documents SET position = ? WHERE project_id = ? AND document_id = ?
		`, position, projectID, id); err != nil {
			return nil, err
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE projects SET updated_at = ? WHERE id = ?`, now(), projectID)
	if err != nil {
		return nil, err
	}
	if changed, err := result.RowsAffected(); err != nil {
		return nil, err
	} else if changed == 0 {
		return nil, sql.ErrNoRows
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.ListProjectDocuments(ctx, projectID)
}

func requireRow(ctx context.Context, tx *sql.Tx, query string, args ...any) error {
	var value int
	return tx.QueryRowContext(ctx, query, args...).Scan(&value)
}

func (s *Store) CreateProjectExport(ctx context.Context, export ProjectExport) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO project_exports(
			id, project_id, job_id, format, include_page_numbers, text_scope, include_annotations, include_uncertain,
			status, asset_id, last_error, created_at, finished_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, NULLIF(?, ''))
	`, export.ID, export.ProjectID, export.JobID, export.Format, export.IncludePageNumbers,
		export.TextScope, export.IncludeAnnotations, export.IncludeUncertain, export.Status,
		export.AssetID, export.LastError, export.CreatedAt, export.FinishedAt)
	return err
}

func (s *Store) GetProjectExport(ctx context.Context, id string) (ProjectExport, error) {
	var export ProjectExport
	var assetID, finishedAt sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT id, project_id, job_id, format, include_page_numbers, text_scope, include_annotations, include_uncertain,
		       status, asset_id, last_error, created_at, finished_at
		FROM project_exports WHERE id = ?
	`, id).Scan(&export.ID, &export.ProjectID, &export.JobID, &export.Format, &export.IncludePageNumbers,
		&export.TextScope, &export.IncludeAnnotations, &export.IncludeUncertain,
		&export.Status, &assetID, &export.LastError, &export.CreatedAt, &finishedAt)
	export.AssetID = nullString(assetID)
	export.FinishedAt = nullString(finishedAt)
	if err == nil && export.AssetID != "" {
		asset, assetErr := s.GetAsset(ctx, export.AssetID)
		if assetErr != nil {
			return ProjectExport{}, assetErr
		}
		export.StoragePath = asset.StoragePath
		export.DownloadURL = "/api/project-exports/" + export.ID + "/download"
	}
	return export, err
}

func (s *Store) MarkProjectExportRunning(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE project_exports SET status = 'running', last_error = '', finished_at = NULL
		WHERE id = ? AND status = 'queued'
	`, id)
	return err
}

func (s *Store) FinishProjectExport(ctx context.Context, id, assetID string, cause error) error {
	status, lastError := "succeeded", ""
	if cause != nil {
		status, lastError = "failed", cause.Error()
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE project_exports SET status = ?, asset_id = NULLIF(?, ''), last_error = ?, finished_at = ? WHERE id = ?
	`, status, assetID, lastError, now(), id)
	return err
}

func (s *Store) CancelProjectExport(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE project_exports SET status = 'canceled', last_error = 'canceled', finished_at = ?
		WHERE id = ? AND status IN ('queued', 'running')
	`, now(), id)
	return err
}

func (s *Store) RequeueProjectExport(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE project_exports SET status = 'queued', asset_id = NULL, last_error = '', finished_at = NULL WHERE id = ?
	`, id)
	return err
}
