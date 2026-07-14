package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func (s *Store) ListRecognizerProfiles(ctx context.Context) ([]RecognizerProfile, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.name, p.driver, p.base_url, p.api_key, p.model, p.params_json,
		       COALESCE(p.prompt_version_id, ''), COALESCE(v.version, ''), COALESCE(v.sha256, ''),
		       p.is_default, p.created_at, p.updated_at
		FROM recognizer_profiles p
		LEFT JOIN prompt_versions v ON v.id = p.prompt_version_id
		ORDER BY p.is_default DESC, p.updated_at DESC, p.name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	profiles := []RecognizerProfile{}
	for rows.Next() {
		profile, err := scanRecognizerProfile(rows)
		if err != nil {
			return nil, err
		}
		profile.APIKey = s.profileSecret(profile.ID, profile.APIKey)
		profile.APIKeySet = strings.TrimSpace(profile.APIKey) != ""
		profiles = append(profiles, profile)
	}
	return profiles, rows.Err()
}

func (s *Store) GetRecognizerProfile(ctx context.Context, id string) (RecognizerProfile, error) {
	profile, err := scanRecognizerProfile(s.db.QueryRowContext(ctx, `
		SELECT p.id, p.name, p.driver, p.base_url, p.api_key, p.model, p.params_json,
		       COALESCE(p.prompt_version_id, ''), COALESCE(v.version, ''), COALESCE(v.sha256, ''),
		       p.is_default, p.created_at, p.updated_at
		FROM recognizer_profiles p
		LEFT JOIN prompt_versions v ON v.id = p.prompt_version_id
		WHERE p.id = ?
	`, id))
	if err == nil {
		profile.APIKey = s.profileSecret(profile.ID, profile.APIKey)
		profile.APIKeySet = strings.TrimSpace(profile.APIKey) != ""
	}
	return profile, err
}

func (s *Store) DefaultRecognizerProfile(ctx context.Context) (RecognizerProfile, bool, error) {
	profile, err := scanRecognizerProfile(s.db.QueryRowContext(ctx, `
		SELECT p.id, p.name, p.driver, p.base_url, p.api_key, p.model, p.params_json,
		       COALESCE(p.prompt_version_id, ''), COALESCE(v.version, ''), COALESCE(v.sha256, ''),
		       p.is_default, p.created_at, p.updated_at
		FROM recognizer_profiles p
		LEFT JOIN prompt_versions v ON v.id = p.prompt_version_id
		WHERE p.is_default = 1
	`))
	if errors.Is(err, sql.ErrNoRows) {
		return RecognizerProfile{}, false, nil
	}
	if err == nil {
		profile.APIKey = s.profileSecret(profile.ID, profile.APIKey)
		profile.APIKeySet = strings.TrimSpace(profile.APIKey) != ""
	}
	return profile, err == nil, err
}

