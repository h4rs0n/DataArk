-- +goose Up
ALTER TABLE assessment_batch_runs ADD COLUMN paused_at timestamptz;
ALTER TABLE assessment_batch_runs ADD COLUMN error_code integer NOT NULL DEFAULT 0;
ALTER TABLE assessment_batch_runs ADD COLUMN error text NOT NULL DEFAULT '';
ALTER TABLE assessment_batch_runs ADD COLUMN resume_status text NOT NULL DEFAULT '';
ALTER TABLE assessment_batch_items ADD COLUMN returned_at timestamptz;
ALTER TABLE assessment_batch_items ADD COLUMN restored_job_id text NOT NULL DEFAULT '';

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM assessment_batch_runs WHERE active_key=1) THEN
  RAISE EXCEPTION 'Finish or reset active Batch runs before rollback';
 END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE assessment_batch_items DROP COLUMN restored_job_id;
ALTER TABLE assessment_batch_items DROP COLUMN returned_at;
ALTER TABLE assessment_batch_runs DROP COLUMN resume_status;
ALTER TABLE assessment_batch_runs DROP COLUMN error;
ALTER TABLE assessment_batch_runs DROP COLUMN error_code;
ALTER TABLE assessment_batch_runs DROP COLUMN paused_at;
