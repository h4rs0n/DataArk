-- +goose Up
-- +goose StatementBegin

CREATE TABLE IF NOT EXISTS recommendation_feed_batches (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    status VARCHAR(32) NOT NULL,
    requested_count INTEGER NOT NULL DEFAULT 10,
    actual_count INTEGER NOT NULL DEFAULT 0,
    policy_version VARCHAR(64) NOT NULL DEFAULT 'discovery-feed-v1',
    profile_version BIGINT NOT NULL DEFAULT 0,
    shortage_reasons TEXT NOT NULL DEFAULT '',
    replaced_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_recommendation_feed_batches_user_created
ON recommendation_feed_batches(user_id, created_at DESC, id DESC);

CREATE UNIQUE INDEX IF NOT EXISTS idx_recommendation_feed_batches_active_user
ON recommendation_feed_batches(user_id)
WHERE status = 'active';

ALTER TABLE recommendation_items
    ALTER COLUMN day_id DROP NOT NULL,
    ADD COLUMN IF NOT EXISTS feed_batch_id BIGINT REFERENCES recommendation_feed_batches(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS idx_recommendation_items_feed_batch_id
ON recommendation_items(feed_batch_id);

CREATE UNIQUE INDEX IF NOT EXISTS idx_recommendation_items_feed_candidate
ON recommendation_items(feed_batch_id, candidate_id)
WHERE feed_batch_id IS NOT NULL;

ALTER TABLE recommendation_items
    DROP CONSTRAINT IF EXISTS recommendation_items_exactly_one_parent,
    ADD CONSTRAINT recommendation_items_exactly_one_parent CHECK (
        (day_id IS NOT NULL AND feed_batch_id IS NULL)
        OR (day_id IS NULL AND feed_batch_id IS NOT NULL)
    );

-- +goose StatementEnd

-- +goose Down

-- Data-preserving rollback: feed batches, item ownership, and exposure audit
-- remain available to newer binaries while older binaries ignore them.
SELECT 1;
