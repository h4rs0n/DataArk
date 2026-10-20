-- 调查日期：2026-10-20。适用已部署 Goose 000045；全部只读。
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='45s';
SELECT 'candidate_missing_material' check_name,count(*) n FROM discovery_candidates c LEFT JOIN material m ON m.id=c.material_id WHERE m.id IS NULL
UNION ALL SELECT 'material_missing_article_state',count(*) FROM material m LEFT JOIN material_article_states s ON s.material_id=m.id WHERE s.material_id IS NULL
UNION ALL SELECT 'current_version_wrong_owner',count(*) FROM material m JOIN material_versions v ON v.id=m.current_version_id WHERE v.material_id<>m.id
UNION ALL SELECT 'version_without_body',count(*) FROM material_versions v WHERE NOT EXISTS(SELECT 1 FROM material_representations r WHERE r.version_id=v.id AND r.kind='text' AND r.role='body')
UNION ALL SELECT 'mapping_wrong_owner',count(*) FROM material_candidate_versions x JOIN material_versions v ON v.id=x.version_id WHERE x.material_id<>v.material_id
UNION ALL SELECT 'mapping_candidate_wrong_owner',count(*) FROM material_candidate_versions x JOIN discovery_candidates c ON c.id=x.candidate_id WHERE x.material_id<>c.material_id
UNION ALL SELECT 'legacy_version_missing_mapping',count(*) FROM discovery_article_content_versions v LEFT JOIN material_candidate_versions x ON x.candidate_id=v.candidate_id AND x.content_version=v.content_version WHERE x.candidate_id IS NULL
UNION ALL SELECT 'legacy_version_mapping_disagrees',count(*) FROM discovery_article_content_versions v JOIN material_candidate_versions x ON x.candidate_id=v.candidate_id AND x.content_version=v.content_version WHERE v.material_id IS DISTINCT FROM x.material_id OR v.material_version_id IS DISTINCT FROM x.version_id
UNION ALL SELECT 'ready_candidate_without_current_version',count(*) FROM discovery_candidates c JOIN material m ON m.id=c.material_id WHERE c.processing_state='ready' AND m.current_version_id IS NULL
UNION ALL SELECT 'state_version_disagrees_with_current',count(*) FROM material_article_states s JOIN material m ON m.id=s.material_id JOIN material_versions v ON v.id=m.current_version_id WHERE s.content_version<>v.version
UNION ALL SELECT 'current_assessment_wrong_owner',count(*) FROM material_article_states s JOIN discovery_article_assessments a ON a.id=s.current_assessment_id WHERE a.material_id IS DISTINCT FROM s.material_id
UNION ALL SELECT 'current_assessment_wrong_version',count(*) FROM material_article_states s JOIN discovery_article_assessments a ON a.id=s.current_assessment_id JOIN material m ON m.id=s.material_id WHERE a.material_version_id IS DISTINCT FROM m.current_version_id OR a.content_version<>s.content_version
UNION ALL SELECT 'ready_assessment_without_pointer',count(*) FROM material_article_states WHERE assessment_state='ready' AND current_assessment_id IS NULL
UNION ALL SELECT 'current_rule_assessment',count(*) FROM material_article_states s JOIN discovery_article_assessments a ON a.id=s.current_assessment_id WHERE a.assessor='deterministic_rules'
UNION ALL SELECT 'assessment_missing_material',count(*) FROM discovery_article_assessments WHERE material_id IS NULL
UNION ALL SELECT 'assessment_version_wrong_owner',count(*) FROM discovery_article_assessments a JOIN material_versions v ON v.id=a.material_version_id WHERE a.material_id IS DISTINCT FROM v.material_id
UNION ALL SELECT 'assessment_candidate_wrong_owner',count(*) FROM discovery_article_assessments a JOIN discovery_candidates c ON c.id=a.candidate_id WHERE a.material_id IS DISTINCT FROM c.material_id
UNION ALL SELECT 'provenance_candidate_wrong_owner',count(*) FROM material_provenances p JOIN discovery_candidates c ON c.id=p.candidate_id WHERE p.material_id<>c.material_id
UNION ALL SELECT 'provenance_migration_uncertain',count(*) FROM material_provenances WHERE migration_uncertain
UNION ALL SELECT 'provenance_empty_domain',count(*) FROM material_provenances WHERE domain_key=''
UNION ALL SELECT 'material_ingestion_issues',count(*) FROM material_ingestion_issues
UNION ALL SELECT 'recommendation_missing_material',count(*) FROM recommendation_items WHERE material_id IS NULL
UNION ALL SELECT 'recommendation_wrong_version_owner',count(*) FROM recommendation_items i JOIN material_versions v ON v.id=i.material_version_id WHERE i.material_id IS DISTINCT FROM v.material_id
UNION ALL SELECT 'feedback_item_wrong_owner',count(*) FROM recommendation_feedbacks f JOIN recommendation_items i ON i.id=f.recommendation_item_id WHERE f.material_id IS DISTINCT FROM i.material_id
UNION ALL SELECT 'user_state_candidate_wrong_owner',count(*) FROM user_material_states u JOIN discovery_candidates c ON c.id=u.candidate_id WHERE u.material_id<>c.material_id
UNION ALL SELECT 'redirect_source_still_material',count(*) FROM material_redirects r JOIN material m ON m.id=r.id
UNION ALL SELECT 'redirect_target_is_redirect',count(*) FROM material_redirects r JOIN material_redirects t ON t.id=r.material_id
UNION ALL SELECT 'llm_call_missing_material',count(*) FROM assessment_llm_calls WHERE material_id IS NULL
UNION ALL SELECT 'llm_call_candidate_wrong_owner',count(*) FROM assessment_llm_calls a JOIN discovery_candidates c ON c.id=a.candidate_id WHERE a.material_id IS DISTINCT FROM c.material_id
UNION ALL SELECT 'dedupe_member_wrong_version_owner',count(*) FROM article_dedupe_members d JOIN material_versions v ON v.id=d.version_id WHERE d.material_id<>v.material_id
UNION ALL SELECT 'dedupe_member_wrong_representation_version',count(*) FROM article_dedupe_members d JOIN material_representations r ON r.id=d.representation_id WHERE d.version_id<>r.version_id
UNION ALL SELECT 'batch_item_wrong_version_owner',count(*) FROM assessment_batch_items b JOIN material_versions v ON v.id=b.version_id WHERE b.material_id<>v.material_id
UNION ALL SELECT 'batch_item_assessment_wrong_owner',count(*) FROM assessment_batch_items b JOIN discovery_article_assessments a ON a.id=b.assessment_id WHERE b.material_id IS DISTINCT FROM a.material_id OR b.version_id IS DISTINCT FROM a.material_version_id;
-- Counts reflect the database at execution time; this script never mutates it.
SELECT assessment_state,eligibility_state,count(*) n FROM material_article_states GROUP BY 1,2 ORDER BY 3 DESC;
SELECT 'legacy_duplicate_recommendations' label,count(*) FROM recommendation_items WHERE legacy_duplicate;
SELECT assessor,count(*) n,count(*) FILTER(WHERE material_version_id IS NULL) without_version FROM discovery_article_assessments GROUP BY 1;
SELECT stage,status,count(*) n,count(*) FILTER(WHERE material_id IS NULL) without_material FROM assessment_llm_calls GROUP BY 1,2 ORDER BY 3 DESC;
SELECT id,algorithm,status,indexed,processed,applied,cursor,high_water_id FROM article_dedupe_runs ORDER BY id DESC LIMIT 5;
COMMIT;
