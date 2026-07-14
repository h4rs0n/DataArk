-- +goose Up

ALTER TABLE recommendation_items
    ADD COLUMN IF NOT EXISTS snapshot_processing_state VARCHAR(32) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS snapshot_eligibility_state VARCHAR(32) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS snapshot_dedupe_state VARCHAR(32) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS snapshot_cluster_id VARCHAR(128) NOT NULL DEFAULT '';

UPDATE recommendation_items AS item
SET snapshot_processing_state = COALESCE(candidate.processing_state, ''),
    snapshot_eligibility_state = COALESCE(candidate.eligibility_state, ''),
    snapshot_dedupe_state = COALESCE(candidate.dedupe_state, ''),
    snapshot_cluster_id = COALESCE(candidate.duplicate_cluster_id, '')
FROM discovery_candidates AS candidate
WHERE candidate.id = item.candidate_id
  AND item.snapshot_processing_state = '';

-- +goose Down

-- Data-preserving rollback: publication-time integrity evidence remains.
SELECT 1;
