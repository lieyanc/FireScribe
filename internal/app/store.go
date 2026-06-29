package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/lieyan/firescribe/internal/storage"
)

type Store struct {
	db *sql.DB
}

type DocumentFilter struct {
	Query  string
	Status string
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) CreateDocument(ctx context.Context, doc Document) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO documents(id, title, description, author, source, status, page_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, doc.ID, doc.Title, doc.Description, doc.Author, doc.Source, doc.Status, doc.PageCount, doc.CreatedAt, doc.UpdatedAt)
	return err
}

func (s *Store) ListDocuments(ctx context.Context, filter DocumentFilter) ([]Document, error) {
	query := strings.TrimSpace(filter.Query)
	status := strings.TrimSpace(filter.Status)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, title, description, author, source, status, page_count, created_at, updated_at
		FROM documents
		WHERE (? = '' OR status = ?)
		  AND (? = '' OR title LIKE '%' || ? || '%' OR author LIKE '%' || ? || '%' OR source LIKE '%' || ? || '%')
		ORDER BY updated_at DESC
	`, status, status, query, query, query, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	docs := []Document{}
	for rows.Next() {
		var doc Document
		if err := rows.Scan(&doc.ID, &doc.Title, &doc.Description, &doc.Author, &doc.Source, &doc.Status, &doc.PageCount, &doc.CreatedAt, &doc.UpdatedAt); err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	return docs, rows.Err()
}

func (s *Store) GetDocument(ctx context.Context, id string) (Document, error) {
	var doc Document
	err := s.db.QueryRowContext(ctx, `
		SELECT id, title, description, author, source, status, page_count, created_at, updated_at
		FROM documents WHERE id = ?
	`, id).Scan(&doc.ID, &doc.Title, &doc.Description, &doc.Author, &doc.Source, &doc.Status, &doc.PageCount, &doc.CreatedAt, &doc.UpdatedAt)
	return doc, err
}

func (s *Store) PatchDocument(ctx context.Context, id string, title, description, author, source, status *string) (Document, error) {
	doc, err := s.GetDocument(ctx, id)
	if err != nil {
		return Document{}, err
	}
	if title != nil {
		doc.Title = strings.TrimSpace(*title)
	}
	if description != nil {
		doc.Description = *description
	}
	if author != nil {
		doc.Author = *author
	}
	if source != nil {
		doc.Source = *source
	}
	if status != nil {
		doc.Status = *status
	}
	if doc.Title == "" {
		doc.Title = "未命名文档"
	}
	doc.UpdatedAt = now()
	_, err = s.db.ExecContext(ctx, `
		UPDATE documents
		SET title = ?, description = ?, author = ?, source = ?, status = ?, updated_at = ?
		WHERE id = ?
	`, doc.Title, doc.Description, doc.Author, doc.Source, doc.Status, doc.UpdatedAt, id)
	if err != nil {
		return Document{}, err
	}
	return doc, nil
}

func (s *Store) UpdateDocumentStatus(ctx context.Context, id, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE documents SET status = ?, updated_at = ? WHERE id = ?`, status, now(), id)
	return err
}

func (s *Store) UpdateDocumentReady(ctx context.Context, id string, pageCount int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE documents SET status = 'ready', page_count = ?, updated_at = ? WHERE id = ?`, pageCount, now(), id)
	return err
}

func (s *Store) DeleteDocument(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM documents WHERE id = ?`, id)
	return err
}

func (s *Store) UpsertAsset(ctx context.Context, kind string, file storage.StoredFile) (Asset, error) {
	id := newID("ast")
	createdAt := now()
	_, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO assets(id, kind, sha256, original_name, mime_type, byte_size, storage_path, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, id, kind, file.SHA256, file.OriginalName, file.MimeType, file.ByteSize, file.RelativePath, createdAt)
	if err != nil {
		return Asset{}, err
	}
	var asset Asset
	err = s.db.QueryRowContext(ctx, `
		SELECT id, kind, sha256, original_name, mime_type, byte_size, storage_path, created_at
		FROM assets WHERE kind = ? AND sha256 = ?
	`, kind, file.SHA256).Scan(&asset.ID, &asset.Kind, &asset.SHA256, &asset.OriginalName, &asset.MimeType, &asset.ByteSize, &asset.StoragePath, &asset.CreatedAt)
	return asset, err
}

func (s *Store) GetAsset(ctx context.Context, id string) (Asset, error) {
	var asset Asset
	err := s.db.QueryRowContext(ctx, `
		SELECT id, kind, sha256, original_name, mime_type, byte_size, storage_path, created_at
		FROM assets WHERE id = ?
	`, id).Scan(&asset.ID, &asset.Kind, &asset.SHA256, &asset.OriginalName, &asset.MimeType, &asset.ByteSize, &asset.StoragePath, &asset.CreatedAt)
	return asset, err
}

