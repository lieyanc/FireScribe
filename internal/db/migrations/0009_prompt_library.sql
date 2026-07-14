CREATE TABLE prompt_versions (
  id TEXT PRIMARY KEY,
  version TEXT NOT NULL,
  content TEXT NOT NULL,
  sha256 TEXT NOT NULL,
  is_active INTEGER NOT NULL DEFAULT 0 CHECK (is_active IN (0, 1)),
  created_at TEXT NOT NULL,
  activated_at TEXT,
  UNIQUE(version, sha256)
);

CREATE INDEX idx_prompt_versions_created
ON prompt_versions(created_at DESC);

CREATE UNIQUE INDEX idx_prompt_versions_active
ON prompt_versions(is_active)
WHERE is_active = 1;
