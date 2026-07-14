CREATE TABLE cross_checks (
  id TEXT PRIMARY KEY,
  document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  job_id TEXT NOT NULL UNIQUE REFERENCES jobs(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  page_ids_json TEXT NOT NULL,
  merge_profile_id TEXT REFERENCES recognizer_profiles(id) ON DELETE SET NULL,
  status TEXT NOT NULL,
  error TEXT NOT NULL DEFAULT '',
  consensus_pages INTEGER NOT NULL DEFAULT 0,
  disagreement_pages INTEGER NOT NULL DEFAULT 0,
  failed_pages INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT
);

CREATE INDEX idx_cross_checks_document ON cross_checks(document_id, created_at DESC);

CREATE TABLE cross_check_variants (
  id TEXT PRIMARY KEY,
  cross_check_id TEXT NOT NULL REFERENCES cross_checks(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  recognizer_profile_id TEXT,
  provider_adapter_id TEXT,
  prompt_version_id TEXT,
  snapshot_json TEXT NOT NULL DEFAULT '{}',
  image_source TEXT NOT NULL DEFAULT 'original' CHECK(image_source IN ('original', 'enhanced')),
  position INTEGER NOT NULL,
  status TEXT NOT NULL DEFAULT 'queued',
  run_id TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT,
  CHECK((recognizer_profile_id IS NULL) OR (provider_adapter_id IS NULL)),
  UNIQUE(cross_check_id, position)
);

CREATE INDEX idx_cross_check_variants_check ON cross_check_variants(cross_check_id, position);

CREATE TABLE cross_check_pages (
  cross_check_id TEXT NOT NULL REFERENCES cross_checks(id) ON DELETE CASCADE,
  page_id TEXT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
  page_no INTEGER NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  agreement REAL,
  result_ids_json TEXT NOT NULL DEFAULT '[]',
  consensus_version_id TEXT REFERENCES text_versions(id) ON DELETE SET NULL,
  merged_version_id TEXT REFERENCES text_versions(id) ON DELETE SET NULL,
  annotation_id TEXT REFERENCES annotations(id) ON DELETE SET NULL,
  conflicts_json TEXT NOT NULL DEFAULT '[]',
  adopted_version_id TEXT REFERENCES text_versions(id) ON DELETE SET NULL,
  adopted_at TEXT,
  error TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (cross_check_id, page_id)
);

CREATE INDEX idx_cross_check_pages_page ON cross_check_pages(page_id);

-- Adoption resolves the candidate created from a consensus recognition result.
CREATE INDEX idx_text_versions_source_result ON text_versions(source_result_id);
