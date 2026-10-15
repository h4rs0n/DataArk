-- +goose Up
-- Existing batches keep their actual JSON Object requests; only newly built
-- batches use JSON Schema, including within an already authorized run.
ALTER TABLE assessment_batches ADD COLUMN response_mode text NOT NULL DEFAULT 'json_object'
    CHECK (response_mode IN ('json_object','json_schema'));

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM assessment_batch_runs WHERE active_key=1) THEN
  RAISE EXCEPTION 'Finish or reset active Batch runs before rollback';
 END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE assessment_batches DROP COLUMN response_mode;