func (s *Store) LinkDocumentAsset(ctx context.Context, documentID, assetID, role string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO document_assets(document_id, asset_id, role, created_at)
		VALUES (?, ?, ?, ?)
	`, documentID, assetID, role, now())
	return err
}

func (s *Store) CreatePage(ctx context.Context, page Page) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO pages(id, document_id, page_no, image_asset_id, thumb_asset_id, width, height, status, created_at, updated_at)
		VALUES (?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, ?, ?)
	`, page.ID, page.DocumentID, page.PageNo, page.ImageAssetID, page.ThumbAssetID, page.Width, page.Height, page.Status, page.CreatedAt, page.UpdatedAt)
	return err
}

func (s *Store) GetPage(ctx context.Context, id string) (Page, error) {
	var page Page
	var imageAsset, thumbAsset sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT id, document_id, page_no, image_asset_id, thumb_asset_id, width, height, status, created_at, updated_at
		FROM pages WHERE id = ?
	`, id).Scan(&page.ID, &page.DocumentID, &page.PageNo, &imageAsset, &thumbAsset, &page.Width, &page.Height, &page.Status, &page.CreatedAt, &page.UpdatedAt)
	page.ImageAssetID = nullString(imageAsset)
	page.ThumbAssetID = nullString(thumbAsset)
	return page, err
}

func (s *Store) ListPages(ctx context.Context, documentID string) ([]Page, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, document_id, page_no, image_asset_id, thumb_asset_id, width, height, status, created_at, updated_at
		FROM pages WHERE document_id = ? ORDER BY page_no
	`, documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	pages := []Page{}
	for rows.Next() {
		var page Page
		var imageAsset, thumbAsset sql.NullString
		if err := rows.Scan(&page.ID, &page.DocumentID, &page.PageNo, &imageAsset, &thumbAsset, &page.Width, &page.Height, &page.Status, &page.CreatedAt, &page.UpdatedAt); err != nil {
			return nil, err
		}
		page.ImageAssetID = nullString(imageAsset)
		page.ThumbAssetID = nullString(thumbAsset)
		pages = append(pages, page)
	}
	return pages, rows.Err()
}

func (s *Store) ListPageDetails(ctx context.Context, documentID string) ([]PageDetail, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT page_id, document_id, page_no, page_status, width, height, image_asset_id, thumb_asset_id,
		       recognition_count, best_confidence, last_provider, last_model, last_recognized_at,
		       has_candidate, has_manual, has_final, updated_at
		FROM page_details WHERE document_id = ? ORDER BY page_no
	`, documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	pages := []PageDetail{}
	for rows.Next() {
		detail, err := scanPageDetail(rows)
		if err != nil {
			return nil, err
		}
		pages = append(pages, detail)
	}
	return pages, rows.Err()
}

func (s *Store) GetPageDetail(ctx context.Context, pageID string) (PageDetail, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT page_id, document_id, page_no, page_status, width, height, image_asset_id, thumb_asset_id,
		       recognition_count, best_confidence, last_provider, last_model, last_recognized_at,
		       has_candidate, has_manual, has_final, updated_at
		FROM page_details WHERE page_id = ?
	`, pageID)
	return scanPageDetail(row)
}

func (s *Store) UpdatePageStatus(ctx context.Context, pageID, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE pages SET status = ?, updated_at = ? WHERE id = ?`, status, now(), pageID)
	return err
}

func (s *Store) CreateRecognitionRun(ctx context.Context, run RecognitionRun) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO recognition_runs(id, document_id, provider, model, prompt_version, config_json, status, started_at, finished_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?)
	`, run.ID, run.DocumentID, run.Provider, run.Model, run.PromptVersion, run.ConfigJSON, run.Status, run.StartedAt, run.FinishedAt, run.CreatedAt)
	return err
}

func (s *Store) ListRecognitionRuns(ctx context.Context, documentID string) ([]RecognitionRun, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, document_id, provider, model, prompt_version, config_json, status, started_at, finished_at, created_at
		FROM recognition_runs WHERE document_id = ? ORDER BY created_at DESC
	`, documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	runs := []RecognitionRun{}
	for rows.Next() {
		run, err := scanRecognitionRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (s *Store) GetRecognitionRun(ctx context.Context, id string) (RecognitionRun, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, document_id, provider, model, prompt_version, config_json, status, started_at, finished_at, created_at
		FROM recognition_runs WHERE id = ?
	`, id)
	return scanRecognitionRun(row)
}

