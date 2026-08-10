-- +goose Up
-- +goose StatementBegin

CREATE TABLE IF NOT EXISTS article_assessment_workflow_runs (
    id BIGSERIAL PRIMARY KEY,
    seed VARCHAR(160) NOT NULL,
    manifest_digest VARCHAR(128) NOT NULL,
    policy_version VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL,
    created_by BIGINT NOT NULL,
    pass_one_completed_at TIMESTAMPTZ,
    pass_two_started_at TIMESTAMPTZ,
    pass_two_completed_at TIMESTAMPTZ,
    human_completed_at TIMESTAMPTZ,
    evaluation_generation INTEGER NOT NULL DEFAULT 0,
    evaluation_started_at TIMESTAMPTZ,
    evaluation_completed_at TIMESTAMPTZ,
    model VARCHAR(255) NOT NULL DEFAULT '',
    prompt_version VARCHAR(128) NOT NULL DEFAULT '',
    evaluation_error TEXT NOT NULL DEFAULT '',
    report_json TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_article_assessment_workflow_active
ON article_assessment_workflow_runs ((1))
WHERE status IN ('pass_one', 'waiting_pass_two', 'pass_two', 'adjudication', 'human_complete', 'evaluating', 'evaluation_failed');

CREATE TABLE IF NOT EXISTS article_assessment_workflow_items (
    id BIGSERIAL PRIMARY KEY,
    run_id BIGINT NOT NULL REFERENCES article_assessment_workflow_runs(id) ON DELETE CASCADE,
    position INTEGER NOT NULL,
    pass_two_position INTEGER,
    adjudication_position INTEGER,
    sample_id VARCHAR(64) NOT NULL,
    candidate_id BIGINT NOT NULL REFERENCES discovery_candidates(id) ON DELETE RESTRICT,
    content_version_id BIGINT NOT NULL REFERENCES discovery_article_content_versions(id) ON DELETE RESTRICT,
    content_version BIGINT NOT NULL,
    content_hash VARCHAR(128) NOT NULL,
    host VARCHAR(255) NOT NULL DEFAULT '',
    language VARCHAR(32) NOT NULL DEFAULT '',
    body_characters INTEGER NOT NULL,
    stratum VARCHAR(64) NOT NULL,
    baseline_assessor VARCHAR(128) NOT NULL DEFAULT '',
    baseline_quality INTEGER NOT NULL,
    baseline_depth INTEGER NOT NULL,
    baseline_evergreen INTEGER NOT NULL,
    rule_quality INTEGER NOT NULL,
    rule_depth INTEGER NOT NULL,
    rule_evergreen INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (run_id, sample_id),
    UNIQUE (run_id, position)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_article_assessment_workflow_pass_two_position
ON article_assessment_workflow_items(run_id, pass_two_position)
WHERE pass_two_position IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_article_assessment_workflow_adjudication_position
ON article_assessment_workflow_items(run_id, adjudication_position)
WHERE adjudication_position IS NOT NULL;

CREATE TABLE IF NOT EXISTS article_assessment_workflow_labels (
    id BIGSERIAL PRIMARY KEY,
    run_id BIGINT NOT NULL REFERENCES article_assessment_workflow_runs(id) ON DELETE CASCADE,
    sample_id VARCHAR(64) NOT NULL,
    pass INTEGER NOT NULL CHECK (pass BETWEEN 1 AND 3),
    quality INTEGER NOT NULL CHECK (quality BETWEEN 0 AND 100),
    depth INTEGER NOT NULL CHECK (depth BETWEEN 0 AND 100),
    evergreen INTEGER NOT NULL CHECK (evergreen BETWEEN 0 AND 100),
    reason TEXT NOT NULL DEFAULT '',
    genre VARCHAR(32) NOT NULL DEFAULT '',
    extraction_bad BOOLEAN NOT NULL DEFAULT FALSE,
    unjudgeable BOOLEAN NOT NULL DEFAULT FALSE,
    duration_seconds INTEGER NOT NULL DEFAULT 0,
    labeled_by BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (run_id, sample_id, pass)
);

CREATE INDEX IF NOT EXISTS idx_article_assessment_workflow_labels_progress
ON article_assessment_workflow_labels(run_id, pass);

CREATE TABLE IF NOT EXISTS article_assessment_workflow_scores (
    id BIGSERIAL PRIMARY KEY,
    run_id BIGINT NOT NULL REFERENCES article_assessment_workflow_runs(id) ON DELETE CASCADE,
    generation INTEGER NOT NULL,
    sample_id VARCHAR(64) NOT NULL,
    model_run INTEGER NOT NULL CHECK (model_run BETWEEN 1 AND 2),
    candidate_id BIGINT NOT NULL,
    quality INTEGER NOT NULL DEFAULT 0,
    depth INTEGER NOT NULL DEFAULT 0,
    evergreen INTEGER NOT NULL DEFAULT 0,
    reasons_json TEXT NOT NULL DEFAULT '',
    evidence_tokens INTEGER NOT NULL DEFAULT 0,
    original_evidence_tokens INTEGER NOT NULL DEFAULT 0,
    evidence_truncated BOOLEAN NOT NULL DEFAULT FALSE,
    duration_milliseconds BIGINT NOT NULL DEFAULT 0,
    error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (run_id, generation, sample_id, model_run)
);

CREATE INDEX IF NOT EXISTS idx_article_assessment_workflow_scores_progress
ON article_assessment_workflow_scores(run_id, generation);

CREATE TABLE IF NOT EXISTS article_assessment_workflow_calls (
    id BIGSERIAL PRIMARY KEY,
    run_id BIGINT NOT NULL REFERENCES article_assessment_workflow_runs(id) ON DELETE CASCADE,
    generation INTEGER NOT NULL,
    candidate_id BIGINT NOT NULL,
    status VARCHAR(32) NOT NULL,
    error_type VARCHAR(64) NOT NULL DEFAULT '',
    model VARCHAR(255) NOT NULL DEFAULT '',
    response_mode VARCHAR(32) NOT NULL DEFAULT '',
    attempt INTEGER NOT NULL DEFAULT 0,
    evidence_tokens INTEGER NOT NULL DEFAULT 0,
    original_evidence_tokens INTEGER NOT NULL DEFAULT 0,
    evidence_truncated BOOLEAN NOT NULL DEFAULT FALSE,
    duration_milliseconds BIGINT NOT NULL DEFAULT 0,
    usage_available BOOLEAN NOT NULL DEFAULT FALSE,
    prompt_tokens INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    reasoning_tokens INTEGER NOT NULL DEFAULT 0,
    cached_tokens INTEGER NOT NULL DEFAULT 0,
    total_tokens INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_article_assessment_workflow_calls_run_generation
ON article_assessment_workflow_calls(run_id, generation, id);

-- +goose StatementEnd

-- +goose Down

-- Data-preserving rollback: newer binaries keep the private human labels and
-- evaluation audit, while older binaries simply ignore these tables.
SELECT 1;
