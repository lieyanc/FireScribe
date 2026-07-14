package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/lieyan/firescribe/internal/storage"
)

type Store struct {
	db         *sql.DB
	secretMu   sync.RWMutex
	secretPath string
	secrets    storedSecrets
}

type DocumentFilter struct {
	Query  string
	Status string
	Tag    string
}

var ErrPromptVersionExists = errors.New("prompt version with the same content already exists")

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) ListPromptVersions(ctx context.Context) ([]PromptVersion, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, version, content, sha256, is_active, created_at, COALESCE(activated_at, '')
		FROM prompt_versions
		ORDER BY is_active DESC, created_at DESC, id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	versions := []PromptVersion{}
	for rows.Next() {
		var item PromptVersion
		if err := rows.Scan(&item.ID, &item.Version, &item.Content, &item.SHA256, &item.IsActive, &item.CreatedAt, &item.ActivatedAt); err != nil {
			return nil, err
		}
		versions = append(versions, item)
	}
	return versions, rows.Err()
}

func (s *Store) GetPromptVersion(ctx context.Context, id string) (PromptVersion, error) {
	var item PromptVersion
	err := s.db.QueryRowContext(ctx, `
		SELECT id, version, content, sha256, is_active, created_at, COALESCE(activated_at, '')
		FROM prompt_versions WHERE id = ?
	`, id).Scan(&item.ID, &item.Version, &item.Content, &item.SHA256, &item.IsActive, &item.CreatedAt, &item.ActivatedAt)
	return item, err
}

func (s *Store) CreatePromptVersion(ctx context.Context, version, content, sha256 string) (PromptVersion, error) {
	var existingID string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM prompt_versions WHERE version = ? AND sha256 = ?`, version, sha256).Scan(&existingID)
	if err == nil {
		return PromptVersion{}, ErrPromptVersionExists
	}
	if err != sql.ErrNoRows {
		return PromptVersion{}, err
	}
	item := PromptVersion{
		ID:        newID("prompt"),
		Version:   version,
		Content:   content,
		SHA256:    sha256,
		CreatedAt: now(),
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO prompt_versions(id, version, content, sha256, is_active, created_at)
		VALUES (?, ?, ?, ?, 0, ?)
	`, item.ID, item.Version, item.Content, item.SHA256, item.CreatedAt)
	return item, err
}

// EnsureActivePromptVersion imports a prompt file/config pair into the
// library when necessary, then makes that exact immutable snapshot active.
func (s *Store) EnsureActivePromptVersion(ctx context.Context, version, content, sha256 string) (PromptVersion, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PromptVersion{}, err
	}
	defer tx.Rollback()

	var item PromptVersion
	err = tx.QueryRowContext(ctx, `
		SELECT id, version, content, sha256, is_active, created_at, COALESCE(activated_at, '')
		FROM prompt_versions WHERE version = ? AND sha256 = ?
	`, version, sha256).Scan(&item.ID, &item.Version, &item.Content, &item.SHA256, &item.IsActive, &item.CreatedAt, &item.ActivatedAt)
	if err == sql.ErrNoRows {
		item = PromptVersion{
			ID:        newID("prompt"),
			Version:   version,
			Content:   content,
			SHA256:    sha256,
			CreatedAt: now(),
		}
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO prompt_versions(id, version, content, sha256, is_active, created_at)
			VALUES (?, ?, ?, ?, 0, ?)
		`, item.ID, item.Version, item.Content, item.SHA256, item.CreatedAt); err != nil {
			return PromptVersion{}, err
		}
	} else if err != nil {
		return PromptVersion{}, err
	}
	if item.IsActive {
		if err = tx.Commit(); err != nil {
			return PromptVersion{}, err
		}
		return item, nil
	}

	activatedAt := now()
	if _, err = tx.ExecContext(ctx, `UPDATE prompt_versions SET is_active = 0 WHERE is_active = 1 AND id <> ?`, item.ID); err != nil {
		return PromptVersion{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE prompt_versions SET is_active = 1, activated_at = ? WHERE id = ?`, activatedAt, item.ID); err != nil {
		return PromptVersion{}, err
	}
	if err = tx.Commit(); err != nil {
		return PromptVersion{}, err
	}
	item.IsActive = true
	item.ActivatedAt = activatedAt
	return item, nil
}

