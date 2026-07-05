ALTER TABLE recognition_runs ADD COLUMN total_pages INTEGER NOT NULL DEFAULT 0;
ALTER TABLE recognition_runs ADD COLUMN done_pages INTEGER NOT NULL DEFAULT 0;
ALTER TABLE recognition_runs ADD COLUMN failed_pages INTEGER NOT NULL DEFAULT 0;
ALTER TABLE recognition_runs ADD COLUMN error TEXT NOT NULL DEFAULT '';

CREATE TABLE run_pages (
  run_id TEXT NOT NULL REFERENCES recognition_runs(id) ON DELETE CASCADE,
  page_id TEXT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
  page_no INTEGER NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  attempts INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '',
  started_at TEXT,
  finished_at TEXT,
  PRIMARY KEY (run_id, page_id)
);

CREATE INDEX idx_run_pages_run ON run_pages(run_id, page_no);
CREATE INDEX idx_run_pages_status ON run_pages(run_id, status);
