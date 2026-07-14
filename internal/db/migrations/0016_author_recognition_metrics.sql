CREATE TABLE author_correction_metrics (
  correction_id TEXT PRIMARY KEY REFERENCES author_corrections(id) ON DELETE CASCADE,
  source_char_count INTEGER NOT NULL CHECK(source_char_count >= 0),
  reference_char_count INTEGER NOT NULL CHECK(reference_char_count >= 0),
  edit_distance INTEGER NOT NULL CHECK(edit_distance >= 0),
  substitution_count INTEGER NOT NULL CHECK(substitution_count >= 0),
  omission_count INTEGER NOT NULL CHECK(omission_count >= 0),
  addition_count INTEGER NOT NULL CHECK(addition_count >= 0),
  error_patterns_json TEXT NOT NULL DEFAULT '[]',
  algorithm_version INTEGER NOT NULL DEFAULT 1,
  computed_at TEXT NOT NULL
);