func (s *Store) ActivatePromptVersion(ctx context.Context, id string) (PromptVersion, error) {
	item, err := s.GetPromptVersion(ctx, id)
	if err != nil {
		return PromptVersion{}, err
	}
	return s.EnsureActivePromptVersion(ctx, item.Version, item.Content, item.SHA256)
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
	tag := strings.TrimSpace(filter.Tag)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, title, description, author, source, status, page_count, created_at, updated_at
		FROM documents
		WHERE (? = '' OR status = ?)
		  AND (? = '' OR title LIKE '%' || ? || '%' OR author LIKE '%' || ? || '%' OR source LIKE '%' || ? || '%')
		  AND (? = '' OR EXISTS (
		    SELECT 1
		    FROM document_tags dt
		    JOIN tags t ON t.id = dt.tag_id
		    WHERE dt.document_id = documents.id AND t.name = ? COLLATE NOCASE
		  ))
		ORDER BY updated_at DESC
	`, status, status, query, query, query, query, tag, tag)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return s.attachDocumentTags(ctx, docs)
}

func (s *Store) GetDocument(ctx context.Context, id string) (Document, error) {
	var doc Document
	err := s.db.QueryRowContext(ctx, `
		SELECT id, title, description, author, source, status, page_count, created_at, updated_at
		FROM documents WHERE id = ?
	`, id).Scan(&doc.ID, &doc.Title, &doc.Description, &doc.Author, &doc.Source, &doc.Status, &doc.PageCount, &doc.CreatedAt, &doc.UpdatedAt)
	if err != nil {
		return doc, err
	}
	doc.Tags, err = s.ListDocumentTags(ctx, id)
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Document{}, err
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE documents
		SET title = ?, description = ?, author = ?, source = ?, status = ?, updated_at = ?
		WHERE id = ?
	`, doc.Title, doc.Description, doc.Author, doc.Source, doc.Status, doc.UpdatedAt, id); err != nil {
		_ = tx.Rollback()
		return Document{}, err
	}
	if title != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE text_search SET title = ? WHERE document_id = ?`, doc.Title, id); err != nil {
			_ = tx.Rollback()
			return Document{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Document{}, err
	}
	doc.Tags, err = s.ListDocumentTags(ctx, id)
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

func (s *Store) ResetDocumentImport(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM pages WHERE document_id = ?`, id); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM document_assets WHERE document_id = ? AND role IN ('page_image', 'thumbnail')`, id); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE documents SET status = 'importing', page_count = 0, updated_at = ? WHERE id = ?`, now(), id); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteDocument(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM text_search WHERE document_id = ?`, id); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM documents WHERE id = ?`, id); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
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

