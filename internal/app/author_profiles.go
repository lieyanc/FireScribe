package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

type AuthorProfile struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Notes           string `json:"notes"`
	DocumentCount   int    `json:"document_count"`
	TermCount       int    `json:"term_count"`
	CorrectionCount int    `json:"correction_count"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

type AuthorTerm struct {
	ID              string  `json:"id"`
	AuthorProfileID string  `json:"author_profile_id"`
	Term            string  `json:"term"`
	Replacement     string  `json:"replacement"`
	Note            string  `json:"note"`
	Weight          float64 `json:"weight"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

type AuthorCorrection struct {
	ID              string `json:"id"`
	AuthorProfileID string `json:"author_profile_id"`
	DocumentID      string `json:"document_id"`
	DocumentTitle   string `json:"document_title"`
	PageID          string `json:"page_id"`
	PageNo          int    `json:"page_no"`
	ImageAssetID    string `json:"image_asset_id"`
	TextVersionID   string `json:"text_version_id"`
	SourceResultID  string `json:"source_result_id,omitempty"`
	Provider        string `json:"provider,omitempty"`
	Model           string `json:"model,omitempty"`
	PromptVersion   string `json:"prompt_version,omitempty"`
	SourceText      string `json:"source_text"`
	CorrectedText   string `json:"corrected_text"`
	Kind            string `json:"kind"`
	CreatedAt       string `json:"created_at"`
}

// AuthorRecognitionContext is a stable author-specific prompt supplement and
// its complete audit snapshot. Recognition runs persist SnapshotJSON so later
// prompt changes, term edits, and new corrections never alter historical runs.
type AuthorRecognitionContext struct {
	ProfileID     string `json:"profile_id,omitempty"`
	PromptContext string `json:"prompt_context,omitempty"`
	SnapshotJSON  string `json:"snapshot_json,omitempty"`
}

type authorRecognitionSnapshot struct {
	ProfileID          string                    `json:"profile_id"`
	ProfileName        string                    `json:"profile_name"`
	Notes              string                    `json:"notes,omitempty"`
	Terms              []AuthorTerm              `json:"terms"`
	CorrectionExamples []authorCorrectionExample `json:"correction_examples"`
	ContextSHA256      string                    `json:"context_sha256"`
}

type authorCorrectionExample struct {
	CorrectionID string `json:"correction_id"`
	Source       string `json:"source"`
	Corrected    string `json:"corrected"`
}

var ErrAuthorTermExists = errors.New("author term already exists")

