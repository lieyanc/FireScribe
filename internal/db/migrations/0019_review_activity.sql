CREATE TABLE review_activity_sessions (
  id TEXT PRIMARY KEY,
  document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  page_id TEXT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
  active_seconds REAL NOT NULL DEFAULT 0 CHECK(active_seconds >= 0),
  started_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  finished_at TEXT
);

CREATE INDEX idx_review_activity_page
ON review_activity_sessions(page_id, started_at, updated_at);
