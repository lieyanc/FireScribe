ALTER TABLE recognition_results
ADD COLUMN metadata_json TEXT NOT NULL DEFAULT '{}';