func (s *Store) SaveRecognizerProfile(ctx context.Context, profile RecognizerProfile) (RecognizerProfile, error) {
	databaseSecret, err := s.saveProfileSecret(profile.ID, profile.APIKey)
	if err != nil {
		return RecognizerProfile{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RecognizerProfile{}, err
	}
	defer tx.Rollback()
	if profile.IsDefault {
		if _, err := tx.ExecContext(ctx, `UPDATE recognizer_profiles SET is_default = 0 WHERE is_default = 1 AND id <> ?`, profile.ID); err != nil {
			return RecognizerProfile{}, err
		}
	}
	if profile.CreatedAt == "" {
		profile.CreatedAt = now()
	}
	profile.UpdatedAt = now()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO recognizer_profiles(id, name, driver, base_url, api_key, model, params_json,
		       prompt_version_id, is_default, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		  name = excluded.name,
		  driver = excluded.driver,
		  base_url = excluded.base_url,
		  api_key = excluded.api_key,
		  model = excluded.model,
		  params_json = excluded.params_json,
		  prompt_version_id = excluded.prompt_version_id,
		  is_default = excluded.is_default,
		  updated_at = excluded.updated_at
	`, profile.ID, profile.Name, profile.Driver, profile.BaseURL, databaseSecret, profile.Model,
		profile.ParamsJSON, profile.PromptVersionID, profile.IsDefault, profile.CreatedAt, profile.UpdatedAt)
	if err != nil {
		return RecognizerProfile{}, err
	}
	if err := tx.Commit(); err != nil {
		return RecognizerProfile{}, err
	}
	return s.GetRecognizerProfile(ctx, profile.ID)
}

func (s *Store) DeleteRecognizerProfile(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM recognizer_profiles WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	return s.deleteProfileSecret(id)
}

func (s *Store) ActivePromptVersion(ctx context.Context) (PromptVersion, bool, error) {
	var item PromptVersion
	err := s.db.QueryRowContext(ctx, `
		SELECT id, version, content, sha256, is_active, created_at, COALESCE(activated_at, '')
		FROM prompt_versions WHERE is_active = 1
	`).Scan(&item.ID, &item.Version, &item.Content, &item.SHA256, &item.IsActive, &item.CreatedAt, &item.ActivatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return PromptVersion{}, false, nil
	}
	return item, err == nil, err
}

func (s *Store) CreateCandidateMerge(ctx context.Context, merge CandidateMerge, version TextVersion) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO text_versions(id, document_id, page_id, kind, base_version_id, source_result_id, text, status, created_by, created_at)
		VALUES (?, ?, ?, 'candidate', NULLIF(?, ''), NULL, ?, ?, ?, ?)
	`, version.ID, version.DocumentID, version.PageID, version.BaseVersionID, version.Text, version.Status, version.CreatedBy, version.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert merged text version (document=%q page=%q base=%q): %w", version.DocumentID, version.PageID, version.BaseVersionID, err)
	}
	sourceIDs, _ := json.Marshal(merge.SourceResultIDs)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO candidate_merges(id, page_id, text_version_id, source_result_ids_json,
		       recognizer_profile_id, driver, prompt_version, prompt_hash, raw_response, created_at)
		VALUES (?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?)
	`, merge.ID, merge.PageID, version.ID, string(sourceIDs), merge.RecognizerProfileID, merge.Driver,
		merge.PromptVersion, merge.PromptHash, merge.RawResponse, merge.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert candidate merge: %w", err)
	}
	for _, segment := range merge.Segments {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO candidate_merge_segments(id, candidate_merge_id, ordinal, source_result_id,
			       source_start, source_end, output_start, output_end, text)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, segment.ID, merge.ID, segment.Ordinal, segment.SourceResultID, segment.SourceStart,
			segment.SourceEnd, segment.OutputStart, segment.OutputEnd, segment.Text)
		if err != nil {
			return fmt.Errorf("insert candidate merge segment %d: %w", segment.Ordinal, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return s.IndexTextVersion(ctx, version)
}

func (s *Store) GetCandidateMergeByTextVersion(ctx context.Context, textVersionID string) (CandidateMerge, error) {
	var merge CandidateMerge
	var sourceJSON string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, page_id, text_version_id, source_result_ids_json,
		       COALESCE(recognizer_profile_id, ''), driver, prompt_version, prompt_hash, raw_response, created_at
		FROM candidate_merges WHERE text_version_id = ?
	`, textVersionID).Scan(&merge.ID, &merge.PageID, &merge.TextVersionID, &sourceJSON,
		&merge.RecognizerProfileID, &merge.Driver, &merge.PromptVersion, &merge.PromptHash, &merge.RawResponse, &merge.CreatedAt)
	if err != nil {
		return CandidateMerge{}, err
	}
	_ = json.Unmarshal([]byte(sourceJSON), &merge.SourceResultIDs)
	version, err := s.GetTextVersion(ctx, textVersionID)
	if err != nil {
		return CandidateMerge{}, err
	}
	merge.TextVersion = version
	allResults, err := s.ListRecognitionResults(ctx, merge.PageID)
	if err != nil {
		return CandidateMerge{}, err
	}
	byID := make(map[string]RecognitionResult, len(allResults))
	for _, result := range allResults {
		byID[result.ID] = result
	}
	for _, id := range merge.SourceResultIDs {
		if result, ok := byID[id]; ok {
			merge.Sources = append(merge.Sources, result)
		}
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, candidate_merge_id, ordinal, source_result_id, source_start, source_end,
		       output_start, output_end, text
		FROM candidate_merge_segments WHERE candidate_merge_id = ? ORDER BY ordinal
	`, merge.ID)
	if err != nil {
		return CandidateMerge{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var segment CandidateMergeSegment
		if err := rows.Scan(&segment.ID, &segment.CandidateMergeID, &segment.Ordinal, &segment.SourceResultID,
			&segment.SourceStart, &segment.SourceEnd, &segment.OutputStart, &segment.OutputEnd, &segment.Text); err != nil {
			return CandidateMerge{}, err
		}
		merge.Segments = append(merge.Segments, segment)
	}
	if err := rows.Err(); err != nil {
		return CandidateMerge{}, err
	}
	return merge, nil
}

func scanRecognizerProfile(scanner interface{ Scan(...any) error }) (RecognizerProfile, error) {
	var profile RecognizerProfile
	var isDefault int
	err := scanner.Scan(&profile.ID, &profile.Name, &profile.Driver, &profile.BaseURL, &profile.APIKey,
		&profile.Model, &profile.ParamsJSON, &profile.PromptVersionID, &profile.PromptVersion, &profile.PromptSHA256,
		&isDefault, &profile.CreatedAt, &profile.UpdatedAt)
	profile.APIKeySet = strings.TrimSpace(profile.APIKey) != ""
	profile.IsDefault = isDefault != 0
	return profile, err
}
