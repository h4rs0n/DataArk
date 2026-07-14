-- +goose Up

ALTER TABLE recommendation_days
    ADD COLUMN IF NOT EXISTS failure_reason TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS degraded BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS degradation_reason TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS supplement_policy VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS supplemented_at TIMESTAMPTZ;

UPDATE recommendation_days
SET status = CASE status
    WHEN 'pending' THEN 'draft'
    WHEN 'generated' THEN 'published'
    ELSE status
END,
published_at = CASE
    WHEN status IN ('generated', 'published') AND published_at IS NULL THEN generated_at
    ELSE published_at
END;

CREATE INDEX IF NOT EXISTS idx_recommendation_days_degraded ON recommendation_days(degraded);
CREATE INDEX IF NOT EXISTS idx_recommendation_days_supplemented_at ON recommendation_days(supplemented_at);

-- +goose Down

-- Data-preserving rollback: immutable lifecycle and failure audit fields remain
-- available to newer binaries while older binaries ignore them.
SELECT 1;
