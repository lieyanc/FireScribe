package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

func (s *Store) CreateCrossCheck(ctx context.Context, check CrossCheck, variants []CrossCheckVariant, pages []Page, job Job) error {
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
	pageIDs, err := json.Marshal(check.PageIDs)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO cross_checks(id, document_id, job_id, name, page_ids_json, merge_profile_id, status,
		       error, consensus_pages, disagreement_pages, failed_pages, created_at, started_at, finished_at)
		VALUES (?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''))
	`, check.ID, check.DocumentID, check.JobID, check.Name, string(pageIDs), check.MergeProfileID, check.Status,
		check.Error, check.ConsensusPages, check.DisagreementPages, check.FailedPages,
		check.CreatedAt, check.StartedAt, check.FinishedAt)
	if err != nil {
		return err
	}
	for _, variant := range variants {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO cross_check_variants(id, cross_check_id, name, recognizer_profile_id, provider_adapter_id,
			       prompt_version_id, snapshot_json, image_source, position, status, run_id, error, created_at, started_at, finished_at)
			VALUES (?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''))
		`, variant.ID, variant.CrossCheckID, variant.Name, variant.ProfileID, variant.ProviderAdapterID,
			variant.PromptVersionID, variant.SnapshotJSON, variant.ImageSource, variant.Position, variant.Status,
			variant.RunID, variant.Error, variant.CreatedAt, variant.StartedAt, variant.FinishedAt)
		if err != nil {
			return err
		}
	}
	for _, page := range pages {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO cross_check_pages(cross_check_id, page_id, page_no, status) VALUES (?, ?, ?, 'pending')
		`, check.ID, page.ID, page.PageNo); err != nil {
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

func (s *Store) ListCrossChecks(ctx context.Context, documentID string) ([]CrossCheck, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, document_id, job_id, name, page_ids_json, COALESCE(merge_profile_id, ''), status, error,
		       consensus_pages, disagreement_pages, failed_pages, created_at,
		       COALESCE(started_at, ''), COALESCE(finished_at, '')
		FROM cross_checks WHERE document_id = ? ORDER BY created_at DESC
	`, documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []CrossCheck{}
	for rows.Next() {
		item, err := scanCrossCheck(rows)
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
		items[index].Variants, err = s.listCrossCheckVariants(ctx, items[index].ID)
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (s *Store) GetCrossCheck(ctx context.Context, id string) (CrossCheck, error) {
	item, err := scanCrossCheck(s.db.QueryRowContext(ctx, `
		SELECT id, document_id, job_id, name, page_ids_json, COALESCE(merge_profile_id, ''), status, error,
		       consensus_pages, disagreement_pages, failed_pages, created_at,
		       COALESCE(started_at, ''), COALESCE(finished_at, '')
		FROM cross_checks WHERE id = ?
	`, id))
	if err != nil {
		return CrossCheck{}, err
	}
	if item.Variants, err = s.listCrossCheckVariants(ctx, item.ID); err != nil {
		return CrossCheck{}, err
	}
	if item.Pages, err = s.ListCrossCheckPages(ctx, item.ID); err != nil {
		return CrossCheck{}, err
	}
	return item, nil
}

func (s *Store) CrossCheckByJobID(ctx context.Context, jobID string) (CrossCheck, error) {
	var id string
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM cross_checks WHERE job_id = ?`, jobID).Scan(&id); err != nil {
		return CrossCheck{}, err
	}
	return s.GetCrossCheck(ctx, id)
}

func (s *Store) ActiveCrossCheckForDocument(ctx context.Context, documentID string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM cross_checks WHERE document_id = ? AND status IN ('queued', 'running')
	`, documentID).Scan(&count)
	return count > 0, err
}

func scanCrossCheck(scanner interface{ Scan(...any) error }) (CrossCheck, error) {
	var item CrossCheck
	var pageIDs string
	err := scanner.Scan(&item.ID, &item.DocumentID, &item.JobID, &item.Name, &pageIDs, &item.MergeProfileID,
		&item.Status, &item.Error, &item.ConsensusPages, &item.DisagreementPages, &item.FailedPages,
		&item.CreatedAt, &item.StartedAt, &item.FinishedAt)
	if err == nil {
		_ = json.Unmarshal([]byte(pageIDs), &item.PageIDs)
	}
	return item, err
}

func (s *Store) listCrossCheckVariants(ctx context.Context, checkID string) ([]CrossCheckVariant, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, cross_check_id, name, COALESCE(recognizer_profile_id, ''), COALESCE(provider_adapter_id, ''),
		       COALESCE(prompt_version_id, ''), snapshot_json, image_source, position, status, run_id, error,
		       created_at, COALESCE(started_at, ''), COALESCE(finished_at, '')
		FROM cross_check_variants WHERE cross_check_id = ? ORDER BY position
	`, checkID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []CrossCheckVariant{}
	for rows.Next() {
		var item CrossCheckVariant
		if err := rows.Scan(&item.ID, &item.CrossCheckID, &item.Name, &item.ProfileID, &item.ProviderAdapterID,
			&item.PromptVersionID, &item.SnapshotJSON, &item.ImageSource, &item.Position, &item.Status,
			&item.RunID, &item.Error, &item.CreatedAt, &item.StartedAt, &item.FinishedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListCrossCheckPages(ctx context.Context, checkID string) ([]CrossCheckPage, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT cp.cross_check_id, cp.page_id, cp.page_no, cp.status, cp.agreement, cp.result_ids_json,
		       COALESCE(cp.consensus_version_id, ''), COALESCE(cp.merged_version_id, ''), COALESCE(cp.annotation_id, ''),
		       cp.conflicts_json, COALESCE(cp.adopted_version_id, ''), COALESCE(cp.adopted_at, ''), cp.error,
		       COALESCE(ev.kind, '')
		FROM cross_check_pages cp
		LEFT JOIN effective_text_versions ev ON ev.page_id = cp.page_id
		WHERE cp.cross_check_id = ? ORDER BY cp.page_no
	`, checkID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []CrossCheckPage{}
	for rows.Next() {
		item, err := scanCrossCheckPage(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanCrossCheckPage(scanner interface{ Scan(...any) error }) (CrossCheckPage, error) {
	var item CrossCheckPage
	var agreement sql.NullFloat64
	var resultIDs, conflicts string
	err := scanner.Scan(&item.CrossCheckID, &item.PageID, &item.PageNo, &item.Status, &agreement, &resultIDs,
		&item.ConsensusVersionID, &item.MergedVersionID, &item.AnnotationID, &conflicts,
		&item.AdoptedVersionID, &item.AdoptedAt, &item.Error, &item.EffectiveKind)
	if err != nil {
		return CrossCheckPage{}, err
	}
	if agreement.Valid {
		item.Agreement = &agreement.Float64
	}
	_ = json.Unmarshal([]byte(resultIDs), &item.ResultIDs)
	_ = json.Unmarshal([]byte(conflicts), &item.Conflicts)
	if item.ResultIDs == nil {
		item.ResultIDs = []string{}
	}
	if item.Conflicts == nil {
		item.Conflicts = []CrossCheckConflict{}
	}
	return item, nil
}

// LatestCrossCheckForPage returns the newest cross-check outcome recorded for
// a page, so the review UI can surface agreement and conflicts in place.
func (s *Store) LatestCrossCheckForPage(ctx context.Context, pageID string) (CrossCheck, CrossCheckPage, error) {
	page, err := scanCrossCheckPage(s.db.QueryRowContext(ctx, `
		SELECT cp.cross_check_id, cp.page_id, cp.page_no, cp.status, cp.agreement, cp.result_ids_json,
		       COALESCE(cp.consensus_version_id, ''), COALESCE(cp.merged_version_id, ''), COALESCE(cp.annotation_id, ''),
		       cp.conflicts_json, COALESCE(cp.adopted_version_id, ''), COALESCE(cp.adopted_at, ''), cp.error,
		       COALESCE(ev.kind, '')
		FROM cross_check_pages cp
		JOIN cross_checks cc ON cc.id = cp.cross_check_id
		LEFT JOIN effective_text_versions ev ON ev.page_id = cp.page_id
		WHERE cp.page_id = ?
		ORDER BY cc.created_at DESC, cc.id DESC LIMIT 1
	`, pageID))
	if err != nil {
		return CrossCheck{}, CrossCheckPage{}, err
	}
	check, err := scanCrossCheck(s.db.QueryRowContext(ctx, `
		SELECT id, document_id, job_id, name, page_ids_json, COALESCE(merge_profile_id, ''), status, error,
		       consensus_pages, disagreement_pages, failed_pages, created_at,
		       COALESCE(started_at, ''), COALESCE(finished_at, '')
		FROM cross_checks WHERE id = ?
	`, page.CrossCheckID))
	if err != nil {
		return CrossCheck{}, CrossCheckPage{}, err
	}
	if check.Variants, err = s.listCrossCheckVariants(ctx, check.ID); err != nil {
		return CrossCheck{}, CrossCheckPage{}, err
	}
	return check, page, nil
}

func (s *Store) MarkCrossCheckRunning(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE cross_checks SET status = 'running', started_at = ?, finished_at = NULL, error = ''
		WHERE id = ? AND status = 'queued'
	`, now(), id)
	return err
}

// FinishCrossCheck records terminal state and folds the page outcome counters
// into the row so list views need no extra queries.
func (s *Store) FinishCrossCheck(ctx context.Context, id, status, message string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE cross_checks SET status = ?, error = ?, finished_at = ?,
		  consensus_pages = (SELECT COUNT(*) FROM cross_check_pages cp WHERE cp.cross_check_id = cross_checks.id AND cp.status = 'consensus'),
		  disagreement_pages = (SELECT COUNT(*) FROM cross_check_pages cp WHERE cp.cross_check_id = cross_checks.id AND cp.status = 'disagreement'),
		  failed_pages = (SELECT COUNT(*) FROM cross_check_pages cp WHERE cp.cross_check_id = cross_checks.id AND cp.status IN ('failed', 'canceled'))
		WHERE id = ?
	`, status, message, now(), id)
	return err
}

// CancelCrossCheck is the guarded cancel transition: it only touches checks
// that are still queued/running, so a cancel racing natural completion cannot
// flip a succeeded check to canceled. Counters are folded like FinishCrossCheck.
func (s *Store) CancelCrossCheck(ctx context.Context, id, message string) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE cross_checks SET status = 'canceled', error = ?, finished_at = ?,
		  consensus_pages = (SELECT COUNT(*) FROM cross_check_pages cp WHERE cp.cross_check_id = cross_checks.id AND cp.status = 'consensus'),
		  disagreement_pages = (SELECT COUNT(*) FROM cross_check_pages cp WHERE cp.cross_check_id = cross_checks.id AND cp.status = 'disagreement'),
		  failed_pages = (SELECT COUNT(*) FROM cross_check_pages cp WHERE cp.cross_check_id = cross_checks.id AND cp.status IN ('failed', 'canceled'))
		WHERE id = ? AND status IN ('queued', 'running')
	`, message, now(), id)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) MarkCrossCheckVariantRunning(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE cross_check_variants SET status = 'running', started_at = ?, finished_at = NULL, error = '' WHERE id = ?
	`, now(), id)
	return err
}

// SetCrossCheckVariantRun records the run as soon as it starts so cancellation
// can find and stop the live run before the variant reaches a terminal state.
func (s *Store) SetCrossCheckVariantRun(ctx context.Context, id, runID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE cross_check_variants SET run_id = ? WHERE id = ?`, runID, id)
	return err
}

func (s *Store) FinishCrossCheckVariant(ctx context.Context, id, status, runID, message string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE cross_check_variants
		SET status = ?, run_id = CASE WHEN ? <> '' THEN ? ELSE run_id END, error = ?, finished_at = ?
		WHERE id = ?
	`, status, runID, runID, message, now(), id)
	return err
}

