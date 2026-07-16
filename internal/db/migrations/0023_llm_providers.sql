-- LLM providers (API interfaces) own base_url + credentials.
-- recognizer_profiles become models under a provider (same IDs keep run FKs).

CREATE TABLE llm_providers (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL COLLATE NOCASE UNIQUE,
  driver TEXT NOT NULL CHECK(driver IN ('openai-compatible', 'mock')),
  base_url TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX idx_llm_providers_updated
ON llm_providers(updated_at DESC, name);

ALTER TABLE recognizer_profiles
ADD COLUMN provider_id TEXT REFERENCES llm_providers(id) ON DELETE CASCADE;

CREATE INDEX idx_recognizer_profiles_provider
ON recognizer_profiles(provider_id, updated_at DESC);
