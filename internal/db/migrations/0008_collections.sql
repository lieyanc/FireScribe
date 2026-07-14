CREATE TABLE projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE project_documents (
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  position INTEGER NOT NULL DEFAULT 0,
  added_at TEXT NOT NULL,
  PRIMARY KEY (project_id, document_id)
);

CREATE INDEX idx_project_documents_order
ON project_documents(project_id, position, added_at);

CREATE INDEX idx_project_documents_document
ON project_documents(document_id);

CREATE TABLE project_exports (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  format TEXT NOT NULL,
  include_page_numbers INTEGER NOT NULL DEFAULT 0,
  text_scope TEXT NOT NULL DEFAULT 'current',
  include_annotations INTEGER NOT NULL DEFAULT 0,
  include_uncertain INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL,
  asset_id TEXT REFERENCES assets(id) ON DELETE SET NULL,
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  finished_at TEXT
);

CREATE INDEX idx_project_exports_project
ON project_exports(project_id, created_at);

CREATE UNIQUE INDEX idx_project_exports_job
ON project_exports(job_id);
