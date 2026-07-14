CREATE TABLE recognizer_profiles (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  driver TEXT NOT NULL CHECK(driver IN ('openai-compatible', 'mock')),
  base_url TEXT NOT NULL DEFAULT '',
  api_key TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL DEFAULT '',
  params_json TEXT NOT NULL DEFAULT '{}',
  prompt_version_id TEXT REFERENCES prompt_versions(id) ON DELETE SET NULL,
  is_default INTEGER NOT NULL DEFAULT 0 CHECK(is_default IN (0, 1)),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE UNIQUE INDEX idx_recognizer_profiles_default
ON recognizer_profiles(is_default)
WHERE is_default = 1;

CREATE INDEX idx_recognizer_profiles_updated
ON recognizer_profiles(updated_at DESC);

ALTER TABLE recognition_runs ADD COLUMN recognizer_profile_id TEXT REFERENCES recognizer_profiles(id) ON DELETE SET NULL;
ALTER TABLE recognition_runs ADD COLUMN recognizer_driver TEXT NOT NULL DEFAULT '';
ALTER TABLE recognition_runs ADD COLUMN profile_snapshot_json TEXT NOT NULL DEFAULT '{}';

CREATE TABLE candidate_merges (
  id TEXT PRIMARY KEY,
  page_id TEXT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
  text_version_id TEXT NOT NULL UNIQUE REFERENCES text_versions(id) ON DELETE CASCADE,
  source_result_ids_json TEXT NOT NULL,
  recognizer_profile_id TEXT REFERENCES recognizer_profiles(id) ON DELETE SET NULL,
  driver TEXT NOT NULL,
  prompt_version TEXT NOT NULL,
  prompt_hash TEXT NOT NULL,
  raw_response TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL
);

CREATE INDEX idx_candidate_merges_page
ON candidate_merges(page_id, created_at DESC);
