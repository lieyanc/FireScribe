CREATE TABLE provider_adapters (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL COLLATE NOCASE UNIQUE,
  engine TEXT NOT NULL CHECK(engine = 'generic-http-json'),
  endpoint TEXT NOT NULL,
  model TEXT NOT NULL,
  auth_type TEXT NOT NULL DEFAULT 'none' CHECK(auth_type IN ('none', 'bearer', 'x-api-key')),
  secret TEXT NOT NULL DEFAULT '',
  timeout_seconds INTEGER NOT NULL DEFAULT 120 CHECK(timeout_seconds BETWEEN 5 AND 3600),
  request_config_json TEXT NOT NULL DEFAULT '{}',
  response_config_json TEXT NOT NULL DEFAULT '{}',
  is_enabled INTEGER NOT NULL DEFAULT 1 CHECK(is_enabled IN (0, 1)),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX idx_provider_adapters_updated
ON provider_adapters(is_enabled DESC, updated_at DESC, name);

ALTER TABLE recognition_runs
ADD COLUMN provider_adapter_id TEXT REFERENCES provider_adapters(id) ON DELETE SET NULL;

CREATE TABLE recognition_experiments (
  id TEXT PRIMARY KEY,
  document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  job_id TEXT NOT NULL UNIQUE REFERENCES jobs(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  page_ids_json TEXT NOT NULL,
  status TEXT NOT NULL,
  winner_variant_id TEXT,
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT
);

CREATE TABLE recognition_experiment_variants (
  id TEXT PRIMARY KEY,
  experiment_id TEXT NOT NULL REFERENCES recognition_experiments(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  recognizer_profile_id TEXT,
  provider_adapter_id TEXT,
  prompt_version_id TEXT,
  image_source TEXT NOT NULL DEFAULT 'original' CHECK(image_source IN ('original', 'enhanced')),
  position INTEGER NOT NULL,
  status TEXT NOT NULL DEFAULT 'queued',
  run_ids_json TEXT NOT NULL DEFAULT '[]',
  avg_confidence REAL,
  duration_ms INTEGER NOT NULL DEFAULT 0,
  manual_edit_distance INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT,
  CHECK((recognizer_profile_id IS NULL) OR (provider_adapter_id IS NULL)),
  UNIQUE(experiment_id, position)
);

CREATE INDEX idx_recognition_experiments_document
ON recognition_experiments(document_id, created_at DESC);

CREATE INDEX idx_recognition_experiment_variants_experiment
ON recognition_experiment_variants(experiment_id, position);