func (s *Store) ListAuthorProfiles(ctx context.Context) ([]AuthorProfile, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.name, p.notes,
		       (SELECT COUNT(*) FROM documents d WHERE d.author_profile_id = p.id),
		       (SELECT COUNT(*) FROM author_terms t WHERE t.author_profile_id = p.id),
		       (SELECT COUNT(*) FROM author_corrections c WHERE c.author_profile_id = p.id),
		       p.created_at, p.updated_at
		FROM author_profiles p
		ORDER BY lower(p.name), p.name, p.created_at
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	profiles := []AuthorProfile{}
	for rows.Next() {
		var profile AuthorProfile
		if err := rows.Scan(&profile.ID, &profile.Name, &profile.Notes, &profile.DocumentCount,
			&profile.TermCount, &profile.CorrectionCount, &profile.CreatedAt, &profile.UpdatedAt); err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	return profiles, rows.Err()
}

func (s *Store) GetAuthorProfile(ctx context.Context, id string) (AuthorProfile, error) {
	var profile AuthorProfile
	err := s.db.QueryRowContext(ctx, `
		SELECT p.id, p.name, p.notes,
		       (SELECT COUNT(*) FROM documents d WHERE d.author_profile_id = p.id),
		       (SELECT COUNT(*) FROM author_terms t WHERE t.author_profile_id = p.id),
		       (SELECT COUNT(*) FROM author_corrections c WHERE c.author_profile_id = p.id),
		       p.created_at, p.updated_at
		FROM author_profiles p WHERE p.id = ?
	`, id).Scan(&profile.ID, &profile.Name, &profile.Notes, &profile.DocumentCount,
		&profile.TermCount, &profile.CorrectionCount, &profile.CreatedAt, &profile.UpdatedAt)
	return profile, err
}

func (s *Store) CreateAuthorProfile(ctx context.Context, name, notes string) (AuthorProfile, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return AuthorProfile{}, errors.New("author profile name is required")
	}
	if utf8.RuneCountInString(name) > 128 {
		return AuthorProfile{}, errors.New("author profile name must not exceed 128 characters")
	}
	profile := AuthorProfile{ID: newID("author"), Name: name, Notes: notes, CreatedAt: now(), UpdatedAt: now()}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO author_profiles(id, name, notes, created_at, updated_at) VALUES (?, ?, ?, ?, ?)
	`, profile.ID, profile.Name, profile.Notes, profile.CreatedAt, profile.UpdatedAt)
	return profile, err
}

func (s *Store) PatchAuthorProfile(ctx context.Context, id string, name, notes *string) (AuthorProfile, error) {
	profile, err := s.GetAuthorProfile(ctx, id)
	if err != nil {
		return AuthorProfile{}, err
	}
	if name != nil {
		profile.Name = strings.TrimSpace(*name)
		if profile.Name == "" {
			return AuthorProfile{}, errors.New("author profile name is required")
		}
		if utf8.RuneCountInString(profile.Name) > 128 {
			return AuthorProfile{}, errors.New("author profile name must not exceed 128 characters")
		}
	}
	if notes != nil {
		profile.Notes = *notes
	}
	profile.UpdatedAt = now()
	result, err := s.db.ExecContext(ctx, `UPDATE author_profiles SET name = ?, notes = ?, updated_at = ? WHERE id = ?`,
		profile.Name, profile.Notes, profile.UpdatedAt, id)
	if err != nil {
		return AuthorProfile{}, err
	}
	if count, err := result.RowsAffected(); err != nil || count == 0 {
		if err != nil {
			return AuthorProfile{}, err
		}
		return AuthorProfile{}, sql.ErrNoRows
	}
	return s.GetAuthorProfile(ctx, id)
}

func (s *Store) DeleteAuthorProfile(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM author_profiles WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if count, err := result.RowsAffected(); err != nil || count == 0 {
		if err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) ListAuthorTerms(ctx context.Context, profileID string) ([]AuthorTerm, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, author_profile_id, term, replacement, note, weight, created_at, updated_at
		FROM author_terms WHERE author_profile_id = ?
		ORDER BY weight DESC, lower(term), term, updated_at DESC
	`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	terms := []AuthorTerm{}
	for rows.Next() {
		var term AuthorTerm
		if err := rows.Scan(&term.ID, &term.AuthorProfileID, &term.Term, &term.Replacement, &term.Note,
			&term.Weight, &term.CreatedAt, &term.UpdatedAt); err != nil {
			return nil, err
		}
		terms = append(terms, term)
	}
	return terms, rows.Err()
}

func normalizeAuthorTerm(term AuthorTerm) (AuthorTerm, error) {
	term.Term = strings.TrimSpace(term.Term)
	term.Replacement = strings.TrimSpace(term.Replacement)
	term.Note = strings.TrimSpace(term.Note)
	if term.Term == "" {
		return AuthorTerm{}, errors.New("term is required")
	}
	if utf8.RuneCountInString(term.Term) > 256 || utf8.RuneCountInString(term.Replacement) > 256 {
		return AuthorTerm{}, errors.New("term and common misrecognition must not exceed 256 characters")
	}
	if term.Weight <= 0 {
		term.Weight = 1
	}
	if term.Weight > 100 {
		term.Weight = 100
	}
	return term, nil
}

func (s *Store) CreateAuthorTerm(ctx context.Context, profileID string, term AuthorTerm) (AuthorTerm, error) {
	if _, err := s.GetAuthorProfile(ctx, profileID); err != nil {
		return AuthorTerm{}, err
	}
	var err error
	term, err = normalizeAuthorTerm(term)
	if err != nil {
		return AuthorTerm{}, err
	}
	term.ID = newID("term")
	term.AuthorProfileID = profileID
	term.CreatedAt = now()
	term.UpdatedAt = term.CreatedAt
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO author_terms(id, author_profile_id, term, replacement, note, weight, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, term.ID, term.AuthorProfileID, term.Term, term.Replacement, term.Note, term.Weight, term.CreatedAt, term.UpdatedAt)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique") {
		return AuthorTerm{}, ErrAuthorTermExists
	}
	return term, err
}

