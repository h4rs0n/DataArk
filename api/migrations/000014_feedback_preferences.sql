-- +goose Up

ALTER TABLE recommendation_settings
    ADD COLUMN IF NOT EXISTS preferred_topics TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS preferred_languages TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS preferred_length VARCHAR(32) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS preferred_depth DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS favorite_sources TEXT NOT NULL DEFAULT '';

ALTER TABLE recommendation_feedbacks
    ADD COLUMN IF NOT EXISTS is_current BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS current_key VARCHAR(128),
    ADD COLUMN IF NOT EXISTS supersedes_id BIGINT,
    ADD COLUMN IF NOT EXISTS closed_reason VARCHAR(32) NOT NULL DEFAULT '';

WITH ranked AS (
    SELECT id,
           ROW_NUMBER() OVER (PARTITION BY user_id, recommendation_item_id ORDER BY created_at DESC, id DESC) AS position
    FROM recommendation_feedbacks
    WHERE reverted_at IS NULL
)
UPDATE recommendation_feedbacks AS feedback
SET is_current = ranked.position = 1,
    current_key = CASE WHEN ranked.position = 1 THEN feedback.user_id::text || ':' || feedback.recommendation_item_id::text ELSE NULL END,
    closed_reason = CASE WHEN ranked.position = 1 THEN '' ELSE 'superseded' END
FROM ranked
WHERE ranked.id = feedback.id;

CREATE UNIQUE INDEX IF NOT EXISTS idx_recommendation_feedbacks_current_key ON recommendation_feedbacks(current_key);
CREATE INDEX IF NOT EXISTS idx_recommendation_feedbacks_is_current ON recommendation_feedbacks(is_current);
CREATE INDEX IF NOT EXISTS idx_recommendation_feedbacks_supersedes_id ON recommendation_feedbacks(supersedes_id);

ALTER TABLE user_block_rules ADD COLUMN IF NOT EXISTS feedback_id BIGINT;
CREATE INDEX IF NOT EXISTS idx_user_block_rules_feedback_id ON user_block_rules(feedback_id);

ALTER TABLE user_recommendation_profiles ADD COLUMN IF NOT EXISTS feedback_reset_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_user_recommendation_profiles_feedback_reset_at ON user_recommendation_profiles(feedback_reset_at);

-- +goose Down

-- Data-preserving rollback: feedback history and preference reset boundaries
-- remain available to newer binaries while older binaries ignore the columns.
SELECT 1;
