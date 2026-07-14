CREATE TABLE author_profiles (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL COLLATE NOCASE UNIQUE,
  notes TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

ALTER TABLE documents
ADD COLUMN author_profile_id TEXT REFERENCES author_profiles(id) ON DELETE SET NULL;

CREATE TABLE author_terms (
  id TEXT PRIMARY KEY,
  author_profile_id TEXT NOT NULL REFERENCES author_profiles(id) ON DELETE CASCADE,
  term TEXT NOT NULL,
  replacement TEXT NOT NULL DEFAULT '',
  note TEXT NOT NULL DEFAULT '',
  weight REAL NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(author_profile_id, term, replacement)
);

CREATE TABLE author_corrections (
  id TEXT PRIMARY KEY,
  author_profile_id TEXT NOT NULL REFERENCES author_profiles(id) ON DELETE CASCADE,
  document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  page_id TEXT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
  text_version_id TEXT NOT NULL REFERENCES text_versions(id) ON DELETE CASCADE UNIQUE,
  source_result_id TEXT REFERENCES recognition_results(id) ON DELETE SET NULL,
  source_text TEXT NOT NULL,
  corrected_text TEXT NOT NULL,
  kind TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX idx_documents_author_profile ON documents(author_profile_id);
CREATE INDEX idx_author_terms_profile ON author_terms(author_profile_id, weight DESC, updated_at DESC);
CREATE INDEX idx_author_corrections_profile ON author_corrections(author_profile_id, created_at DESC);