func (s *Store) PatchAuthorTerm(ctx context.Context, id string, input AuthorTerm) (AuthorTerm, error) {
	var current AuthorTerm
	err := s.db.QueryRowContext(ctx, `
		SELECT id, author_profile_id, term, replacement, note, weight, created_at, updated_at FROM author_terms WHERE id = ?
	`, id).Scan(&current.ID, &current.AuthorProfileID, &current.Term, &current.Replacement, &current.Note,
		&current.Weight, &current.CreatedAt, &current.UpdatedAt)
	if err != nil {
		return AuthorTerm{}, err
	}
	if input.Term != "" {
		current.Term = input.Term
	}
	current.Replacement = input.Replacement
	current.Note = input.Note
	if input.Weight != 0 {
		current.Weight = input.Weight
	}
	current, err = normalizeAuthorTerm(current)
	if err != nil {
		return AuthorTerm{}, err
	}
	current.UpdatedAt = now()
	_, err = s.db.ExecContext(ctx, `
		UPDATE author_terms SET term = ?, replacement = ?, note = ?, weight = ?, updated_at = ? WHERE id = ?
	`, current.Term, current.Replacement, current.Note, current.Weight, current.UpdatedAt, id)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique") {
		return AuthorTerm{}, ErrAuthorTermExists
	}
	return current, err
}