func (s *Store) UpdateRecognitionRunStatus(ctx context.Context, id, status, startedAt, finishedAt string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE recognition_runs
		SET status = ?, started_at = COALESCE(NULLIF(?, ''), started_at), finished_at = COALESCE(NULLIF(?, ''), finished_at)
		WHERE id = ?
	`, status, startedAt, finishedAt, id)
	return err
}

func (s *Store) CreateRecognitionResult(ctx context.Context, result RecognitionResult) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO recognition_results(id, run_id, page_id, text, confidence, raw_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, result.ID, result.RunID, result.PageID, result.Text, result.Confidence, result.RawJSON, result.CreatedAt)
	return err
}

func (s *Store) ListRecognitionResults(ctx context.Context, pageID string) ([]RecognitionResult, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.id, r.run_id, r.page_id, r.text, r.confidence, r.raw_json, r.created_at, run.provider, run.model
		FROM recognition_results r
		JOIN recognition_runs run ON run.id = r.run_id
		WHERE r.page_id = ?
		ORDER BY r.created_at DESC
	`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := []RecognitionResult{}
	for rows.Next() {
		var result RecognitionResult
		var confidence sql.NullFloat64
		if err := rows.Scan(&result.ID, &result.RunID, &result.PageID, &result.Text, &confidence, &result.RawJSON, &result.CreatedAt, &result.Provider, &result.Model); err != nil {
			return nil, err
		}
		if confidence.Valid {
			result.Confidence = &confidence.Float64
		}
		results = append(results, result)
	}
	return results, rows.Err()
}

func (s *Store) CreateTextVersion(ctx context.Context, version TextVersion) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO text_versions(id, document_id, page_id, kind, base_version_id, source_result_id, text, status, created_by, created_at)
		VALUES (?, ?, NULLIF(?, ''), ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, ?)
	`, version.ID, version.DocumentID, version.PageID, version.Kind, version.BaseVersionID, version.SourceResultID, version.Text, version.Status, version.CreatedBy, version.CreatedAt)
	if err != nil {
		return err
	}
	if version.PageID != "" && shouldIndexKind(version.Kind) {
		return s.IndexTextVersion(ctx, version)
	}
	return nil
}

func (s *Store) ListTextVersions(ctx context.Context, pageID string) ([]TextVersion, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, document_id, page_id, kind, base_version_id, source_result_id, text, status, created_by, created_at
		FROM text_versions WHERE page_id = ? ORDER BY created_at DESC
	`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	versions := []TextVersion{}
	for rows.Next() {
		version, err := scanTextVersion(rows)
		if err != nil {
			return nil, err
		}
		versions = append(versions, version)
	}
	return versions, rows.Err()
}

func (s *Store) LatestTextForPage(ctx context.Context, pageID string) (string, string, error) {
	var id, text string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, text
		FROM text_versions
		WHERE page_id = ?
		ORDER BY CASE kind
			WHEN 'final' THEN 0
			WHEN 'manual' THEN 1
			WHEN 'candidate' THEN 2
			WHEN 'raw_selected' THEN 3
			ELSE 4
		END, created_at DESC
		LIMIT 1
	`, pageID).Scan(&id, &text)
	if err == nil {
		return id, text, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", "", err
	}
	err = s.db.QueryRowContext(ctx, `
		SELECT id, text FROM recognition_results
		WHERE page_id = ? ORDER BY created_at DESC LIMIT 1
	`, pageID).Scan(&id, &text)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	return id, text, err
}

func (s *Store) IndexTextVersion(ctx context.Context, version TextVersion) error {
	doc, err := s.GetDocument(ctx, version.DocumentID)
	if err != nil {
		return err
	}
	if version.Kind != "final" {
		var finals int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM text_versions WHERE page_id = ? AND kind = 'final'`, version.PageID).Scan(&finals); err != nil {
			return err
		}
		if finals > 0 {
			return nil
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM text_search WHERE page_id = ?`, version.PageID); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO text_search(document_id, page_id, text_version_id, title, body)
		VALUES (?, ?, ?, ?, ?)
	`, version.DocumentID, version.PageID, version.ID, doc.Title, version.Text); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) Search(ctx context.Context, query string) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []SearchResult{}, nil
	}
	if runeLen(query) >= 3 {
		results, err := s.searchFTS(ctx, quoteFTS(query))
		if err == nil {
			return results, nil
		}
	}
	return s.searchLike(ctx, query)
}

