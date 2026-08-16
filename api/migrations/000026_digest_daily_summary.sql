-- +goose Up

ALTER TABLE recommendation_days
    ADD COLUMN IF NOT EXISTS summary_text TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS summary_highlights TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS summary_topics TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS summary_model VARCHAR(255) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS summary_prompt_version VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS summary_actual_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS summary_generated_at TIMESTAMPTZ;

-- +goose Down

-- Data-preserving rollback: digest summary fields remain available to newer
-- binaries while older binaries ignore them.
SELECT 1;
