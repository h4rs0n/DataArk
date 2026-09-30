-- +goose Up

-- Remove the retired human-label and model-acceptance workflow, including its data.
-- Drop child tables first; retain content versions and production assessments.
DROP TABLE IF EXISTS article_assessment_workflow_labels;
DROP TABLE IF EXISTS article_assessment_workflow_scores;
DROP TABLE IF EXISTS article_assessment_workflow_calls;
DROP TABLE IF EXISTS article_assessment_workflow_items;
DROP TABLE IF EXISTS article_assessment_workflow_runs;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    RAISE EXCEPTION 'Restore the pre-workflow-removal database backup and binary to roll back';
END $$;
-- +goose StatementEnd