func (s *Store) CreateJob(ctx context.Context, job Job) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO jobs(id, type, status, target_type, target_id, payload_json, attempts, max_attempts, last_error, created_at, started_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''))
	`, job.ID, job.Type, job.Status, job.TargetType, job.TargetID, job.PayloadJSON, job.Attempts, job.MaxAttempts, job.LastError, job.CreatedAt, job.StartedAt, job.FinishedAt)
	return err
}

func (s *Store) ListJobs(ctx context.Context) ([]Job, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, status, target_type, target_id, payload_json, attempts, max_attempts, last_error, created_at, started_at, finished_at
		FROM jobs ORDER BY created_at DESC LIMIT 200
	`)
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

func (s *Store) GetJob(ctx context.Context, id string) (Job, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, type, status, target_type, target_id, payload_json, attempts, max_attempts, last_error, created_at, started_at, finished_at
		FROM jobs WHERE id = ?
	`, id)
	return scanJob(row)
}

func (s *Store) MarkJobRunning(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE jobs SET status = 'running', attempts = attempts + 1, started_at = ? WHERE id = ?`, now(), id)
	return err
}

func (s *Store) MarkJobDone(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE jobs SET status = 'succeeded', finished_at = ?, last_error = '' WHERE id = ?`, now(), id)
	return err
}

func (s *Store) MarkJobFailed(ctx context.Context, id string, cause error) error {
	_, err := s.db.ExecContext(ctx, `UPDATE jobs SET status = 'failed', finished_at = ?, last_error = ? WHERE id = ?`, now(), errorString(cause), id)
	return err
}

func (s *Store) MarkJobCanceled(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE jobs SET status = 'canceled', finished_at = ? WHERE id = ? AND status IN ('queued', 'running')`, now(), id)
	return err
}

func (s *Store) CreateAnnotation(ctx context.Context, annotation Annotation) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO annotations(id, document_id, page_id, text_version_id, kind, status, body, anchor_json, created_at, updated_at)
		VALUES (?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, ?, ?, ?)
	`, annotation.ID, annotation.DocumentID, annotation.PageID, annotation.TextVersionID, annotation.Kind, annotation.Status, annotation.Body, annotation.AnchorJSON, annotation.CreatedAt, annotation.UpdatedAt)
	return err
}

func (s *Store) ListAnnotations(ctx context.Context, documentID, pageID string) ([]Annotation, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, document_id, page_id, text_version_id, kind, status, body, anchor_json, created_at, updated_at
		FROM annotations
		WHERE document_id = ? AND (? = '' OR page_id = ?)
		ORDER BY CASE status WHEN 'open' THEN 0 WHEN 'resolved' THEN 1 ELSE 2 END, created_at DESC
	`, documentID, pageID, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	annotations := []Annotation{}
	for rows.Next() {
		annotation, err := scanAnnotation(rows)
		if err != nil {
			return nil, err
		}
		annotations = append(annotations, annotation)
	}
	return annotations, rows.Err()
}

func (s *Store) GetAnnotation(ctx context.Context, id string) (Annotation, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, document_id, page_id, text_version_id, kind, status, body, anchor_json, created_at, updated_at
		FROM annotations WHERE id = ?
	`, id)
	return scanAnnotation(row)
}

func (s *Store) PatchAnnotation(ctx context.Context, id string, status, body *string) (Annotation, error) {
	annotation, err := s.GetAnnotation(ctx, id)
	if err != nil {
		return Annotation{}, err
	}
	if status != nil {
		annotation.Status = *status
	}
	if body != nil {
		annotation.Body = *body
	}
	annotation.UpdatedAt = now()
	_, err = s.db.ExecContext(ctx, `
		UPDATE annotations SET status = ?, body = ?, updated_at = ? WHERE id = ?
	`, annotation.Status, annotation.Body, annotation.UpdatedAt, id)
	if err != nil {
		return Annotation{}, err
	}
	return annotation, nil
}

func (s *Store) searchFTS(ctx context.Context, expr string) ([]SearchResult, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT ts.document_id, d.title, ts.page_id, p.page_no, ts.text_version_id,
		       snippet(text_search, 4, '', '', '...', 18)
		FROM text_search ts
		JOIN documents d ON d.id = ts.document_id
		JOIN pages p ON p.id = ts.page_id
		WHERE text_search MATCH ?
		ORDER BY rank
		LIMIT 50
	`, expr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSearchResults(rows)
}

