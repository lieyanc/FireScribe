CREATE TABLE candidate_merge_segments (
  id TEXT PRIMARY KEY,
  candidate_merge_id TEXT NOT NULL REFERENCES candidate_merges(id) ON DELETE CASCADE,
  ordinal INTEGER NOT NULL,
  source_result_id TEXT NOT NULL REFERENCES recognition_results(id) ON DELETE RESTRICT,
  source_start INTEGER NOT NULL,
  source_end INTEGER NOT NULL,
  output_start INTEGER NOT NULL,
  output_end INTEGER NOT NULL,
  text TEXT NOT NULL,
  UNIQUE(candidate_merge_id, ordinal)
);

CREATE INDEX idx_candidate_merge_segments_merge
ON candidate_merge_segments(candidate_merge_id, ordinal);
