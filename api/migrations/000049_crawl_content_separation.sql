-- +goose Up
-- Record evidence without guessing the configured model, policy or threshold.
-- Activation is performed by assessment using the normal qualification rules.
CREATE TABLE material_crawl_repairs (
    material_id bigint PRIMARY KEY REFERENCES material(id) ON DELETE CASCADE,
    assessment_id bigint NOT NULL,
    state_updated_at timestamptz NOT NULL,
    repaired_at timestamptz
);

INSERT INTO material_crawl_repairs(material_id, assessment_id, state_updated_at)
SELECT s.material_id, a.id, s.updated_at
FROM material_article_states s
JOIN material m ON m.id=s.material_id
JOIN material_versions v ON v.id=m.current_version_id AND v.material_id=m.id AND v.version=s.content_version
JOIN material_representations r ON r.version_id=v.id AND r.kind='text' AND r.role='body' AND r.text<>''
JOIN discovery_article_assessments a ON a.id=s.current_assessment_id
    AND a.material_id=s.material_id AND a.content_version=s.content_version
WHERE (
    (s.eligibility_state='unknown' AND s.assessment_state='pending'
     AND s.eligibility_reasons IN ('dedupe_pending,assessment_pending','assessment_pending'))
    OR (
        (s.eligibility_reasons IN ('http_status','robots_disallowed','unsafe_url',
            'body_too_large','content_type','extraction_failed','not_article','missing_title','body_too_short','language_unknown')
         OR (s.eligibility_state='unknown' AND s.eligibility_reasons IN ('article_quality_passed','article_quality_below_threshold')))
        AND EXISTS (SELECT 1 FROM discovery_candidates c WHERE c.material_id=m.id
            AND c.processing_error_type<>'' AND c.updated_at>=a.created_at
            AND c.processing_state IN ('fetch_pending','failed','review','ineligible'))
    )
)
AND (r.extractor='archive_html' OR EXISTS (
    SELECT 1 FROM discovery_candidates c
    JOIN material_candidate_versions cv ON cv.candidate_id=c.id AND cv.version_id=v.id
    WHERE c.material_id=m.id AND c.extracted_at IS NOT NULL
));

-- +goose Down
-- Do not restore crawl-corrupted article state on downgrade.
DROP TABLE material_crawl_repairs;
