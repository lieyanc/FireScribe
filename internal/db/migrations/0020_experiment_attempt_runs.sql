ALTER TABLE recognition_experiment_variants
ADD COLUMN current_run_ids_json TEXT NOT NULL DEFAULT '[]';

UPDATE recognition_experiment_variants
SET current_run_ids_json = run_ids_json;