func (s *Store) DeleteAuthorTerm(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM author_terms WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if count, err := result.RowsAffected(); err != nil || count == 0 {
		if err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) SetDocumentAuthorProfile(ctx context.Context, documentID, profileID string) (AuthorProfile, error) {
	if _, err := s.GetDocument(ctx, documentID); err != nil {
		return AuthorProfile{}, err
	}
	if strings.TrimSpace(profileID) == "" {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return AuthorProfile{}, err
		}
		defer tx.Rollback()
		if _, err = tx.ExecContext(ctx, `UPDATE documents SET author_profile_id = NULL, updated_at = ? WHERE id = ?`, now(), documentID); err != nil {
			return AuthorProfile{}, err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM author_corrections WHERE document_id = ?`, documentID); err != nil {
			return AuthorProfile{}, err
		}
		return AuthorProfile{}, tx.Commit()
	}
	profile, err := s.GetAuthorProfile(ctx, profileID)
	if err != nil {
		return AuthorProfile{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AuthorProfile{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
		UPDATE documents
		SET author_profile_id = ?, author = CASE WHEN trim(author) = '' THEN ? ELSE author END, updated_at = ?
		WHERE id = ?
	`, profileID, profile.Name, now(), documentID)
	if err != nil {
		return AuthorProfile{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE author_corrections SET author_profile_id = ? WHERE document_id = ?`, profileID, documentID); err != nil {
		return AuthorProfile{}, err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, document_id, page_id, kind, COALESCE(base_version_id, ''),
		       COALESCE(source_result_id, ''), text, status, created_by, created_at
		FROM text_versions
		WHERE document_id = ? AND kind IN ('manual', 'final')
		ORDER BY created_at, rowid
	`, documentID)
	if err != nil {
		return AuthorProfile{}, err
	}
	versions := []TextVersion{}
	for rows.Next() {
		version, scanErr := scanTextVersion(rows)
		if scanErr != nil {
			_ = rows.Close()
			return AuthorProfile{}, scanErr
		}
		versions = append(versions, version)
	}
	if err = rows.Close(); err != nil {
		return AuthorProfile{}, err
	}
	for _, version := range versions {
		if err = recordAuthorCorrectionWith(ctx, tx, profileID, version); err != nil {
			return AuthorProfile{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return AuthorProfile{}, err
	}
	return profile, nil
}

func (s *Store) GetDocumentAuthorProfile(ctx context.Context, documentID string) (AuthorProfile, bool, error) {
	var profileID sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT author_profile_id FROM documents WHERE id = ?`, documentID).Scan(&profileID); err != nil {
		return AuthorProfile{}, false, err
	}
	if !profileID.Valid || strings.TrimSpace(profileID.String) == "" {
		return AuthorProfile{}, false, nil
	}
	profile, err := s.GetAuthorProfile(ctx, profileID.String)
	return profile, err == nil, err
}

func (s *Store) ListAuthorProfileDocuments(ctx context.Context, profileID string) ([]Document, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM documents WHERE author_profile_id = ? ORDER BY updated_at DESC`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	documents := make([]Document, 0, len(ids))
	for _, id := range ids {
		document, err := s.GetDocument(ctx, id)
		if err != nil {
			return nil, err
		}
		documents = append(documents, document)
	}
	return documents, nil
}

func (s *Store) RecordAuthorCorrection(ctx context.Context, version TextVersion) error {
	if version.PageID == "" || (version.Kind != "manual" && version.Kind != "final") {
		return nil
	}
	var profileID sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT author_profile_id FROM documents WHERE id = ?`, version.DocumentID).Scan(&profileID); err != nil {
		return err
	}
	if !profileID.Valid || profileID.String == "" {
		return nil
	}
	return recordAuthorCorrectionWith(ctx, s.db, profileID.String, version)
}

type authorCorrectionRunner interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func recordAuthorCorrectionWith(ctx context.Context, runner authorCorrectionRunner, profileID string, version TextVersion) error {
	sourceText := ""
	sourceResultID := version.SourceResultID
	if version.BaseVersionID != "" {
		_ = runner.QueryRowContext(ctx, `
			SELECT text, COALESCE(source_result_id, '') FROM text_versions WHERE id = ?
		`, version.BaseVersionID).Scan(&sourceText, &sourceResultID)
	}
	if sourceText == "" && sourceResultID != "" {
		_ = runner.QueryRowContext(ctx, `SELECT text FROM recognition_results WHERE id = ?`, sourceResultID).Scan(&sourceText)
	}
	if sourceText == "" {
		_ = runner.QueryRowContext(ctx, `
			SELECT id, text FROM recognition_results WHERE page_id = ? ORDER BY created_at DESC, rowid DESC LIMIT 1
		`, version.PageID).Scan(&sourceResultID, &sourceText)
	}
	if strings.TrimSpace(sourceText) == "" || sourceText == version.Text {
		return nil
	}
	correctionID := newID("correction")
	result, err := runner.ExecContext(ctx, `
		INSERT OR IGNORE INTO author_corrections(
			id, author_profile_id, document_id, page_id, text_version_id, source_result_id,
			source_text, corrected_text, kind, created_at
		) VALUES (?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?)
	`, correctionID, profileID, version.DocumentID, version.PageID, version.ID, sourceResultID,
		sourceText, version.Text, version.Kind, version.CreatedAt)
	if err != nil {
		return err
	}
	inserted, err := result.RowsAffected()
	if err != nil || inserted == 0 {
		return err
	}
	return persistAuthorCorrectionMetric(ctx, runner, correctionID, sourceText, version.Text)
}

// SyncAuthorCorrections backfills training samples for manual/final versions
// created before a document was associated with an author profile.
func (s *Store) SyncAuthorCorrections(ctx context.Context, documentID string) (int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, document_id, page_id, kind, COALESCE(base_version_id, ''),
		       COALESCE(source_result_id, ''), text, status, created_by, created_at
		FROM text_versions
		WHERE document_id = ? AND kind IN ('manual', 'final')
		ORDER BY created_at, rowid
	`, documentID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	versions := []TextVersion{}
	for rows.Next() {
		version, err := scanTextVersion(rows)
		if err != nil {
			return 0, err
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	before := 0
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM author_corrections WHERE document_id = ?`, documentID).Scan(&before); err != nil {
		return 0, err
	}
	for _, version := range versions {
		if err := s.RecordAuthorCorrection(ctx, version); err != nil {
			return 0, err
		}
	}
	if _, err := s.SyncAuthorCorrectionMetrics(ctx, documentID); err != nil {
		return 0, err
	}
	after := 0
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM author_corrections WHERE document_id = ?`, documentID).Scan(&after); err != nil {
		return 0, err
	}
	return after - before, nil
}

func (s *Store) ListAuthorCorrections(ctx context.Context, profileID string, limit int) ([]AuthorCorrection, error) {
	if limit <= 0 || limit > 10000 {
		limit = 10000
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.author_profile_id, c.document_id, d.title, c.page_id, p.page_no,
		       COALESCE(p.image_asset_id, ''), c.text_version_id, COALESCE(c.source_result_id, ''),
		       COALESCE(run.provider, ''), COALESCE(run.model, ''), COALESCE(run.prompt_version, ''), c.source_text,
		       c.corrected_text, c.kind, c.created_at
		FROM author_corrections c
		JOIN documents d ON d.id = c.document_id
		JOIN pages p ON p.id = c.page_id
		LEFT JOIN recognition_results result ON result.id = c.source_result_id
		LEFT JOIN recognition_runs run ON run.id = result.run_id
		WHERE c.author_profile_id = ?
		ORDER BY c.created_at DESC, c.id DESC LIMIT ?
	`, profileID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AuthorCorrection{}
	for rows.Next() {
		var item AuthorCorrection
		if err := rows.Scan(&item.ID, &item.AuthorProfileID, &item.DocumentID, &item.DocumentTitle,
			&item.PageID, &item.PageNo, &item.ImageAssetID, &item.TextVersionID, &item.SourceResultID,
			&item.Provider, &item.Model, &item.PromptVersion, &item.SourceText,
			&item.CorrectedText, &item.Kind, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// BuildAuthorRecognitionContext resolves an explicit profile override first,
// otherwise the document association. It caps prompt examples while retaining
// enough immutable detail in SnapshotJSON to reproduce the actual request.
func (s *Store) BuildAuthorRecognitionContext(ctx context.Context, documentID, overrideProfileID string) (AuthorRecognitionContext, error) {
	profileID := strings.TrimSpace(overrideProfileID)
	if profileID == "" {
		var linked sql.NullString
		if err := s.db.QueryRowContext(ctx, `SELECT author_profile_id FROM documents WHERE id = ?`, documentID).Scan(&linked); err != nil {
			return AuthorRecognitionContext{}, err
		}
		if linked.Valid {
			profileID = linked.String
		}
	}
	if profileID == "" {
		return AuthorRecognitionContext{}, nil
	}
	profile, err := s.GetAuthorProfile(ctx, profileID)
	if err != nil {
		return AuthorRecognitionContext{}, err
	}
	terms, err := s.ListAuthorTerms(ctx, profileID)
	if err != nil {
		return AuthorRecognitionContext{}, err
	}
	if len(terms) > 100 {
		terms = terms[:100]
	}
	corrections, err := s.ListAuthorCorrections(ctx, profileID, 20)
	if err != nil {
		return AuthorRecognitionContext{}, err
	}

	examples := make([]authorCorrectionExample, 0, len(corrections))
	for _, correction := range corrections {
		examples = append(examples, authorCorrectionExample{
			CorrectionID: correction.ID,
			Source:       promptExcerpt(correction.SourceText, 160),
			Corrected:    promptExcerpt(correction.CorrectedText, 160),
		})
	}

	var prompt strings.Builder
	prompt.WriteString("\n\n【作者笔迹档案上下文】\n")
	fmt.Fprintf(&prompt, "作者：%s\n", profile.Name)
	if strings.TrimSpace(profile.Notes) != "" {
		fmt.Fprintf(&prompt, "档案说明：%s\n", strings.TrimSpace(profile.Notes))
	}
	if len(terms) > 0 {
		prompt.WriteString("专有词与常见误识别（只在图像证据支持时使用，不得据此补写）：\n")
		for _, term := range terms {
			if term.Replacement == "" {
				fmt.Fprintf(&prompt, "- %s", term.Term)
			} else {
				fmt.Fprintf(&prompt, "- 常见误识别“%s”应核对为“%s”", term.Replacement, term.Term)
			}
			if term.Note != "" {
				fmt.Fprintf(&prompt, "（%s）", term.Note)
			}
			prompt.WriteByte('\n')
		}
	}
	if len(examples) > 0 {
		prompt.WriteString("历史校对样例（仅用于辨认该作者字形，不得复制不可见内容）：\n")
		for _, example := range examples {
			fmt.Fprintf(&prompt, "- %s → %s\n", example.Source, example.Corrected)
		}
	}
	promptText := strings.TrimSpace(prompt.String())
	hash := sha256.Sum256([]byte(promptText))
	snapshot := authorRecognitionSnapshot{
		ProfileID:          profile.ID,
		ProfileName:        profile.Name,
		Notes:              profile.Notes,
		Terms:              terms,
		CorrectionExamples: examples,
		ContextSHA256:      hex.EncodeToString(hash[:]),
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return AuthorRecognitionContext{}, err
	}
	return AuthorRecognitionContext{ProfileID: profile.ID, PromptContext: promptText, SnapshotJSON: string(raw)}, nil
}

func promptExcerpt(value string, maxRunes int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes]) + "…"
}
