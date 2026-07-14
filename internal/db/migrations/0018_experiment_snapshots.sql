ALTER TABLE recognition_experiment_variants
ADD COLUMN snapshot_json TEXT NOT NULL DEFAULT '{}';
