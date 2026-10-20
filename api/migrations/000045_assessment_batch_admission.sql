-- +goose Up
ALTER TABLE assessment_batch_runs ADD COLUMN admission_enabled boolean NOT NULL DEFAULT false;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM assessment_batch_runs WHERE active_key=1) THEN
  RAISE EXCEPTION 'Finish or reset active Batch runs before rollback';
 END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE assessment_batch_runs DROP COLUMN admission_enabled;
