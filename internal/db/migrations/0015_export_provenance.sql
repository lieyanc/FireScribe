CREATE TABLE export_page_snapshots (
  export_id TEXT NOT NULL REFERENCES exports(id) ON DELETE CASCADE,
  ordinal INTEGER NOT NULL,
  document_id TEXT NOT NULL,
  document_title TEXT NOT NULL,
  document_position INTEGER NOT NULL DEFAULT 0,
  page_id TEXT NOT NULL,
  page_no INTEGER NOT NULL,
  text_version_id TEXT NOT NULL,
  text_version_kind TEXT NOT NULL,
  annotations_json TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL,
  PRIMARY KEY (export_id, ordinal)
);

CREATE INDEX idx_export_page_snapshots_page
ON export_page_snapshots(page_id, export_id);

CREATE TABLE project_export_page_snapshots (
  project_export_id TEXT NOT NULL REFERENCES project_exports(id) ON DELETE CASCADE,
  ordinal INTEGER NOT NULL,
  document_id TEXT NOT NULL,
  document_title TEXT NOT NULL,
  document_position INTEGER NOT NULL,
  page_id TEXT NOT NULL,
  page_no INTEGER NOT NULL,
  text_version_id TEXT NOT NULL,
  text_version_kind TEXT NOT NULL,
  annotations_json TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL,
  PRIMARY KEY (project_export_id, ordinal)
);

CREATE INDEX idx_project_export_page_snapshots_page
ON project_export_page_snapshots(page_id, project_export_id);
