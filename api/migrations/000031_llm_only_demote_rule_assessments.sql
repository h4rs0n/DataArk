-- +goose Up

-- 历史规则评估不再作为 eligibility 真相；清空当前指针，等待 LLM 重评。
UPDATE discovery_candidates AS candidate
SET assessment_state = 'pending',
    eligibility_state = 'ineligible',
    eligibility_reasons = 'llm_only_requires_model_assessment',
    current_assessment_id = NULL,
    assessment_error = '',
    updated_at = CURRENT_TIMESTAMP
FROM discovery_article_assessments AS assessment
WHERE candidate.current_assessment_id = assessment.id
  AND assessment.assessor = 'deterministic_rules';

-- +goose Down

-- Data-preserving rollback：规则行仍保留在 discovery_article_assessments，但不自动恢复为当前指针。
SELECT 1;
