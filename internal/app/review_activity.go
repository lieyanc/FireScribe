package app

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

type ReviewActivitySession struct {
	ID            string  `json:"id"`
	DocumentID    string  `json:"document_id"`
	PageID        string  `json:"page_id"`
	ActiveSeconds float64 `json:"active_seconds"`
	StartedAt     string  `json:"started_at"`
	UpdatedAt     string  `json:"updated_at"`
	FinishedAt    string  `json:"finished_at"`
}

func (s *Store) RecordReviewActivity(ctx context.Context, pageID, sessionID string, activeSeconds float64, finished bool) (ReviewActivitySession, error) {
	pageID = strings.TrimSpace(pageID)
	sessionID = strings.TrimSpace(sessionID)
	if pageID == "" || sessionID == "" {
		return ReviewActivitySession{}, errors.New("page_id and session_id are required")
	}
	if activeSeconds < 0 || activeSeconds > 24*60*60 {
		return ReviewActivitySession{}, errors.New("active_seconds must be between 0 and 86400")
	}
	page, err := s.GetPage(ctx, pageID)
	if err != nil {
		return ReviewActivitySession{}, err
	}
	timestamp := now()
	finishedAt := ""
	if finished {
		finishedAt = timestamp
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO review_activity_sessions(id, document_id, page_id, active_seconds, started_at, updated_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, NULLIF(?, ''))
		ON CONFLICT(id) DO UPDATE SET
		  active_seconds = MAX(review_activity_sessions.active_seconds, excluded.active_seconds),
		  updated_at = excluded.updated_at,
		  finished_at = CASE WHEN excluded.finished_at IS NOT NULL THEN excluded.finished_at ELSE review_activity_sessions.finished_at END
		WHERE review_activity_sessions.page_id = excluded.page_id
		  AND review_activity_sessions.finished_at IS NULL
	`, sessionID, page.DocumentID, page.ID, activeSeconds, timestamp, timestamp, finishedAt)
	if err != nil {
		return ReviewActivitySession{}, err
	}
	var item ReviewActivitySession
	var terminal sql.NullString
	err = s.db.QueryRowContext(ctx, `
		SELECT id, document_id, page_id, active_seconds, started_at, updated_at, finished_at
		FROM review_activity_sessions WHERE id = ? AND page_id = ?
	`, sessionID, pageID).Scan(&item.ID, &item.DocumentID, &item.PageID, &item.ActiveSeconds, &item.StartedAt, &item.UpdatedAt, &terminal)
	if terminal.Valid {
		item.FinishedAt = terminal.String
	}
	return item, err
}