func (s *Store) ListDocumentAssets(ctx context.Context, documentID string) ([]DocumentAsset, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id, a.kind, da.role, a.sha256, a.original_name, a.mime_type, a.byte_size, a.storage_path, a.created_at
		FROM document_assets da
		JOIN assets a ON a.id = da.asset_id
		WHERE da.document_id = ?
		ORDER BY CASE da.role
			WHEN 'original' THEN 0
			WHEN 'page_image' THEN 1
			WHEN 'thumbnail' THEN 2
			WHEN 'export' THEN 3
			ELSE 4
		END, a.created_at DESC
	`, documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	assets := []DocumentAsset{}
	for rows.Next() {
		var asset DocumentAsset
		if err := rows.Scan(
			&asset.ID, &asset.Kind, &asset.Role, &asset.SHA256, &asset.OriginalName,
			&asset.MimeType, &asset.ByteSize, &asset.StoragePath, &asset.CreatedAt,
		); err != nil {
			return nil, err
		}
		assets = append(assets, asset)
	}
	return assets, rows.Err()
}

func (s *Store) ListTags(ctx context.Context) ([]Tag, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, color FROM tags ORDER BY lower(name), name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tags := []Tag{}
	for rows.Next() {
		var tag Tag
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.Color); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

func (s *Store) ListDocumentTags(ctx context.Context, documentID string) ([]Tag, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.name, t.color
		FROM document_tags dt
		JOIN tags t ON t.id = dt.tag_id
		WHERE dt.document_id = ?
		ORDER BY lower(t.name), t.name
	`, documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tags := []Tag{}
	for rows.Next() {
		var tag Tag
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.Color); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

func (s *Store) SetDocumentTags(ctx context.Context, documentID string, names []string) ([]Tag, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	tagIDs := make([]string, 0, len(names))
	for _, rawName := range names {
		name := strings.TrimSpace(rawName)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		var tagID string
		err := tx.QueryRowContext(ctx, `SELECT id FROM tags WHERE name = ? COLLATE NOCASE`, name).Scan(&tagID)
		if errors.Is(err, sql.ErrNoRows) {
			tagID = newID("tag")
			_, err = tx.ExecContext(ctx, `INSERT INTO tags(id, name, color) VALUES (?, ?, '')`, tagID, name)
		}
		if err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		tagIDs = append(tagIDs, tagID)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM document_tags WHERE document_id = ?`, documentID); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	for _, tagID := range tagIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO document_tags(document_id, tag_id) VALUES (?, ?)
		`, documentID, tagID); err != nil {
			_ = tx.Rollback()
			return nil, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE documents SET updated_at = ? WHERE id = ?`, now(), documentID); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.ListDocumentTags(ctx, documentID)
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
	return s.CreateRecognitionRunForVariant(ctx, run, "")
}

func (s *Store) CreateRecognitionRunForVariant(ctx context.Context, run RecognitionRun, variantID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO recognition_runs(id, document_id, provider, model, prompt_version, config_json,
			recognizer_profile_id, recognizer_driver, profile_snapshot_json, provider_adapter_id, input_source, status,
			total_pages, done_pages, failed_pages, error, started_at, finished_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?)
	`, run.ID, run.DocumentID, run.Provider, run.Model, run.PromptVersion, run.ConfigJSON,
		run.ProfileID, run.Driver, run.ProfileSnapshotJSON, run.ProviderAdapterID, run.InputSource, run.Status,
		run.TotalPages, run.DonePages, run.FailedPages, run.Error, run.StartedAt, run.FinishedAt, run.CreatedAt)
	if err != nil {
		return err
	}
	if strings.TrimSpace(variantID) != "" {
		var raw, currentRaw string
		if err := tx.QueryRowContext(ctx, `SELECT run_ids_json, current_run_ids_json FROM recognition_experiment_variants WHERE id = ?`, variantID).Scan(&raw, &currentRaw); err != nil {
			return err
		}
		var runIDs []string
		_ = json.Unmarshal([]byte(raw), &runIDs)
		runIDs = appendUniqueString(runIDs, run.ID)
		var currentRunIDs []string
		_ = json.Unmarshal([]byte(currentRaw), &currentRunIDs)
		currentRunIDs = appendUniqueString(currentRunIDs, run.ID)
		updated, _ := json.Marshal(runIDs)
		currentUpdated, _ := json.Marshal(currentRunIDs)
		if _, err := tx.ExecContext(ctx, `UPDATE recognition_experiment_variants SET run_ids_json = ?, current_run_ids_json = ? WHERE id = ?`, string(updated), string(currentUpdated), variantID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListRecognitionRuns(ctx context.Context, documentID string) ([]RecognitionRun, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, document_id, provider, model, prompt_version, config_json,
		       COALESCE(recognizer_profile_id, ''), recognizer_driver, profile_snapshot_json,
		       COALESCE(provider_adapter_id, ''), input_source, status,
		       total_pages, done_pages, failed_pages, error, started_at, finished_at, created_at
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
		SELECT id, document_id, provider, model, prompt_version, config_json,
		       COALESCE(recognizer_profile_id, ''), recognizer_driver, profile_snapshot_json,
		       COALESCE(provider_adapter_id, ''), input_source, status,
		       total_pages, done_pages, failed_pages, error, started_at, finished_at, created_at
		FROM recognition_runs WHERE id = ?
	`, id)
	return scanRecognitionRun(row)
}

// ActiveRecognitionRun returns the queued/running run for a document, if any.
func (s *Store) ActiveRecognitionRun(ctx context.Context, documentID string) (RecognitionRun, bool, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, document_id, provider, model, prompt_version, config_json,
		       COALESCE(recognizer_profile_id, ''), recognizer_driver, profile_snapshot_json,
		       COALESCE(provider_adapter_id, ''), input_source, status,
		       total_pages, done_pages, failed_pages, error, started_at, finished_at, created_at
		FROM recognition_runs
		WHERE document_id = ? AND status IN ('queued', 'running')
		ORDER BY created_at DESC LIMIT 1
	`, documentID)
	run, err := scanRecognitionRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return RecognitionRun{}, false, nil
	}
	if err != nil {
		return RecognitionRun{}, false, err
	}
	return run, true, nil
}

func (s *Store) UpdateRecognitionRunStatus(ctx context.Context, id, status, startedAt, finishedAt string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE recognition_runs
		SET status = ?, started_at = COALESCE(NULLIF(?, ''), started_at), finished_at = COALESCE(NULLIF(?, ''), finished_at)
		WHERE id = ?
	`, status, startedAt, finishedAt, id)
	return err
}

// FinishRecognitionRun records the terminal state of a run.
func (s *Store) FinishRecognitionRun(ctx context.Context, id, status, errMsg string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE recognition_runs
		SET status = ?, error = ?, finished_at = COALESCE(NULLIF(finished_at, ''), ?)
		WHERE id = ?
	`, status, errMsg, now(), id)
	return err
}

func (s *Store) CreateRunPages(ctx context.Context, runID string, pages []Page) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, page := range pages {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO run_pages(run_id, page_id, page_no, status) VALUES (?, ?, ?, 'pending')
		`, runID, page.ID, page.PageNo); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListRunPages(ctx context.Context, runID string) ([]RunPage, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT run_id, page_id, page_no, status, attempts, error, started_at, finished_at
		FROM run_pages WHERE run_id = ? ORDER BY page_no
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	pages := []RunPage{}
	for rows.Next() {
		var page RunPage
		var startedAt, finishedAt sql.NullString
		if err := rows.Scan(&page.RunID, &page.PageID, &page.PageNo, &page.Status, &page.Attempts, &page.Error, &startedAt, &finishedAt); err != nil {
			return nil, err
		}
		page.StartedAt = nullString(startedAt)
		page.FinishedAt = nullString(finishedAt)
		pages = append(pages, page)
	}
	return pages, rows.Err()
}

func (s *Store) MarkRunPageRunning(ctx context.Context, runID, pageID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE run_pages
		SET status = 'running', attempts = attempts + 1, started_at = ?
		WHERE run_id = ? AND page_id = ?
	`, now(), runID, pageID)
	return err
}

// MarkRunPageFinished stores a page's terminal status and folds it into the
// run's progress counters so polling clients see incremental progress.
func (s *Store) MarkRunPageFinished(ctx context.Context, runID, pageID, status, errMsg string) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE run_pages SET status = ?, error = ?, finished_at = ? WHERE run_id = ? AND page_id = ?
	`, status, errMsg, now(), runID, pageID); err != nil {
		return err
	}
	failed := 0
	if status == "failed" {
		failed = 1
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE recognition_runs SET done_pages = done_pages + 1, failed_pages = failed_pages + ? WHERE id = ?
	`, failed, runID)
	return err
}

