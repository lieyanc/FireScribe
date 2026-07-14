package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

func (s *Store) ListProviderAdapters(ctx context.Context) ([]ProviderAdapter, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, engine, endpoint, model, auth_type, secret, timeout_seconds,
		       request_config_json, response_config_json, is_enabled, created_at, updated_at
		FROM provider_adapters ORDER BY is_enabled DESC, updated_at DESC, name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ProviderAdapter{}
	for rows.Next() {
		item, err := scanProviderAdapter(rows)
		if err != nil {
			return nil, err
		}
		item.Secret = s.adapterSecret(item.ID, item.Secret)
		item.SecretSet = strings.TrimSpace(item.Secret) != ""
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetProviderAdapter(ctx context.Context, id string) (ProviderAdapter, error) {
	item, err := scanProviderAdapter(s.db.QueryRowContext(ctx, `
		SELECT id, name, engine, endpoint, model, auth_type, secret, timeout_seconds,
		       request_config_json, response_config_json, is_enabled, created_at, updated_at
		FROM provider_adapters WHERE id = ?
	`, id))
	if err == nil {
		item.Secret = s.adapterSecret(item.ID, item.Secret)
		item.SecretSet = strings.TrimSpace(item.Secret) != ""
	}
	return item, err
}

func (s *Store) SaveProviderAdapter(ctx context.Context, item ProviderAdapter) (ProviderAdapter, error) {
	databaseSecret, err := s.saveAdapterSecret(item.ID, item.Secret)
	if err != nil {
		return ProviderAdapter{}, err
	}
	if item.CreatedAt == "" {
		item.CreatedAt = now()
	}
	item.UpdatedAt = now()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO provider_adapters(id, name, engine, endpoint, model, auth_type, secret,
		       timeout_seconds, request_config_json, response_config_json, is_enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		  name = excluded.name,
		  engine = excluded.engine,
		  endpoint = excluded.endpoint,
		  model = excluded.model,
		  auth_type = excluded.auth_type,
		  secret = excluded.secret,
		  timeout_seconds = excluded.timeout_seconds,
		  request_config_json = excluded.request_config_json,
		  response_config_json = excluded.response_config_json,
		  is_enabled = excluded.is_enabled,
		  updated_at = excluded.updated_at
	`, item.ID, item.Name, item.Engine, item.Endpoint, item.Model, item.AuthType, databaseSecret,
		item.TimeoutSeconds, item.RequestConfigJSON, item.ResponseConfigJSON, item.IsEnabled, item.CreatedAt, item.UpdatedAt)
	if err != nil {
		return ProviderAdapter{}, err
	}
	return s.GetProviderAdapter(ctx, item.ID)
}

func (s *Store) DeleteProviderAdapter(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM provider_adapters WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	return s.deleteAdapterSecret(id)
}

func scanProviderAdapter(scanner interface{ Scan(...any) error }) (ProviderAdapter, error) {
	var item ProviderAdapter
	var enabled int
	err := scanner.Scan(&item.ID, &item.Name, &item.Engine, &item.Endpoint, &item.Model, &item.AuthType,
		&item.Secret, &item.TimeoutSeconds, &item.RequestConfigJSON, &item.ResponseConfigJSON,
		&enabled, &item.CreatedAt, &item.UpdatedAt)
	item.SecretSet = strings.TrimSpace(item.Secret) != ""
	item.IsEnabled = enabled != 0
	return item, err
}

func (s *Store) CreateRecognitionExperiment(ctx context.Context, experiment RecognitionExperiment, variants []RecognitionExperimentVariant, job Job) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if strings.TrimSpace(job.PayloadJSON) == "" {
		job.PayloadJSON = "{}"
	}
	if strings.TrimSpace(job.ResultJSON) == "" {
		job.ResultJSON = "{}"
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO jobs(id, type, status, target_type, target_id, payload_json, attempts, max_attempts,
		       last_error, progress_current, progress_total, progress_message, result_json, created_at, started_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''))
	`, job.ID, job.Type, job.Status, job.TargetType, job.TargetID, job.PayloadJSON, job.Attempts, job.MaxAttempts,
		job.LastError, job.ProgressCurrent, job.ProgressTotal, job.ProgressMessage, job.ResultJSON,
		job.CreatedAt, job.StartedAt, job.FinishedAt)
	if err != nil {
		return err
	}
	pageIDs, err := json.Marshal(experiment.PageIDs)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO recognition_experiments(id, document_id, job_id, name, page_ids_json, status,
		       winner_variant_id, error, created_at, started_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, NULLIF(?, ''), NULLIF(?, ''))
	`, experiment.ID, experiment.DocumentID, experiment.JobID, experiment.Name, string(pageIDs),
		experiment.Status, experiment.WinnerVariantID, experiment.Error, experiment.CreatedAt,
		experiment.StartedAt, experiment.FinishedAt)
	if err != nil {
		return err
	}
	for _, variant := range variants {
		runIDs, _ := json.Marshal(variant.RunIDs)
		_, err = tx.ExecContext(ctx, `
			INSERT INTO recognition_experiment_variants(
			  id, experiment_id, name, recognizer_profile_id, provider_adapter_id, prompt_version_id,
			  snapshot_json, image_source, position, status, run_ids_json, current_run_ids_json, avg_confidence, duration_ms,
			  manual_edit_distance, error, created_at, started_at, finished_at)
			VALUES (?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''))
		`, variant.ID, variant.ExperimentID, variant.Name, variant.ProfileID, variant.ProviderAdapterID,
			variant.PromptVersionID, variant.SnapshotJSON, variant.ImageSource, variant.Position, variant.Status, string(runIDs), "[]",
			variant.AverageConfidence, variant.DurationMS, variant.ManualEditDistance, variant.Error,
			variant.CreatedAt, variant.StartedAt, variant.FinishedAt)
		if err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO job_events(id, job_id, attempt, level, stage, message, data_json, created_at)
		VALUES (?, ?, ?, 'info', 'queued', '任务已进入队列', '{}', ?)
	`, newID("evt"), job.ID, job.Attempts, now()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ListRecognitionExperiments(ctx context.Context, documentID string) ([]RecognitionExperiment, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, document_id, job_id, name, page_ids_json, status,
		       COALESCE(winner_variant_id, ''), error, created_at,
		       COALESCE(started_at, ''), COALESCE(finished_at, '')
		FROM recognition_experiments WHERE document_id = ? ORDER BY created_at DESC
	`, documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []RecognitionExperiment{}
	for rows.Next() {
		item, err := scanRecognitionExperiment(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range items {
		items[index].Variants, err = s.listRecognitionExperimentVariants(ctx, items[index].ID, items[index].WinnerVariantID)
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (s *Store) GetRecognitionExperiment(ctx context.Context, id string) (RecognitionExperiment, error) {
	item, err := scanRecognitionExperiment(s.db.QueryRowContext(ctx, `
		SELECT id, document_id, job_id, name, page_ids_json, status,
		       COALESCE(winner_variant_id, ''), error, created_at,
		       COALESCE(started_at, ''), COALESCE(finished_at, '')
		FROM recognition_experiments WHERE id = ?
	`, id))
	if err != nil {
		return RecognitionExperiment{}, err
	}
	item.Variants, err = s.listRecognitionExperimentVariants(ctx, item.ID, item.WinnerVariantID)
	return item, err
}

func scanRecognitionExperiment(scanner interface{ Scan(...any) error }) (RecognitionExperiment, error) {
	var item RecognitionExperiment
	var pageIDs string
	err := scanner.Scan(&item.ID, &item.DocumentID, &item.JobID, &item.Name, &pageIDs, &item.Status,
		&item.WinnerVariantID, &item.Error, &item.CreatedAt, &item.StartedAt, &item.FinishedAt)
	if err == nil {
		_ = json.Unmarshal([]byte(pageIDs), &item.PageIDs)
	}
	return item, err
}

func (s *Store) listRecognitionExperimentVariants(ctx context.Context, experimentID, winnerID string) ([]RecognitionExperimentVariant, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, experiment_id, name, COALESCE(recognizer_profile_id, ''), COALESCE(provider_adapter_id, ''),
		       COALESCE(prompt_version_id, ''), snapshot_json, image_source, position, status, run_ids_json, current_run_ids_json,
		       avg_confidence, duration_ms, manual_edit_distance, error, created_at,
		       COALESCE(started_at, ''), COALESCE(finished_at, '')
		FROM recognition_experiment_variants WHERE experiment_id = ? ORDER BY position
	`, experimentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []RecognitionExperimentVariant{}
	for rows.Next() {
		var item RecognitionExperimentVariant
		var runIDs, currentRunIDs string
		var confidence sql.NullFloat64
		if err := rows.Scan(&item.ID, &item.ExperimentID, &item.Name, &item.ProfileID, &item.ProviderAdapterID,
			&item.PromptVersionID, &item.SnapshotJSON, &item.ImageSource, &item.Position, &item.Status, &runIDs, &currentRunIDs, &confidence,
			&item.DurationMS, &item.ManualEditDistance, &item.Error, &item.CreatedAt,
			&item.StartedAt, &item.FinishedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(runIDs), &item.RunIDs)
		_ = json.Unmarshal([]byte(currentRunIDs), &item.CurrentRunIDs)
		if confidence.Valid {
			item.AverageConfidence = &confidence.Float64
		}
		item.SelectedWinner = item.ID == winnerID
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) MarkRecognitionExperimentRunning(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE recognition_experiments SET status = 'running', started_at = ?, finished_at = NULL, error = ''
		WHERE id = ? AND status = 'queued'
	`, now(), id)
	return err
}

func (s *Store) FinishRecognitionExperiment(ctx context.Context, id, status, message string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE recognition_experiments SET status = ?, error = ?, finished_at = ? WHERE id = ?
	`, status, message, now(), id)
	return err
}

func (s *Store) MarkExperimentVariantRunning(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE recognition_experiment_variants
		SET status = 'running', started_at = ?, finished_at = NULL, error = '' WHERE id = ?
	`, now(), id)
	return err
}

func (s *Store) FinishExperimentVariant(ctx context.Context, id, status string, runIDs []string, confidence *float64, durationMS int64, message string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var existingRaw string
	if err := tx.QueryRowContext(ctx, `SELECT run_ids_json FROM recognition_experiment_variants WHERE id = ?`, id).Scan(&existingRaw); err != nil {
		return err
	}
	var existing []string
	_ = json.Unmarshal([]byte(existingRaw), &existing)
	for _, runID := range runIDs {
		existing = appendUniqueString(existing, runID)
	}
	raw, _ := json.Marshal(existing)
	_, err = tx.ExecContext(ctx, `
		UPDATE recognition_experiment_variants
		SET status = ?, run_ids_json = ?, avg_confidence = ?, duration_ms = ?, error = ?, finished_at = ?
		WHERE id = ?
	`, status, string(raw), confidence, durationMS, message, now(), id)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SetExperimentVariantEditDistance(ctx context.Context, id string, distance int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE recognition_experiment_variants SET manual_edit_distance = ? WHERE id = ?`, distance, id)
	return err
}

func (s *Store) SelectRecognitionExperimentWinner(ctx context.Context, experimentID, variantID string) error {
	var count int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM recognition_experiment_variants WHERE id = ? AND experiment_id = ?
	`, variantID, experimentID).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	_, err := s.db.ExecContext(ctx, `UPDATE recognition_experiments SET winner_variant_id = ? WHERE id = ?`, variantID, experimentID)
	return err
}

func (s *Store) ResetRecognitionExperimentForRetry(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `
		UPDATE recognition_experiments SET status = 'queued', error = '', started_at = NULL, finished_at = NULL WHERE id = ?
	`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE recognition_experiment_variants
		SET status = 'queued', current_run_ids_json = '[]', avg_confidence = NULL, duration_ms = 0,
		    manual_edit_distance = 0, error = '', started_at = NULL, finished_at = NULL
		WHERE experiment_id = ?
	`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RequeueRecognitionExperimentJob(ctx context.Context, jobID, experimentID string) (Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE jobs SET status = 'queued', last_error = '', progress_current = 0, progress_message = '等待重试',
		       result_json = '{}', started_at = NULL, finished_at = NULL
		WHERE id = ? AND status = 'failed' AND attempts < max_attempts
	`, jobID)
	if err != nil {
		return Job{}, err
	}
	if changed, _ := result.RowsAffected(); changed == 0 {
		return Job{}, errors.New("job cannot be retried (it may have reached max attempts)")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE recognition_experiments SET status = 'queued', error = '', started_at = NULL, finished_at = NULL WHERE id = ?`, experimentID); err != nil {
		return Job{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE recognition_experiment_variants
		SET status = 'queued', current_run_ids_json = '[]', avg_confidence = NULL, duration_ms = 0, manual_edit_distance = 0,
		    error = '', started_at = NULL, finished_at = NULL WHERE experiment_id = ?
	`, experimentID); err != nil {
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
	if err := tx.Commit(); err != nil {
		return Job{}, err
	}
	return job, nil
}

func appendUniqueString(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

func (s *Store) RecognitionResultsForRuns(ctx context.Context, runIDs []string) ([]RecognitionResult, error) {
	if len(runIDs) == 0 {
		return []RecognitionResult{}, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(runIDs)), ",")
	args := make([]any, len(runIDs))
	for index := range runIDs {
		args[index] = runIDs[index]
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.id, r.run_id, r.page_id, r.text, r.confidence, r.raw_json, r.metadata_json, r.created_at,
		       run.provider, run.model, run.prompt_version, run.config_json
		FROM recognition_results r JOIN recognition_runs run ON run.id = r.run_id
		WHERE r.run_id IN (`+placeholders+`) ORDER BY r.created_at, r.id
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := []RecognitionResult{}
	for rows.Next() {
		var item RecognitionResult
		var confidence sql.NullFloat64
		if err := rows.Scan(&item.ID, &item.RunID, &item.PageID, &item.Text, &confidence, &item.RawJSON,
			&item.MetadataJSON, &item.CreatedAt, &item.Provider, &item.Model, &item.PromptVersion,
			&item.ConfigJSON); err != nil {
			return nil, err
		}
		if confidence.Valid {
			item.Confidence = &confidence.Float64
		}
		results = append(results, item)
	}
	return results, rows.Err()
}

func (s *Store) ExperimentByJobID(ctx context.Context, jobID string) (RecognitionExperiment, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM recognition_experiments WHERE job_id = ?`, jobID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return RecognitionExperiment{}, err
	}
	if err != nil {
		return RecognitionExperiment{}, err
	}
	return s.GetRecognitionExperiment(ctx, id)
}

func (s *Store) LatestHumanTextAfter(ctx context.Context, pageID, after string) (string, bool, error) {
	var text string
	err := s.db.QueryRowContext(ctx, `
		SELECT text FROM text_versions
		WHERE page_id = ? AND kind IN ('manual', 'final') AND created_at >= ?
		ORDER BY created_at DESC, rowid DESC LIMIT 1
	`, pageID, after).Scan(&text)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return text, err == nil, err
}
