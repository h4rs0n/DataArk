-- +goose Up

CREATE TABLE IF NOT EXISTS assessment_llm_calls (
    id BIGSERIAL PRIMARY KEY,
    candidate_id BIGINT NOT NULL DEFAULT 0,
    stage TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT '',
    attempt INTEGER NOT NULL DEFAULT 0,
    duration_ms BIGINT NOT NULL DEFAULT 0,
    prompt_tokens INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    reasoning_tokens INTEGER NOT NULL DEFAULT 0,
    cached_tokens INTEGER NOT NULL DEFAULT 0,
    total_tokens INTEGER NOT NULL DEFAULT 0,
    evidence_tokens INTEGER NOT NULL DEFAULT 0,
    response_mode TEXT NOT NULL DEFAULT '',
    error_type TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_assessment_llm_calls_stage_created
    ON assessment_llm_calls (stage, created_at);
CREATE INDEX IF NOT EXISTS idx_assessment_llm_calls_candidate
    ON assessment_llm_calls (candidate_id);

-- +goose Down

-- Data-preserving rollback: token rows remain for newer binaries.
SELECT 1;