// CancelPendingRunPages marks every unfinished page of a run as canceled and
// returns how many pages were affected.
func (s *Store) CancelPendingRunPages(ctx context.Context, runID, message string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE run_pages SET status = 'canceled', error = ?, finished_at = ?
		WHERE run_id = ? AND status IN ('pending', 'running')
	`, message, now(), runID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) RunPageStatusCounts(ctx context.Context, runID string) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT status, COUNT(*) FROM run_pages WHERE run_id = ? GROUP BY status
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := map[string]int{}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		counts[status] = count
	}
	return counts, rows.Err()
}

// RecomputeDocumentStatus derives a document's status from its pages and text
// versions. It never touches documents that are importing or failed imports.
func (s *Store) RecomputeDocumentStatus(ctx context.Context, documentID string) error {
	doc, err := s.GetDocument(ctx, documentID)
	if err != nil {
		return err
	}
	switch doc.Status {
	case "importing", "failed":
		return nil
	}

	var totalPages int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pages WHERE document_id = ?`, documentID).Scan(&totalPages); err != nil {
		return err
	}
	status := "ready"
	if totalPages > 0 {
		var finalizedPages int
		if err := s.db.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM effective_text_versions
			WHERE document_id = ? AND kind = 'final'
		`, documentID).Scan(&finalizedPages); err != nil {
			return err
		}
		var manualPages int
		if err := s.db.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM effective_text_versions
			WHERE document_id = ? AND kind = 'manual'
		`, documentID).Scan(&manualPages); err != nil {
			return err
		}
		var pendingPages int
		if err := s.db.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM pages p
			LEFT JOIN effective_text_versions ev ON ev.page_id = p.id
			WHERE p.document_id = ?
			  AND (
			    ev.kind IN ('candidate', 'raw_selected')
			    OR (ev.id IS NULL AND EXISTS(
			      SELECT 1 FROM recognition_results r WHERE r.page_id = p.id
			    ))
			  )
		`, documentID).Scan(&pendingPages); err != nil {
			return err
		}
		switch {
		case finalizedPages == totalPages:
			status = "finalized"
		case manualPages > 0 || finalizedPages > 0:
			status = "reviewing"
		case pendingPages > 0:
			status = "review_pending"
		}
	}
	if status == doc.Status {
		return nil
	}
	return s.UpdateDocumentStatus(ctx, documentID, status)
}

func (s *Store) CreateRecognitionResult(ctx context.Context, result RecognitionResult) error {
	if strings.TrimSpace(result.MetadataJSON) == "" {
		result.MetadataJSON = "{}"
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO recognition_results(id, run_id, page_id, text, confidence, raw_json, metadata_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, result.ID, result.RunID, result.PageID, result.Text, result.Confidence, result.RawJSON, result.MetadataJSON, result.CreatedAt)
	return err
}

func (s *Store) ListRecognitionResults(ctx context.Context, pageID string) ([]RecognitionResult, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.id, r.run_id, r.page_id, r.text, r.confidence, r.raw_json, r.metadata_json, r.created_at,
		       run.provider, run.model, run.prompt_version, run.config_json
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
		if err := rows.Scan(
			&result.ID, &result.RunID, &result.PageID, &result.Text, &confidence, &result.RawJSON, &result.MetadataJSON,
			&result.CreatedAt, &result.Provider, &result.Model, &result.PromptVersion, &result.ConfigJSON,
		); err != nil {
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

func (s *Store) GetTextVersion(ctx context.Context, id string) (TextVersion, error) {
	return scanTextVersion(s.db.QueryRowContext(ctx, `
		SELECT id, document_id, page_id, kind, base_version_id, source_result_id, text, status, created_by, created_at
		FROM text_versions WHERE id = ?
	`, id))
}

func (s *Store) LatestTextForPage(ctx context.Context, pageID string) (string, string, error) {
	var id, text string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, text
		FROM effective_text_versions
		WHERE page_id = ?
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

func (s *Store) EffectiveTextForPage(ctx context.Context, pageID string) (TextVersion, bool, error) {
	version, err := scanTextVersion(s.db.QueryRowContext(ctx, `
		SELECT id, document_id, page_id, kind, base_version_id, source_result_id, text, status, created_by, created_at
		FROM effective_text_versions WHERE page_id = ?
	`, pageID))
	if err == nil {
		return version, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return TextVersion{}, false, err
	}
	var result RecognitionResult
	var confidence sql.NullFloat64
	err = s.db.QueryRowContext(ctx, `
		SELECT r.id, r.run_id, r.page_id, r.text, r.confidence, r.raw_json, r.metadata_json, r.created_at,
		       run.provider, run.model, run.prompt_version, run.config_json
		FROM recognition_results r
		JOIN recognition_runs run ON run.id = r.run_id
		WHERE r.page_id = ? ORDER BY r.created_at DESC, r.rowid DESC LIMIT 1
	`, pageID).Scan(
		&result.ID, &result.RunID, &result.PageID, &result.Text, &confidence, &result.RawJSON, &result.MetadataJSON,
		&result.CreatedAt, &result.Provider, &result.Model, &result.PromptVersion, &result.ConfigJSON,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return TextVersion{}, false, nil
	}
	if err != nil {
		return TextVersion{}, false, err
	}
	return TextVersion{ID: result.ID, PageID: pageID, Kind: "raw_selected", Text: result.Text, Status: "draft", CreatedBy: "recognizer", CreatedAt: result.CreatedAt}, true, nil
}

func (s *Store) LatestFinalTextForPage(ctx context.Context, pageID string) (TextVersion, bool, error) {
	version, err := scanTextVersion(s.db.QueryRowContext(ctx, `
		SELECT id, document_id, page_id, kind, base_version_id, source_result_id, text, status, created_by, created_at
		FROM text_versions
		WHERE page_id = ? AND kind = 'final'
		ORDER BY julianday(created_at) DESC, created_at DESC, rowid DESC
		LIMIT 1
	`, pageID))
	if errors.Is(err, sql.ErrNoRows) {
		return TextVersion{}, false, nil
	}
	return version, err == nil, err
}

func (s *Store) IndexTextVersion(ctx context.Context, version TextVersion) error {
	current, err := scanTextVersion(s.db.QueryRowContext(ctx, `
		SELECT id, document_id, page_id, kind, base_version_id, source_result_id, text, status, created_by, created_at
		FROM effective_text_versions
		WHERE page_id = ?
	`, version.PageID))
	if err != nil {
		return err
	}
	doc, err := s.GetDocument(ctx, current.DocumentID)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM text_search WHERE page_id = ?`, current.PageID); err != nil {
		_ = tx.Rollback()
		return err
	}
	if !shouldIndexKind(current.Kind) {
		return tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO text_search(document_id, page_id, text_version_id, title, body)
		VALUES (?, ?, ?, ?, ?)
	`, current.DocumentID, current.PageID, current.ID, doc.Title, current.Text); err != nil {
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

func (s *Store) RebuildSearchIndex(ctx context.Context) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM text_search`); err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO text_search(document_id, page_id, text_version_id, title, body)
		SELECT ev.document_id, ev.page_id, ev.id, d.title, ev.text
		FROM effective_text_versions ev
		JOIN documents d ON d.id = ev.document_id
		WHERE ev.kind IN ('candidate', 'manual', 'final')
	`)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	count, err := result.RowsAffected()
	return int(count), err
}

func (s *Store) CreateJob(ctx context.Context, job Job) error {
	if strings.TrimSpace(job.PayloadJSON) == "" {
		job.PayloadJSON = "{}"
	}
	if strings.TrimSpace(job.ResultJSON) == "" {
		job.ResultJSON = "{}"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO jobs(
			id, type, status, target_type, target_id, payload_json, attempts, max_attempts, last_error,
			progress_current, progress_total, progress_message, result_json, created_at, started_at, finished_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''))
	`, job.ID, job.Type, job.Status, job.TargetType, job.TargetID, job.PayloadJSON, job.Attempts, job.MaxAttempts, job.LastError,
		job.ProgressCurrent, job.ProgressTotal, job.ProgressMessage, job.ResultJSON, job.CreatedAt, job.StartedAt, job.FinishedAt)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO job_events(id, job_id, attempt, level, stage, message, data_json, created_at)
		VALUES (?, ?, ?, 'info', 'queued', '任务已进入队列', '{}', ?)
	`, newID("evt"), job.ID, job.Attempts, now()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) appendJobEvent(ctx context.Context, jobID string, attempt int, level, stage, message, dataJSON string) error {
	if strings.TrimSpace(dataJSON) == "" {
		dataJSON = "{}"
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO job_events(id, job_id, attempt, level, stage, message, data_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, newID("evt"), jobID, attempt, level, stage, message, dataJSON, now())
	return err
}

func appendJobEventTx(ctx context.Context, tx *sql.Tx, jobID string, attempt int, level, stage, message, dataJSON string) error {
	if strings.TrimSpace(dataJSON) == "" {
		dataJSON = "{}"
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO job_events(id, job_id, attempt, level, stage, message, data_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, newID("evt"), jobID, attempt, level, stage, message, dataJSON, now())
	return err
}

func (s *Store) ListJobEvents(ctx context.Context, jobID string) ([]JobEvent, error) {
	if _, err := s.GetJob(ctx, jobID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, job_id, attempt, level, stage, message, data_json, created_at
		FROM job_events WHERE job_id = ? ORDER BY created_at, id
	`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []JobEvent{}
	for rows.Next() {
		var event JobEvent
		if err := rows.Scan(&event.ID, &event.JobID, &event.Attempt, &event.Level, &event.Stage, &event.Message, &event.DataJSON, &event.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Store) ListJobs(ctx context.Context) ([]Job, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, status, target_type, target_id, payload_json, attempts, max_attempts, last_error,
		       progress_current, progress_total, progress_message, result_json, created_at, started_at, finished_at
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
		SELECT id, type, status, target_type, target_id, payload_json, attempts, max_attempts, last_error,
		       progress_current, progress_total, progress_message, result_json, created_at, started_at, finished_at
		FROM jobs WHERE id = ?
	`, id)
	return scanJob(row)
}

func (s *Store) MarkJobRunning(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE jobs
		SET status = 'running', attempts = attempts + 1, started_at = ?, finished_at = NULL, last_error = ''
		WHERE id = ? AND status = 'queued'
	`, now(), id)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return fmt.Errorf("job %s is not queued", id)
	}
	var attempt int
	if err := tx.QueryRowContext(ctx, `SELECT attempts FROM jobs WHERE id = ?`, id).Scan(&attempt); err != nil {
		return err
	}
	if err := appendJobEventTx(ctx, tx, id, attempt, "info", "attempt_started", fmt.Sprintf("开始第 %d 次尝试", attempt), "{}"); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) MarkJobDone(ctx context.Context, id string) error {
	return s.MarkJobDoneWithResult(ctx, id, "{}")
}

