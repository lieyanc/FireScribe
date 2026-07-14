ALTER TABLE recognition_runs ADD COLUMN input_source TEXT NOT NULL DEFAULT 'original';

CREATE TABLE page_processing_runs (
  id TEXT PRIMARY KEY,
  document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  config_json TEXT NOT NULL DEFAULT '{}',
  status TEXT NOT NULL,
  total_pages INTEGER NOT NULL DEFAULT 0,
  done_pages INTEGER NOT NULL DEFAULT 0,
  failed_pages INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT,
  UNIQUE(job_id)
);

CREATE TABLE page_processing_results (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES page_processing_runs(id) ON DELETE CASCADE,
  page_id TEXT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
  source_asset_id TEXT NOT NULL REFERENCES assets(id) ON DELETE RESTRICT,
  output_asset_id TEXT REFERENCES assets(id) ON DELETE SET NULL,
  status TEXT NOT NULL,
  config_json TEXT NOT NULL DEFAULT '{}',
  metadata_json TEXT NOT NULL DEFAULT '{}',
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT,
  UNIQUE(run_id, page_id)
);

CREATE TABLE page_segments (
  id TEXT PRIMARY KEY,
  page_id TEXT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
  processing_result_id TEXT NOT NULL REFERENCES page_processing_results(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  position INTEGER NOT NULL DEFAULT 0,
  x INTEGER NOT NULL,
  y INTEGER NOT NULL,
  width INTEGER NOT NULL,
  height INTEGER NOT NULL,
  label TEXT NOT NULL DEFAULT '',
  metadata_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL
);

CREATE INDEX idx_page_processing_runs_document
ON page_processing_runs(document_id, created_at DESC);

CREATE INDEX idx_page_processing_results_page
ON page_processing_results(page_id, created_at DESC);

CREATE INDEX idx_page_processing_results_run_status
ON page_processing_results(run_id, status);

CREATE INDEX idx_page_segments_page
ON page_segments(page_id, processing_result_id, position);

CREATE UNIQUE INDEX idx_page_processing_single_active_document
ON page_processing_runs(document_id)
WHERE status IN ('queued', 'running');