func (s *Store) UpdateCrossCheckPage(ctx context.Context, page CrossCheckPage) error {
	resultIDs, err := json.Marshal(page.ResultIDs)
	if err != nil {
		return err
	}
	conflicts, err := json.Marshal(page.Conflicts)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE cross_check_pages
		SET status = ?, agreement = ?, result_ids_json = ?, consensus_version_id = NULLIF(?, ''),
		    merged_version_id = NULLIF(?, ''), annotation_id = NULLIF(?, ''), conflicts_json = ?, error = ?
		WHERE cross_check_id = ? AND page_id = ?
	`, page.Status, page.Agreement, string(resultIDs), page.ConsensusVersionID,
		page.MergedVersionID, page.AnnotationID, string(conflicts), page.Error,
		page.CrossCheckID, page.PageID)
	return err
}

// ErrCrossCheckPageDecided means the page is not an unadopted consensus page
// (already adopted, or its status changed since the caller loaded it).
var ErrCrossCheckPageDecided = errors.New("page is not an unadopted consensus page")

// ErrCrossCheckHumanVersion means a manual/final version is currently
// effective for the page; adoption never overwrites human work.
var ErrCrossCheckHumanVersion = errors.New("page already has a human text version")

// AdoptCrossCheckPage performs the user's consensus sign-off atomically: the
// human-version guard, the final text version insert, and the adoption claim
// happen in one transaction, so concurrent adoptions cannot double-write and a
// manual version saved concurrently cannot be overshadowed. The version must
// have kind "final" and a fresh ID.
func (s *Store) AdoptCrossCheckPage(ctx context.Context, checkID string, version TextVersion) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var effectiveKind string
	err = tx.QueryRowContext(ctx, `SELECT kind FROM effective_text_versions WHERE page_id = ?`, version.PageID).Scan(&effectiveKind)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if effectiveKind == "manual" || effectiveKind == "final" {
		return ErrCrossCheckHumanVersion
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO text_versions(id, document_id, page_id, kind, base_version_id, source_result_id, text, status, created_by, created_at)
		VALUES (?, ?, NULLIF(?, ''), ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, ?)
	`, version.ID, version.DocumentID, version.PageID, version.Kind, version.BaseVersionID, version.SourceResultID,
		version.Text, version.Status, version.CreatedBy, version.CreatedAt); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE cross_check_pages SET adopted_version_id = ?, adopted_at = ?
		WHERE cross_check_id = ? AND page_id = ? AND status = 'consensus' AND adopted_version_id IS NULL
	`, version.ID, now(), checkID, version.PageID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return ErrCrossCheckPageDecided
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return s.IndexTextVersion(ctx, version)
}

func (s *Store) CancelPendingCrossCheckPages(ctx context.Context, checkID, message string) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE cross_check_pages SET status = 'canceled', error = ? WHERE cross_check_id = ? AND status = 'pending'
	`, message, checkID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// CancelPendingCrossCheckVariants moves variants that never reached a terminal
