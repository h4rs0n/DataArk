-- +goose Up

ALTER TABLE discovery_candidates
    ADD COLUMN IF NOT EXISTS metadata_confidence INTEGER NOT NULL DEFAULT 0;

-- +goose Down
-- Candidate metadata confidence and provenance remain useful after read-path rollback.
SELECT 1;
