-- +goose Up

ALTER TABLE recommendation_items
    ADD COLUMN IF NOT EXISTS snapshot_topics TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS snapshot_content_type VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS snapshot_style VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS snapshot_language VARCHAR(32) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS snapshot_word_count INTEGER NOT NULL DEFAULT 0;

UPDATE recommendation_items AS item
SET snapshot_topics = COALESCE(candidate.topics::text, ''),
    snapshot_content_type = COALESCE(candidate.content_type, ''),
    snapshot_style = COALESCE(candidate.content_style, ''),
    snapshot_language = COALESCE(candidate.language, ''),
    snapshot_word_count = COALESCE(candidate.word_count, 0)
FROM discovery_candidates AS candidate
WHERE candidate.id = item.candidate_id
  AND item.snapshot_topics = '';

-- +goose Down

-- Data-preserving rollback: frozen display context remains for newer binaries.
SELECT 1;
