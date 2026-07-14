ALTER TABLE jobs ADD COLUMN progress_current INTEGER NOT NULL DEFAULT 0;
ALTER TABLE jobs ADD COLUMN progress_total INTEGER NOT NULL DEFAULT 0;
ALTER TABLE jobs ADD COLUMN progress_message TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN result_json TEXT NOT NULL DEFAULT '{}';

CREATE TABLE exports (
  id TEXT PRIMARY KEY,
  document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  format TEXT NOT NULL,
  include_page_numbers INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL,
  asset_id TEXT REFERENCES assets(id) ON DELETE SET NULL,
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  finished_at TEXT
);

CREATE INDEX idx_exports_document ON exports(document_id, created_at);
CREATE UNIQUE INDEX idx_exports_job ON exports(job_id);
CREATE UNIQUE INDEX idx_jobs_single_active_search_rebuild
ON jobs(type)
WHERE type = 'rebuild_search_index' AND status IN ('queued', 'running');