func (s *Store) MarkJobDoneWithResult(ctx context.Context, id, resultJSON string) error {
	if strings.TrimSpace(resultJSON) == "" {
		resultJSON = "{}"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE jobs SET status = 'succeeded', finished_at = ?, last_error = '', result_json = ?,
		progress_current = CASE WHEN progress_total > 0 THEN progress_total ELSE progress_current END
		WHERE id = ? AND status = 'running'
	`, now(), resultJSON, id)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return nil
	}
	var attempt int
	if err := tx.QueryRowContext(ctx, `SELECT attempts FROM jobs WHERE id = ?`, id).Scan(&attempt); err != nil {
		return err
	}
	if err := appendJobEventTx(ctx, tx, id, attempt, "info", "succeeded", "任务执行成功", resultJSON); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) MarkJobFailed(ctx context.Context, id string, cause error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE jobs SET status = 'failed', finished_at = ?, last_error = ? WHERE id = ?`, now(), errorString(cause), id)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return nil
	}
	var attempt int
	if err := tx.QueryRowContext(ctx, `SELECT attempts FROM jobs WHERE id = ?`, id).Scan(&attempt); err != nil {
		return err
	}
	if err := appendJobEventTx(ctx, tx, id, attempt, "error", "failed", errorString(cause), "{}"); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) MarkJobCanceled(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE jobs SET status = 'canceled', finished_at = ?, progress_message = '已取消' WHERE id = ? AND status IN ('queued', 'running')`, now(), id)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return nil
	}
	var attempt int
	if err := tx.QueryRowContext(ctx, `SELECT attempts FROM jobs WHERE id = ?`, id).Scan(&attempt); err != nil {
		return err
	}
	if err := appendJobEventTx(ctx, tx, id, attempt, "warning", "canceled", "任务已取消", "{}"); err != nil {
		return err
	}
	return tx.Commit()
}

// ForceJobTerminal is an emergency fallback for workers whose normal terminal
// transition could not commit together with its job event. It deliberately
// writes no event: callers must try MarkJobDone/Failed/Canceled first so the
// ordinary state-and-event transition remains atomic.
func (s *Store) ForceJobTerminal(ctx context.Context, id, status, lastError, resultJSON string) error {
	if strings.TrimSpace(resultJSON) == "" {
		resultJSON = "{}"
	}
	timestamp := now()
	var (
		result sql.Result
		err    error
	)
	switch status {
	case "succeeded":
		result, err = s.db.ExecContext(ctx, `
			UPDATE jobs SET status = 'succeeded', finished_at = ?, last_error = '', result_json = ?,
			progress_current = CASE WHEN progress_total > 0 THEN progress_total ELSE progress_current END
			WHERE id = ? AND status = 'running'
		`, timestamp, resultJSON, id)
	case "failed":
		result, err = s.db.ExecContext(ctx, `
			UPDATE jobs SET status = 'failed', finished_at = ?, last_error = ?
			WHERE id = ? AND status = 'running'
		`, timestamp, lastError, id)
	case "canceled":
		result, err = s.db.ExecContext(ctx, `
			UPDATE jobs SET status = 'canceled', finished_at = ?, progress_message = '已取消'
			WHERE id = ? AND status = 'running'
		`, timestamp, id)
	default:
		return fmt.Errorf("invalid forced terminal job status %q", status)
	}
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		job, getErr := s.GetJob(ctx, id)
		if getErr != nil {
			return getErr
		}
		if job.Status != status {
			return fmt.Errorf("job %s is %s, cannot force terminal status %s", id, job.Status, status)
		}
	}
	return nil
}

func (s *Store) UpdateJobProgress(ctx context.Context, id string, current, total int, message string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE jobs SET progress_current = ?, progress_total = ?, progress_message = ?
		WHERE id = ? AND status = 'running'
	`, current, total, message, id)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return nil
	}
	var attempt int
	if err := tx.QueryRowContext(ctx, `SELECT attempts FROM jobs WHERE id = ?`, id).Scan(&attempt); err != nil {
		return err
	}
	data := fmt.Sprintf(`{"current":%d,"total":%d}`, current, total)
	if err := appendJobEventTx(ctx, tx, id, attempt, "info", "progress", message, data); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) UpdateJobResult(ctx context.Context, id, resultJSON string) error {
	if strings.TrimSpace(resultJSON) == "" {
		resultJSON = "{}"
	}
	_, err := s.db.ExecContext(ctx, `UPDATE jobs SET result_json = ? WHERE id = ?`, resultJSON, id)
	return err
}

