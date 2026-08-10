-- +goose Up
-- +goose StatementBegin

ALTER TABLE article_assessment_workflow_items
    ADD COLUMN IF NOT EXISTS pass_one_skipped_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS pass_one_skipped_by BIGINT;

CREATE INDEX IF NOT EXISTS idx_article_assessment_workflow_pass_one_skips
ON article_assessment_workflow_items(run_id, pass_one_skipped_at)
WHERE pass_one_skipped_at IS NOT NULL;

-- +goose StatementEnd

-- +goose Down

-- Data-preserving rollback: skip audit remains useful and older binaries ignore
-- these additive columns.
SELECT 1;
