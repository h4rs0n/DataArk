-- +goose Up
CREATE TABLE assessment_batch_runs (
 id bigserial PRIMARY KEY, active_key integer UNIQUE CHECK (active_key=1), status text NOT NULL,
 base_url text NOT NULL, model text NOT NULL, policy_version text NOT NULL,
 high_water bigint NOT NULL DEFAULT 0, cursor bigint NOT NULL DEFAULT 0,
 created_at timestamptz NOT NULL, lease_until timestamptz, finished_at timestamptz
);
CREATE TABLE assessment_batches (
 id bigserial PRIMARY KEY, run_id bigint NOT NULL REFERENCES assessment_batch_runs(id), uuid text NOT NULL UNIQUE,
 model text NOT NULL, phase text NOT NULL, provider_status text NOT NULL DEFAULT '',
 input_file_id text NOT NULL DEFAULT '', output_file_id text NOT NULL DEFAULT '', error_file_id text NOT NULL DEFAULT '', remote_id text NOT NULL DEFAULT '',
 input_jsonl bytea, digest text NOT NULL DEFAULT '', file_bytes bigint NOT NULL DEFAULT 0, total integer NOT NULL DEFAULT 0, completed integer NOT NULL DEFAULT 0, failed integer NOT NULL DEFAULT 0,
 error text NOT NULL DEFAULT '', failure_count integer NOT NULL DEFAULT 0, next_check_at timestamptz NOT NULL, created_at timestamptz NOT NULL,
 submitted_at timestamptz, expires_at timestamptz, finished_at timestamptz, cleaned_at timestamptz
);
CREATE INDEX idx_batches_due ON assessment_batches(next_check_at) WHERE phase <> 'done';
CREATE INDEX idx_batches_run ON assessment_batches(run_id);
CREATE TABLE assessment_batch_items (
 id bigserial PRIMARY KEY, run_id bigint NOT NULL REFERENCES assessment_batch_runs(id), batch_id bigint REFERENCES assessment_batches(id),
 job_id text NOT NULL, material_id bigint NOT NULL, version_id bigint NOT NULL DEFAULT 0, content_version bigint NOT NULL, candidate_id bigint NOT NULL DEFAULT 0,
 custom_id text NOT NULL UNIQUE, active_identity text UNIQUE, status text NOT NULL,
 assessment_id bigint REFERENCES discovery_article_assessments(id), evidence_tokens integer NOT NULL DEFAULT 0,
 prompt_tokens integer NOT NULL DEFAULT 0, completion_tokens integer NOT NULL DEFAULT 0, reasoning_tokens integer NOT NULL DEFAULT 0, total_tokens integer NOT NULL DEFAULT 0,
 error text NOT NULL DEFAULT '', created_at timestamptz NOT NULL, finished_at timestamptz
);
CREATE INDEX idx_batch_items_run ON assessment_batch_items(run_id,status);
CREATE INDEX idx_batch_items_batch ON assessment_batch_items(batch_id,id);
ALTER TABLE assessment_llm_calls ADD COLUMN batch_item_id bigint UNIQUE REFERENCES assessment_batch_items(id);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM assessment_batch_runs WHERE active_key=1) THEN
  RAISE EXCEPTION 'Finish and import active Batch runs before rollback';
 END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE assessment_llm_calls DROP COLUMN batch_item_id;
DROP TABLE assessment_batch_items;
DROP TABLE assessment_batches;
DROP TABLE assessment_batch_runs;