func (s *Store) RequeueJob(ctx context.Context, id string) (Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE jobs
		SET status = 'queued', last_error = '', progress_current = 0, progress_message = '等待重试',
		    result_json = '{}', started_at = NULL, finished_at = NULL
		WHERE id = ? AND status = 'failed' AND attempts < max_attempts
	`, id)
	if err != nil {
		return Job{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return Job{}, err
	}
	if changed == 0 {
		return Job{}, fmt.Errorf("job cannot be retried (it may have reached max attempts)")
	}
	job, err := scanJob(tx.QueryRowContext(ctx, `
		SELECT id, type, status, target_type, target_id, payload_json, attempts, max_attempts, last_error,
		       progress_current, progress_total, progress_message, result_json, created_at, started_at, finished_at
		FROM jobs WHERE id = ?
	`, id))
	if err != nil {
		return Job{}, err
	}
	if err := appendJobEventTx(ctx, tx, id, job.Attempts, "info", "retry_queued", "任务等待重试", "{}"); err != nil {
		return Job{}, err
	}
	if err := tx.Commit(); err != nil {
		return Job{}, err
	}
	return job, nil
}

func (s *Store) HasActiveJobType(ctx context.Context, jobType string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE type = ? AND status IN ('queued', 'running')`, jobType).Scan(&count)
	return count > 0, err
}

