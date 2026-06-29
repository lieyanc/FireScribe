CREATE TABLE documents (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  author TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  page_count INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE assets (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  sha256 TEXT NOT NULL,
  original_name TEXT NOT NULL,
  mime_type TEXT NOT NULL,
  byte_size INTEGER NOT NULL,
  storage_path TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(kind, sha256)
);

CREATE TABLE document_assets (
  document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  asset_id TEXT NOT NULL REFERENCES assets(id) ON DELETE RESTRICT,
  role TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY (document_id, asset_id, role)
);

CREATE TABLE pages (
  id TEXT PRIMARY KEY,
  document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  page_no INTEGER NOT NULL,
  image_asset_id TEXT REFERENCES assets(id) ON DELETE SET NULL,
  thumb_asset_id TEXT REFERENCES assets(id) ON DELETE SET NULL,
  width INTEGER NOT NULL DEFAULT 0,
  height INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(document_id, page_no)
);

CREATE TABLE recognition_runs (
  id TEXT PRIMARY KEY,
  document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  provider TEXT NOT NULL,
  model TEXT NOT NULL,
  prompt_version TEXT NOT NULL DEFAULT '',
  config_json TEXT NOT NULL DEFAULT '{}',
  status TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT,
  created_at TEXT NOT NULL
);

CREATE TABLE recognition_results (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES recognition_runs(id) ON DELETE CASCADE,
  page_id TEXT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
  text TEXT NOT NULL,
  confidence REAL,
  raw_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  UNIQUE(run_id, page_id)
);

CREATE TABLE text_versions (
  id TEXT PRIMARY KEY,
  document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  page_id TEXT REFERENCES pages(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  base_version_id TEXT REFERENCES text_versions(id) ON DELETE SET NULL,
  source_result_id TEXT REFERENCES recognition_results(id) ON DELETE SET NULL,
  text TEXT NOT NULL,
  status TEXT NOT NULL,
  created_by TEXT NOT NULL DEFAULT 'system',
  created_at TEXT NOT NULL
);

CREATE TABLE jobs (
  id TEXT PRIMARY KEY,
  type TEXT NOT NULL,
  status TEXT NOT NULL,
  target_type TEXT NOT NULL,
  target_id TEXT NOT NULL,
  payload_json TEXT NOT NULL DEFAULT '{}',
  attempts INTEGER NOT NULL DEFAULT 0,
  max_attempts INTEGER NOT NULL DEFAULT 3,
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT
);

CREATE TABLE tags (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  color TEXT NOT NULL DEFAULT ''
);

CREATE TABLE document_tags (
  document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  tag_id TEXT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
  PRIMARY KEY (document_id, tag_id)
);

CREATE VIRTUAL TABLE text_search USING fts5(
  document_id UNINDEXED,
  page_id UNINDEXED,
  text_version_id UNINDEXED,
  title,
  body,
  tokenize = 'trigram'
);

CREATE INDEX idx_assets_kind_sha ON assets(kind, sha256);
CREATE INDEX idx_document_assets_doc ON document_assets(document_id);
CREATE INDEX idx_pages_doc ON pages(document_id, page_no);
CREATE INDEX idx_recognition_results_page ON recognition_results(page_id);
CREATE INDEX idx_recognition_runs_doc ON recognition_runs(document_id);
CREATE INDEX idx_text_versions_page ON text_versions(page_id);
CREATE INDEX idx_text_versions_doc ON text_versions(document_id);
CREATE INDEX idx_jobs_status ON jobs(status, created_at);

CREATE VIEW page_details AS
SELECT
  p.id            AS page_id,
  p.document_id,
  p.page_no,
  p.status        AS page_status,
  p.width,
  p.height,
  p.image_asset_id,
  p.thumb_asset_id,
  (SELECT COUNT(*)          FROM recognition_results r WHERE r.page_id = p.id) AS recognition_count,
  (SELECT MAX(r.confidence) FROM recognition_results r WHERE r.page_id = p.id) AS best_confidence,
  (SELECT run.provider FROM recognition_results r
     JOIN recognition_runs run ON run.id = r.run_id
     WHERE r.page_id = p.id ORDER BY r.created_at DESC LIMIT 1) AS last_provider,
  (SELECT run.model FROM recognition_results r
     JOIN recognition_runs run ON run.id = r.run_id
     WHERE r.page_id = p.id ORDER BY r.created_at DESC LIMIT 1) AS last_model,
  (SELECT MAX(r.created_at)  FROM recognition_results r WHERE r.page_id = p.id) AS last_recognized_at,
  EXISTS(SELECT 1 FROM text_versions v WHERE v.page_id = p.id AND v.kind = 'candidate') AS has_candidate,
  EXISTS(SELECT 1 FROM text_versions v WHERE v.page_id = p.id AND v.kind = 'manual') AS has_manual,
  EXISTS(SELECT 1 FROM text_versions v WHERE v.page_id = p.id AND v.kind = 'final') AS has_final,
  p.updated_at
FROM pages p;
