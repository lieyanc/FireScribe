package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func (s *Store) CreatePageProcessingRun(ctx context.Context, run PageProcessingRun, results []PageProcessingResult) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO page_processing_runs(id, document_id, job_id, config_json, status, total_pages,
			done_pages, failed_pages, last_error, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, run.ID, run.DocumentID, run.JobID, run.ConfigJSON, run.Status, run.TotalPages,
		run.DonePages, run.FailedPages, run.LastError, run.CreatedAt); err != nil {
		return err
	}
	for _, result := range results {
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO page_processing_results(id, run_id, page_id, source_asset_id, status,
				config_json, metadata_json, last_error, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, result.ID, result.RunID, result.PageID, result.SourceAssetID, result.Status,
			result.ConfigJSON, result.MetadataJSON, result.LastError, result.CreatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListPageProcessingRuns(ctx context.Context, documentID string) ([]PageProcessingRun, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, document_id, job_id, config_json, status, total_pages, done_pages,
		       failed_pages, last_error, created_at, started_at, finished_at
		FROM page_processing_runs WHERE document_id = ? ORDER BY created_at DESC
	`, documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []PageProcessingRun{}
	for rows.Next() {
		item, err := scanPageProcessingRun(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetPageProcessingRun(ctx context.Context, id string) (PageProcessingRun, error) {
	return scanPageProcessingRun(s.db.QueryRowContext(ctx, `
		SELECT id, document_id, job_id, config_json, status, total_pages, done_pages,
		       failed_pages, last_error, created_at, started_at, finished_at
		FROM page_processing_runs WHERE id = ?
	`, id))
}

func (s *Store) ActivePageProcessingRun(ctx context.Context, documentID string) (PageProcessingRun, bool, error) {
	run, err := scanPageProcessingRun(s.db.QueryRowContext(ctx, `
		SELECT id, document_id, job_id, config_json, status, total_pages, done_pages,
		       failed_pages, last_error, created_at, started_at, finished_at
		FROM page_processing_runs
		WHERE document_id = ? AND status IN ('queued', 'running')
		ORDER BY created_at DESC LIMIT 1
	`, documentID))
	if errors.Is(err, sql.ErrNoRows) {
		return PageProcessingRun{}, false, nil
	}
	return run, err == nil, err
}

func (s *Store) ListPageProcessingResults(ctx context.Context, runID string) ([]PageProcessingResult, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.id, r.run_id, r.page_id, p.page_no, r.source_asset_id, r.output_asset_id,
		       r.status, r.config_json, r.metadata_json, r.last_error, r.created_at,
		       r.started_at, r.finished_at
		FROM page_processing_results r
		JOIN pages p ON p.id = r.page_id
		WHERE r.run_id = ? ORDER BY p.page_no
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []PageProcessingResult{}
	for rows.Next() {
		item, err := scanPageProcessingResult(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) MarkPageProcessingRunRunning(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE page_processing_runs SET status = 'running', started_at = ?, finished_at = NULL,
			last_error = '' WHERE id = ?
	`, now(), id)
	return err
}

func (s *Store) MarkPageProcessingResultRunning(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE page_processing_results SET status = 'running', started_at = ?, finished_at = NULL,
			last_error = '' WHERE id = ?
	`, now(), id)
	return err
}

func (s *Store) FinishPageProcessingResult(ctx context.Context, id, outputAssetID, metadataJSON string, segments []PageSegment, cause error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	status, lastError := "succeeded", ""
	if cause != nil {
		status, lastError = "failed", cause.Error()
	}
	if strings.TrimSpace(metadataJSON) == "" {
		metadataJSON = "{}"
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE page_processing_results
		SET status = ?, output_asset_id = NULLIF(?, ''), metadata_json = ?, last_error = ?, finished_at = ?
		WHERE id = ?
	`, status, outputAssetID, metadataJSON, lastError, now(), id); err != nil {
		return err
	}
	if cause == nil {
		if _, err = tx.ExecContext(ctx, `DELETE FROM page_segments WHERE processing_result_id = ?`, id); err != nil {
			return err
		}
		for _, segment := range segments {
			if _, err = tx.ExecContext(ctx, `
				INSERT INTO page_segments(id, page_id, processing_result_id, kind, position,
					x, y, width, height, label, metadata_json, created_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			`, segment.ID, segment.PageID, id, segment.Kind, segment.Position,
				segment.X, segment.Y, segment.Width, segment.Height, segment.Label,
				segment.MetadataJSON, segment.CreatedAt); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (s *Store) CancelPendingPageProcessingResults(ctx context.Context, runID, reason string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE page_processing_results
		SET status = 'canceled', last_error = ?, finished_at = ?
		WHERE run_id = ? AND status IN ('queued', 'running')
	`, reason, now(), runID)
	return err
}

func (s *Store) ResetFailedPageProcessingResults(ctx context.Context, runID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE page_processing_results
		SET status = 'queued', output_asset_id = NULL, metadata_json = '{}', last_error = '',
			started_at = NULL, finished_at = NULL
		WHERE run_id = ? AND status <> 'succeeded'
	`, runID)
	return err
}

func (s *Store) RequeuePageProcessingRun(ctx context.Context, runID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `
		UPDATE page_processing_results
		SET status = 'queued', output_asset_id = NULL, metadata_json = '{}', last_error = '',
			started_at = NULL, finished_at = NULL
		WHERE run_id = ? AND status <> 'succeeded'
	`, runID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE page_processing_runs
		SET status = 'queued', done_pages = (
			SELECT COUNT(*) FROM page_processing_results WHERE run_id = ? AND status = 'succeeded'
		), failed_pages = 0, last_error = '', started_at = NULL, finished_at = NULL
		WHERE id = ?
	`, runID, runID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RequeuePageProcessingJob(ctx context.Context, jobID, runID string) (Job, error) {
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
	`, jobID)
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
	if _, err = tx.ExecContext(ctx, `
		UPDATE page_processing_results
		SET status = 'queued', output_asset_id = NULL, metadata_json = '{}', last_error = '',
			started_at = NULL, finished_at = NULL
		WHERE run_id = ? AND status <> 'succeeded'
	`, runID); err != nil {
		return Job{}, err
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE page_processing_runs
		SET status = 'queued', done_pages = (
			SELECT COUNT(*) FROM page_processing_results WHERE run_id = ? AND status = 'succeeded'
		), failed_pages = 0, last_error = '', started_at = NULL, finished_at = NULL
		WHERE id = ?
	`, runID, runID); err != nil {
		return Job{}, err
	}
	job, err := scanJob(tx.QueryRowContext(ctx, `
		SELECT id, type, status, target_type, target_id, payload_json, attempts, max_attempts, last_error,
		       progress_current, progress_total, progress_message, result_json, created_at, started_at, finished_at
		FROM jobs WHERE id = ?
	`, jobID))
	if err != nil {
		return Job{}, err
	}
	if err := appendJobEventTx(ctx, tx, jobID, job.Attempts, "info", "retry_queued", "任务等待重试", "{}"); err != nil {
		return Job{}, err
	}
	if err = tx.Commit(); err != nil {
		return Job{}, err
	}
	return job, nil
}

func (s *Store) FinishPageProcessingRun(ctx context.Context, id, status, lastError string, done, failed int) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE page_processing_runs
		SET status = ?, done_pages = ?, failed_pages = ?, last_error = ?, finished_at = ?
		WHERE id = ?
	`, status, done, failed, lastError, now(), id)
	return err
}

func (s *Store) UpdatePageProcessingRunProgress(ctx context.Context, id string, done, failed int) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE page_processing_runs SET done_pages = ?, failed_pages = ? WHERE id = ?
	`, done, failed, id)
	return err
}

func (s *Store) LatestEnhancedResult(ctx context.Context, pageID string) (PageProcessingResult, error) {
	return scanPageProcessingResult(s.db.QueryRowContext(ctx, `
		SELECT r.id, r.run_id, r.page_id, p.page_no, r.source_asset_id, r.output_asset_id,
		       r.status, r.config_json, r.metadata_json, r.last_error, r.created_at,
		       r.started_at, r.finished_at
		FROM page_processing_results r
		JOIN pages p ON p.id = r.page_id
		WHERE r.page_id = ? AND r.status = 'succeeded' AND r.output_asset_id IS NOT NULL
		ORDER BY r.finished_at DESC, r.created_at DESC LIMIT 1
	`, pageID))
}

func (s *Store) ListPageSegments(ctx context.Context, processingResultID string) ([]PageSegment, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, page_id, processing_result_id, kind, position, x, y, width, height,
		       label, metadata_json, created_at
		FROM page_segments WHERE processing_result_id = ? ORDER BY position, id
	`, processingResultID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []PageSegment{}
	for rows.Next() {
		var item PageSegment
		if err := rows.Scan(&item.ID, &item.PageID, &item.ProcessingResultID, &item.Kind,
			&item.Position, &item.X, &item.Y, &item.Width, &item.Height, &item.Label,
			&item.MetadataJSON, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanPageProcessingRun(scanner interface{ Scan(...any) error }) (PageProcessingRun, error) {
	var item PageProcessingRun
	var startedAt, finishedAt sql.NullString
	err := scanner.Scan(&item.ID, &item.DocumentID, &item.JobID, &item.ConfigJSON, &item.Status,
		&item.TotalPages, &item.DonePages, &item.FailedPages, &item.LastError, &item.CreatedAt,
		&startedAt, &finishedAt)
	item.StartedAt = nullString(startedAt)
	item.FinishedAt = nullString(finishedAt)
	return item, err
}

func scanPageProcessingResult(scanner interface{ Scan(...any) error }) (PageProcessingResult, error) {
	var item PageProcessingResult
	var outputAssetID, startedAt, finishedAt sql.NullString
	err := scanner.Scan(&item.ID, &item.RunID, &item.PageID, &item.PageNo, &item.SourceAssetID,
		&outputAssetID, &item.Status, &item.ConfigJSON, &item.MetadataJSON, &item.LastError,
		&item.CreatedAt, &startedAt, &finishedAt)
	item.OutputAssetID = nullString(outputAssetID)
	item.StartedAt = nullString(startedAt)
	item.FinishedAt = nullString(finishedAt)
	return item, err
}