func (s *Store) CreateExport(ctx context.Context, export ExportFile) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO exports(
			id, document_id, job_id, format, include_page_numbers, text_scope, include_annotations, include_uncertain,
			status, asset_id, last_error, created_at, finished_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, NULLIF(?, ''))
	`, export.ID, export.DocumentID, export.JobID, export.Format, export.IncludePageNumbers, export.TextScope,
		export.IncludeAnnotations, export.IncludeUncertain, export.Status, export.AssetID, export.LastError, export.CreatedAt, export.FinishedAt)
	return err
}

func (s *Store) MarkExportRunning(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE exports SET status = 'running', last_error = '', finished_at = NULL WHERE id = ? AND status = 'queued'`, id)
	return err
}

func (s *Store) RequeueExport(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE exports SET status = 'queued', asset_id = NULL, last_error = '', finished_at = NULL WHERE id = ?`, id)
	return err
}

func (s *Store) GetExport(ctx context.Context, id string) (ExportFile, error) {
	var export ExportFile
	var assetID, finishedAt sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT id, document_id, job_id, format, include_page_numbers, text_scope, include_annotations, include_uncertain,
		       status, asset_id, last_error, created_at, finished_at
		FROM exports WHERE id = ?
	`, id).Scan(&export.ID, &export.DocumentID, &export.JobID, &export.Format, &export.IncludePageNumbers,
		&export.TextScope, &export.IncludeAnnotations, &export.IncludeUncertain,
		&export.Status, &assetID, &export.LastError, &export.CreatedAt, &finishedAt)
	export.AssetID = nullString(assetID)
	export.FinishedAt = nullString(finishedAt)
	if err == nil && export.AssetID != "" {
		asset, assetErr := s.GetAsset(ctx, export.AssetID)
		if assetErr != nil {
			return ExportFile{}, assetErr
		}
		export.StoragePath = asset.StoragePath
		export.DownloadURL = "/api/exports/" + export.ID + "/download"
	}
	return export, err
}

func (s *Store) FinishExport(ctx context.Context, id, assetID string, cause error) error {
	status := "succeeded"
	lastError := ""
	if cause != nil {
		status = "failed"
		lastError = cause.Error()
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE exports SET status = ?, asset_id = NULLIF(?, ''), last_error = ?, finished_at = ? WHERE id = ?
	`, status, assetID, lastError, now(), id)
	return err
}

func (s *Store) CancelExport(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE exports SET status = 'canceled', last_error = 'canceled', finished_at = ?
		WHERE id = ? AND status IN ('queued', 'running')
	`, now(), id)
	return err
}

