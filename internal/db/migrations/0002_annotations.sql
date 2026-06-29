CREATE TABLE annotations (
  id TEXT PRIMARY KEY,
  document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  page_id TEXT REFERENCES pages(id) ON DELETE CASCADE,
  text_version_id TEXT REFERENCES text_versions(id) ON DELETE SET NULL,
  kind TEXT NOT NULL,
  status TEXT NOT NULL,
  body TEXT NOT NULL,
  anchor_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX idx_annotations_doc ON annotations(document_id, created_at);
CREATE INDEX idx_annotations_page ON annotations(page_id, created_at);
CREATE INDEX idx_annotations_status ON annotations(status);