func (s *Store) searchLike(ctx context.Context, query string) ([]SearchResult, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT v.document_id, d.title, v.page_id, p.page_no, v.id,
		       substr(v.text, max(instr(v.text, ?) - 18, 1), 80)
		FROM text_versions v
		JOIN documents d ON d.id = v.document_id
		JOIN pages p ON p.id = v.page_id
		WHERE v.text LIKE '%' || ? || '%'
		ORDER BY v.created_at DESC
		LIMIT 50
	`, query, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSearchResults(rows)
}

func scanPageDetail(scanner interface{ Scan(...any) error }) (PageDetail, error) {
	var detail PageDetail
	var imageAsset, thumbAsset, lastProvider, lastModel, lastRecognizedAt sql.NullString
	var confidence sql.NullFloat64
	var hasCandidate, hasManual, hasFinal int
	err := scanner.Scan(
		&detail.PageID, &detail.DocumentID, &detail.PageNo, &detail.PageStatus, &detail.Width, &detail.Height,
		&imageAsset, &thumbAsset, &detail.RecognitionCount, &confidence, &lastProvider, &lastModel, &lastRecognizedAt,
		&hasCandidate, &hasManual, &hasFinal, &detail.UpdatedAt,
	)
	if confidence.Valid {
		detail.BestConfidence = &confidence.Float64
	}
	detail.ImageAssetID = nullString(imageAsset)
	detail.ThumbAssetID = nullString(thumbAsset)
	detail.LastProvider = nullString(lastProvider)
	detail.LastModel = nullString(lastModel)
	detail.LastRecognizedAt = nullString(lastRecognizedAt)
	detail.HasCandidate = hasCandidate != 0
	detail.HasManual = hasManual != 0
	detail.HasFinal = hasFinal != 0
	return detail, err
}

func scanRecognitionRun(scanner interface{ Scan(...any) error }) (RecognitionRun, error) {
	var run RecognitionRun
	var startedAt, finishedAt sql.NullString
	err := scanner.Scan(&run.ID, &run.DocumentID, &run.Provider, &run.Model, &run.PromptVersion, &run.ConfigJSON, &run.Status, &startedAt, &finishedAt, &run.CreatedAt)
	run.StartedAt = nullString(startedAt)
	run.FinishedAt = nullString(finishedAt)
	return run, err
}

func scanTextVersion(scanner interface{ Scan(...any) error }) (TextVersion, error) {
	var version TextVersion
	var pageID, baseVersionID, sourceResultID sql.NullString
	err := scanner.Scan(&version.ID, &version.DocumentID, &pageID, &version.Kind, &baseVersionID, &sourceResultID, &version.Text, &version.Status, &version.CreatedBy, &version.CreatedAt)
	version.PageID = nullString(pageID)
	version.BaseVersionID = nullString(baseVersionID)
	version.SourceResultID = nullString(sourceResultID)
	return version, err
}

func scanJob(scanner interface{ Scan(...any) error }) (Job, error) {
	var job Job
	var startedAt, finishedAt sql.NullString
	err := scanner.Scan(&job.ID, &job.Type, &job.Status, &job.TargetType, &job.TargetID, &job.PayloadJSON, &job.Attempts, &job.MaxAttempts, &job.LastError, &job.CreatedAt, &startedAt, &finishedAt)
	job.StartedAt = nullString(startedAt)
	job.FinishedAt = nullString(finishedAt)
	return job, err
}

func scanAnnotation(scanner interface{ Scan(...any) error }) (Annotation, error) {
	var annotation Annotation
	var pageID, textVersionID sql.NullString
	err := scanner.Scan(
		&annotation.ID, &annotation.DocumentID, &pageID, &textVersionID, &annotation.Kind, &annotation.Status,
		&annotation.Body, &annotation.AnchorJSON, &annotation.CreatedAt, &annotation.UpdatedAt,
	)
	annotation.PageID = nullString(pageID)
	annotation.TextVersionID = nullString(textVersionID)
	return annotation, err
}

func scanSearchResults(rows *sql.Rows) ([]SearchResult, error) {
	results := []SearchResult{}
	for rows.Next() {
		var result SearchResult
		if err := rows.Scan(&result.DocumentID, &result.DocumentTitle, &result.PageID, &result.PageNo, &result.TextVersionID, &result.Snippet); err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, rows.Err()
}

func shouldIndexKind(kind string) bool {
	return kind == "candidate" || kind == "manual" || kind == "final"
}

func nullString(value sql.NullString) string {
	if value.Valid {
		return value.String
	}
	return ""
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func quoteFTS(query string) string {
	return fmt.Sprintf("\"%s\"", strings.ReplaceAll(query, `"`, `""`))
}

func runeLen(value string) int {
	return len([]rune(value))
}

func decodeJobPayload(payload string) map[string]string {
	values := map[string]string{}
	_ = json.Unmarshal([]byte(payload), &values)
	return values
}
