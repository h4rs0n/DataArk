-- +goose Up

ALTER TABLE recommendation_items
    ADD COLUMN IF NOT EXISTS content_version BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS content_updated BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS cooldown_repeat BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_recommendation_items_content_updated ON recommendation_items(content_updated);
CREATE INDEX IF NOT EXISTS idx_recommendation_items_cooldown_repeat ON recommendation_items(cooldown_repeat);

-- The existing per-day candidate uniqueness remains the hard identity guard.
-- Cross-day history is interpreted by cooldown and content_version, not by a
-- permanent user/candidate uniqueness constraint.

-- +goose Down

-- Data-preserving rollback: published item version and recurrence evidence is
-- retained while an older binary ignores these additive columns.
SELECT 1;
