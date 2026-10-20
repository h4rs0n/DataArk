-- 调查/执行日期：2026-10-20。此文件仅验收，不执行修复。
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='45s';
SET LOCAL max_parallel_workers_per_gather=0;
SELECT 'ready_without_pointer' check_name,count(*) n FROM material_article_states WHERE assessment_state='ready' AND current_assessment_id IS NULL
UNION ALL SELECT 'pending_and_eligible',count(*) FROM material_article_states WHERE assessment_state='pending' AND eligibility_state='eligible'
UNION ALL SELECT 'invalid_current_zero_dates',count(*) FROM material WHERE published_at<'0100-01-01'
UNION ALL SELECT 'exposure_time_inverted',count(*) FROM user_material_states WHERE first_exposed_at>last_exposed_at OR updated_at<created_at
UNION ALL SELECT 'review_time_inverted',count(*) FROM discovery_legacy_candidate_state_reviews WHERE updated_at<created_at
UNION ALL SELECT 'stuck_fetching_without_job',count(*) FROM discovery_candidates c WHERE processing_state='fetching' AND updated_at<now()-interval '1 hour' AND NOT EXISTS(SELECT 1 FROM river_job j WHERE j.kind='discovery_process_candidate' AND j.state IN('available','pending','scheduled','retryable','running') AND (j.args->>'candidate_id')::bigint=c.id)
UNION ALL SELECT 'current_score_mismatch',count(*) FROM material_article_states s JOIN discovery_article_assessments a ON a.id=s.current_assessment_id WHERE abs(s.quality_score-a.overall_quality)>0.000001 OR abs(s.depth_score-a.depth)>0.000001
UNION ALL SELECT 'current_summary_mismatch',count(*) FROM material m JOIN material_article_states s ON s.material_id=m.id JOIN discovery_article_assessments a ON a.id=s.current_assessment_id WHERE coalesce(a.summary,'')<>'' AND a.summary IS DISTINCT FROM m.summary
UNION ALL SELECT 'live_zero_version_jobs',count(*) FROM river_job WHERE kind='assessment_assess_article' AND state IN('available','pending','scheduled','retryable','running') AND coalesce((args->>'content_version')::bigint,0)=0
UNION ALL SELECT 'contaminated_material_still_eligible',count(*) FROM material_article_states WHERE material_id IN(12432,118601,118602,118603,123875,123876,123877,135740,135742) AND (eligibility_state<>'review' OR current_assessment_id IS NOT NULL)
UNION ALL SELECT 'contaminated_candidate_still_ready',count(*) FROM discovery_candidates WHERE material_id IN(12432,118601,118602,118603,123875,123876,123877,135740,135742) AND processing_state<>'review'
UNION ALL SELECT 'archive_without_material_body',count(*) FROM archive_documents a LEFT JOIN material m ON m.id=a.material_id WHERE m.id IS NULL OR m.current_version_id IS NULL
UNION ALL SELECT 'current_version_owner_mismatch',count(*) FROM material m JOIN material_versions v ON v.id=m.current_version_id WHERE m.id<>v.material_id
UNION ALL SELECT 'current_assessment_owner_mismatch',count(*) FROM material_article_states s JOIN discovery_article_assessments a ON a.id=s.current_assessment_id WHERE s.material_id IS DISTINCT FROM a.material_id
UNION ALL SELECT 'candidate_mapping_owner_mismatch',count(*) FROM material_candidate_versions x JOIN material_versions v ON v.id=x.version_id WHERE x.material_id<>v.material_id;
SELECT count(*) archive_documents,count(*) FILTER(WHERE coalesce(source_url,'')<>'') with_source_url FROM archive_documents;
SELECT count(*) canceled_with_cleanup_audit FROM river_job WHERE metadata->>'data_cleanup_run'='data-cleanup-20261020' AND state='cancelled';
SELECT count(*) contaminated_materials FROM material_article_states WHERE eligibility_reasons='content_extraction_contaminated';
SELECT name,paused_at FROM river_queue ORDER BY name;
SELECT id,status,high_water,cursor,admission_enabled FROM assessment_batch_runs WHERE active_key IS NOT NULL;
SELECT count(*) assessments,min(created_at),max(created_at) FROM discovery_article_assessments;
SELECT count(*) recommendations,min(created_at),max(created_at) FROM recommendation_items;
COMMIT;
