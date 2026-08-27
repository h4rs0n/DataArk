-- +goose Up

ALTER TABLE assessment_llm_calls
    ADD COLUMN IF NOT EXISTS predicted_tokens INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS predicted_ms BIGINT NOT NULL DEFAULT 0;

-- +goose Down

-- Data-preserving rollback: decode timing columns remain for newer binaries.
SELECT 1;