// CancelJobsForTarget cancels the queued/running jobs driving a target (e.g.
// a recognition run) when the run is finalized without its worker.
func (s *Store) CancelJobsForTarget(ctx context.Context, targetID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id, attempts FROM jobs WHERE target_id = ? AND status IN ('queued', 'running')`, targetID)
	if err != nil {
		return err
	}
	type targetJob struct {
		id      string
		attempt int
	}
	jobs := []targetJob{}
	for rows.Next() {
		var job targetJob
		if err := rows.Scan(&job.id, &job.attempt); err != nil {
			_ = rows.Close()
			return err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE jobs SET status = 'canceled', finished_at = ? WHERE target_id = ? AND status IN ('queued', 'running')`, now(), targetID); err != nil {
		return err
	}
	for _, job := range jobs {
		if err := appendJobEventTx(ctx, tx, job.id, job.attempt, "warning", "canceled", "任务已取消", "{}"); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// RecoverInterrupted cleans up state left behind by a previous process (crash
// or update restart): unfinished run pages, runs, and jobs become failed (so
// their pages are retryable), and documents stuck in transient statuses are
// recomputed from their data.
func (s *Store) RecoverInterrupted(ctx context.Context) (int64, error) {
	const cause = "interrupted by server restart"
	timestamp := now()

	if _, err := s.db.ExecContext(ctx, `
		UPDATE page_processing_results
		SET status = 'failed', last_error = ?, finished_at = ?
		WHERE status IN ('queued', 'running')
		  AND run_id IN (SELECT id FROM page_processing_runs WHERE status IN ('queued', 'running'))
	`, cause, timestamp); err != nil {
		return 0, err
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE page_processing_runs
		SET status = 'failed', last_error = ?, finished_at = ?,
		    done_pages = (SELECT COUNT(*) FROM page_processing_results r WHERE r.run_id = page_processing_runs.id AND r.status = 'succeeded'),
		    failed_pages = (SELECT COUNT(*) FROM page_processing_results r WHERE r.run_id = page_processing_runs.id AND r.status = 'failed')
		WHERE status IN ('queued', 'running')
	`, cause, timestamp); err != nil {
		return 0, err
	}

	if _, err := s.db.ExecContext(ctx, `
		UPDATE run_pages SET status = 'failed', error = ?, finished_at = ?
		WHERE status IN ('pending', 'running')
		  AND run_id IN (SELECT id FROM recognition_runs WHERE status IN ('queued', 'running'))
	`, cause, timestamp); err != nil {
		return 0, err
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE recognition_runs
		SET status = 'failed', error = ?, finished_at = ?,
		    done_pages = (SELECT COUNT(*) FROM run_pages rp WHERE rp.run_id = recognition_runs.id AND rp.status NOT IN ('pending', 'running')),
		    failed_pages = (SELECT COUNT(*) FROM run_pages rp WHERE rp.run_id = recognition_runs.id AND rp.status = 'failed')
		WHERE status IN ('queued', 'running')
	`, cause, timestamp); err != nil {
		return 0, err
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE recognition_experiment_variants
		SET status = 'failed', error = ?, finished_at = ?
		WHERE status IN ('queued', 'running')
		  AND experiment_id IN (SELECT id FROM recognition_experiments WHERE status IN ('queued', 'running'))
	`, cause, timestamp); err != nil {
		return 0, err
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE recognition_experiments
		SET status = 'failed', error = ?, finished_at = ?
		WHERE status IN ('queued', 'running')
	`, cause, timestamp); err != nil {
		return 0, err
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE cross_check_pages
		SET status = 'failed', error = ?
		WHERE status = 'pending'
		  AND cross_check_id IN (SELECT id FROM cross_checks WHERE status IN ('queued', 'running'))
	`, cause); err != nil {
		return 0, err
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE cross_check_variants
		SET status = 'failed', error = ?, finished_at = ?
		WHERE status IN ('queued', 'running')
		  AND cross_check_id IN (SELECT id FROM cross_checks WHERE status IN ('queued', 'running'))
	`, cause, timestamp); err != nil {
		return 0, err
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE cross_checks
		SET status = 'failed', error = ?, finished_at = ?,
		    consensus_pages = (SELECT COUNT(*) FROM cross_check_pages cp WHERE cp.cross_check_id = cross_checks.id AND cp.status = 'consensus'),
		    disagreement_pages = (SELECT COUNT(*) FROM cross_check_pages cp WHERE cp.cross_check_id = cross_checks.id AND cp.status = 'disagreement'),
		    failed_pages = (SELECT COUNT(*) FROM cross_check_pages cp WHERE cp.cross_check_id = cross_checks.id AND cp.status IN ('failed', 'canceled'))
		WHERE status IN ('queued', 'running')
	`, cause, timestamp); err != nil {
		return 0, err
	}

	jobTx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer jobTx.Rollback()
	jobRows, err := jobTx.QueryContext(ctx, `SELECT id, attempts FROM jobs WHERE status IN ('queued', 'running')`)
	if err != nil {
		return 0, err
	}
	type interruptedJob struct {
		id      string
		attempt int
	}
	interrupted := []interruptedJob{}
	for jobRows.Next() {
		var job interruptedJob
		if err := jobRows.Scan(&job.id, &job.attempt); err != nil {
			_ = jobRows.Close()
			return 0, err
		}
		interrupted = append(interrupted, job)
	}
	if err := jobRows.Close(); err != nil {
		return 0, err
	}
	res, err := jobTx.ExecContext(ctx, `
		UPDATE jobs SET status = 'failed', finished_at = ?, last_error = ?
		WHERE status IN ('queued', 'running')
	`, timestamp, cause)
	if err != nil {
		return 0, err
	}
	for _, job := range interrupted {
		if err := appendJobEventTx(ctx, jobTx, job.id, job.attempt, "error", "interrupted", cause, "{}"); err != nil {
			return 0, err
		}
	}
	if err := jobTx.Commit(); err != nil {
		return 0, err
	}
	recovered, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE exports SET status = 'failed', finished_at = ?, last_error = ?
		WHERE status IN ('queued', 'running')
	`, timestamp, cause); err != nil {
		return 0, err
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE project_exports SET status = 'failed', finished_at = ?, last_error = ?
		WHERE status IN ('queued', 'running')
	`, timestamp, cause); err != nil {
		return 0, err
	}

	if _, err := s.db.ExecContext(ctx, `
		UPDATE documents SET status = 'failed', updated_at = ? WHERE status = 'importing'
	`, timestamp); err != nil {
		return recovered, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM documents WHERE status = 'recognizing'`)
	if err != nil {
		return recovered, err
	}
	defer rows.Close()
	var stuck []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return recovered, err
		}
		stuck = append(stuck, id)
	}
	if err := rows.Err(); err != nil {
		return recovered, err
	}
	for _, id := range stuck {
		if _, err := s.db.ExecContext(ctx, `UPDATE documents SET status = 'ready', updated_at = ? WHERE id = ?`, timestamp, id); err != nil {
			return recovered, err
		}
		if err := s.RecomputeDocumentStatus(ctx, id); err != nil {
			return recovered, err
		}
	}
	return recovered, nil
}

func (s *Store) HasActiveJobs(ctx context.Context) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE status IN ('queued', 'running')`).Scan(&count)
	return count > 0, err
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
		FROM effective_text_versions v
		JOIN documents d ON d.id = v.document_id
		JOIN pages p ON p.id = v.page_id
		WHERE v.kind IN ('candidate', 'manual', 'final')
		  AND v.text LIKE '%' || ? || '%'
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
	err := scanner.Scan(&run.ID, &run.DocumentID, &run.Provider, &run.Model, &run.PromptVersion, &run.ConfigJSON,
		&run.ProfileID, &run.Driver, &run.ProfileSnapshotJSON, &run.ProviderAdapterID, &run.InputSource, &run.Status,
		&run.TotalPages, &run.DonePages, &run.FailedPages, &run.Error, &startedAt, &finishedAt, &run.CreatedAt)
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
	err := scanner.Scan(
		&job.ID, &job.Type, &job.Status, &job.TargetType, &job.TargetID, &job.PayloadJSON, &job.Attempts, &job.MaxAttempts,
		&job.LastError, &job.ProgressCurrent, &job.ProgressTotal, &job.ProgressMessage, &job.ResultJSON,
		&job.CreatedAt, &startedAt, &finishedAt,
	)
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

func (s *Store) attachDocumentTags(ctx context.Context, docs []Document) ([]Document, error) {
	for i := range docs {
		tags, err := s.ListDocumentTags(ctx, docs[i].ID)
		if err != nil {
			return nil, err
		}
		docs[i].Tags = tags
	}
	return docs, nil
}
