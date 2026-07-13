-- +goose Up

ALTER TABLE discovery_candidates
    ADD COLUMN IF NOT EXISTS current_assessment_id BIGINT,
    ADD COLUMN IF NOT EXISTS assessment_error TEXT;

CREATE INDEX IF NOT EXISTS idx_discovery_candidates_current_assessment_id ON discovery_candidates(current_assessment_id);

-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_discovery_candidates_current_assessment') THEN
        ALTER TABLE discovery_candidates
            ADD CONSTRAINT fk_discovery_candidates_current_assessment
            FOREIGN KEY (current_assessment_id) REFERENCES discovery_article_assessments(id) ON DELETE SET NULL;
    END IF;
END
$$;
-- +goose StatementEnd

-- Existing extracted representatives are resumed by the shared candidate
-- recovery query and receive a deterministic rule assessment.
UPDATE discovery_candidates
SET assessment_state = 'pending',
    assessment_error = NULL,
    eligibility_state = 'unknown',
    eligibility_reasons = 'assessment_pending',
    updated_at = NOW()
WHERE processing_state = 'ready'
  AND dedupe_state = 'ready'
  AND (representative_id IS NULL OR representative_id = id)
  AND current_assessment_id IS NULL;

-- +goose Down

-- Data-preserving rollback: assessment versions and the active reference are
-- retained so historical decisions remain explainable.
SELECT 1;