// state into 'canceled', so a canceled check does not keep 'queued' variants.
func (s *Store) CancelPendingCrossCheckVariants(ctx context.Context, checkID, message string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE cross_check_variants SET status = 'canceled', error = ?, finished_at = ?
		WHERE cross_check_id = ? AND status IN ('queued', 'running')
	`, message, now(), checkID)
	return err
}

// ResolveCrossCheckAnnotations closes machine-generated disagreement notes
// from earlier cross-checks so a re-check never stacks duplicate open items in
// the review queue. Human annotations are left untouched.
func (s *Store) ResolveCrossCheckAnnotations(ctx context.Context, pageID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE annotations SET status = 'resolved', updated_at = ?
		WHERE page_id = ? AND kind = 'uncertain_text' AND status = 'open' AND body LIKE ?
	`, now(), pageID, crossCheckAnnotationPrefix+"%")
	return err
}

// TextVersionIDBySourceResult finds the candidate text version created from a
// recognition result, used to pin the exact consensus version for adoption.
func (s *Store) TextVersionIDBySourceResult(ctx context.Context, resultID string) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `
		SELECT id FROM text_versions WHERE source_result_id = ? AND kind = 'candidate'
		ORDER BY julianday(created_at) DESC, created_at DESC, rowid DESC LIMIT 1
	`, resultID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func (s *Store) RequeueCrossCheckJob(ctx context.Context, jobID, checkID string) (Job, error) {
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
	if _, err := tx.ExecContext(ctx, `
		UPDATE cross_checks SET status = 'queued', error = '', started_at = NULL, finished_at = NULL,
		       consensus_pages = 0, disagreement_pages = 0, failed_pages = 0
		WHERE id = ?
	`, checkID); err != nil {
		return Job{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE cross_check_variants SET status = 'queued', run_id = '', error = '', started_at = NULL, finished_at = NULL
		WHERE cross_check_id = ?
	`, checkID); err != nil {
		return Job{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE cross_check_pages SET status = 'pending', agreement = NULL, result_ids_json = '[]',
		       consensus_version_id = NULL, merged_version_id = NULL, annotation_id = NULL,
		       conflicts_json = '[]', error = ''
		WHERE cross_check_id = ?
	`, checkID); err != nil {
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
