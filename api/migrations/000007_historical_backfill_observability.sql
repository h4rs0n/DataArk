-- +goose Up

ALTER TABLE discovery_candidates
    ADD COLUMN IF NOT EXISTS published_confidence VARCHAR(32);

ALTER TABLE discovery_backfill_states
    ADD COLUMN IF NOT EXISTS last_batch_at TIMESTAMPTZ;

-- +goose Down
-- Historical coverage and publication-confidence evidence are retained on rollback.
SELECT 1;
