-- 调查日期：2026-10-20。按数据库现存表展开审计；全部事务 READ ONLY。
-- 适用于本次运行库 schema（Goose 000045）；只在匹配 schema 上复用。

-- inventory.sql
\pset format unaligned
\pset tuples_only on
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL max_parallel_workers_per_gather=0;
SET LOCAL work_mem='4MB';
SET LOCAL statement_timeout='30s';
SELECT format('SELECT %L AS relation, jsonb_build_object(%s) AS observations FROM %I.%I;',table_name,
 concat_ws(', ',quote_literal('rows')||', count(*)',
 string_agg(CASE
 WHEN column_name IN('updated_at') AND EXISTS(SELECT 1 FROM information_schema.columns c2 WHERE c2.table_schema=c.table_schema AND c2.table_name=c.table_name AND c2.column_name='created_at') THEN quote_literal('updated_before_created')||', count(*) FILTER(WHERE updated_at < created_at)'
 WHEN column_name IN('created_at','updated_at','finished_at','returned_at','published_at') THEN quote_literal('future:'||column_name)||', count(*) FILTER(WHERE '||quote_ident(column_name)||' > now()+interval ''1 day'')'
 WHEN data_type IN('smallint','integer','bigint','double precision','real','numeric') AND (column_name LIKE '%count%' OR column_name LIKE '%tokens%' OR column_name IN('attempt','attempts','duration_ms','file_bytes','word_count','quality_score','depth_score','content_version','version')) THEN quote_literal('negative:'||column_name)||', count(*) FILTER(WHERE '||quote_ident(column_name)||'<0)'
 WHEN column_name IN('status','processing_state','eligibility_state','assessment_state','role','phase','kind','mode') THEN quote_literal('values:'||column_name)||', jsonb_agg(DISTINCT '||quote_ident(column_name)||')'
 END,', ' ORDER BY ordinal_position)),table_schema,table_name)
FROM information_schema.columns c
WHERE table_schema='public' AND table_name IN(SELECT table_name FROM information_schema.tables WHERE table_schema='public' AND table_type='BASE TABLE')
GROUP BY table_schema,table_name ORDER BY table_name
\gexec
SELECT format('SELECT %L check_name,count(*) FROM %I x LEFT JOIN users u ON u.id=x.%I WHERE x.%I IS NOT NULL AND u.id IS NULL;',table_name||'.'||column_name||'_missing_user',table_name,column_name,column_name)
FROM information_schema.columns WHERE table_schema='public' AND column_name IN('user_id','reporter_user_id') ORDER BY table_name
\gexec
COMMIT;

-- relations.sql
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL max_parallel_workers_per_gather=0;
SET LOCAL work_mem='4MB';
SET LOCAL statement_timeout='45s';
SELECT 'recommendation_item_parent_missing_or_double' check_name,count(*) n FROM recommendation_items i LEFT JOIN recommendation_days d ON d.id=i.day_id LEFT JOIN recommendation_feed_batches f ON f.id=i.feed_batch_id WHERE (i.day_id IS NULL)=(i.feed_batch_id IS NULL) OR (i.day_id IS NOT NULL AND d.id IS NULL) OR(i.feed_batch_id IS NOT NULL AND f.id IS NULL)
UNION ALL SELECT 'recommendation_item_parent_wrong_user',count(*) FROM recommendation_items i LEFT JOIN recommendation_days d ON d.id=i.day_id LEFT JOIN recommendation_feed_batches f ON f.id=i.feed_batch_id WHERE i.user_id IS DISTINCT FROM coalesce(d.user_id,f.user_id)
UNION ALL SELECT 'recommendation_day_actual_count_mismatch',count(*) FROM recommendation_days d WHERE d.actual_count<>(SELECT count(*) FROM recommendation_items i WHERE i.day_id=d.id)
UNION ALL SELECT 'recommendation_feed_actual_count_mismatch',count(*) FROM recommendation_feed_batches b WHERE b.actual_count<>(SELECT count(*) FROM recommendation_items i WHERE i.feed_batch_id=b.id)
UNION ALL SELECT 'recommendation_rank_duplicates',count(*) FROM(SELECT day_id,feed_batch_id,rank FROM recommendation_items GROUP BY 1,2,3 HAVING count(*)>1)x
UNION ALL SELECT 'recommendation_feedback_wrong_user',count(*) FROM recommendation_feedbacks f JOIN recommendation_items i ON i.id=f.recommendation_item_id WHERE f.user_id<>i.user_id
UNION ALL SELECT 'feedback_current_and_reverted',count(*) FROM recommendation_feedbacks WHERE is_current AND reverted_at IS NOT NULL
UNION ALL SELECT 'feedback_supersedes_missing',count(*) FROM recommendation_feedbacks f LEFT JOIN recommendation_feedbacks p ON p.id=f.supersedes_id WHERE f.supersedes_id IS NOT NULL AND p.id IS NULL
UNION ALL SELECT 'feedback_current_without_key',count(*) FROM recommendation_feedbacks WHERE is_current AND coalesce(current_key,'')=''
UNION ALL SELECT 'active_block_from_reverted_feedback',count(*) FROM user_block_rules r JOIN recommendation_feedbacks f ON f.id=r.feedback_id WHERE r.active AND f.reverted_at IS NOT NULL
UNION ALL SELECT 'block_rule_feedback_missing',count(*) FROM user_block_rules r LEFT JOIN recommendation_feedbacks f ON f.id=r.feedback_id WHERE r.feedback_id IS NOT NULL AND f.id IS NULL
UNION ALL SELECT 'user_state_read_without_open',count(*) FROM user_material_states WHERE read_at IS NOT NULL AND opened_at IS NULL
UNION ALL SELECT 'user_state_exposure_time_inverted',count(*) FROM user_material_states WHERE first_exposed_at>last_exposed_at
UNION ALL SELECT 'user_state_exposure_zero_with_time',count(*) FROM user_material_states WHERE exposure_count=0 AND (first_exposed_at IS NOT NULL OR last_exposed_at IS NOT NULL)
UNION ALL SELECT 'source_missing_site',count(*) FROM discovery_sources s LEFT JOIN discovery_sites d ON d.id=s.site_id WHERE s.site_id IS NOT NULL AND d.id IS NULL
UNION ALL SELECT 'source_missing_site_id',count(*) FROM discovery_sources WHERE site_id IS NULL
UNION ALL SELECT 'site_edge_missing_endpoint',count(*) FROM discovery_site_edges e LEFT JOIN discovery_sites a ON a.id=e.from_site_id LEFT JOIN discovery_sites b ON b.id=e.to_site_id WHERE a.id IS NULL OR b.id IS NULL
UNION ALL SELECT 'site_edge_self_loop',count(*) FROM discovery_site_edges WHERE from_site_id=to_site_id
UNION ALL SELECT 'site_empty_domain',count(*) FROM discovery_sites WHERE coalesce(btrim(domain_key),'')=''
UNION ALL SELECT 'source_empty_crawl_host',count(*) FROM discovery_sources WHERE coalesce(btrim(crawl_host),'')=''
UNION ALL SELECT 'candidate_empty_crawl_host',count(*) FROM discovery_candidates WHERE coalesce(btrim(crawl_host),'')=''
UNION ALL SELECT 'fetch_finished_before_started',count(*) FROM discovery_fetch_runs WHERE finished_at<started_at
UNION ALL SELECT 'fetch_terminal_without_finished',count(*) FROM discovery_fetch_runs WHERE status IN('succeeded','failed') AND finished_at IS NULL
UNION ALL SELECT 'candidate_fetching_older_than_hour',count(*) FROM discovery_candidates WHERE processing_state='fetching' AND updated_at<now()-interval '1 hour'
UNION ALL SELECT 'backfill_coverage_inverted',count(*) FROM discovery_backfill_states WHERE earliest_covered>latest_covered
UNION ALL SELECT 'assessment_numeric_out_of_range',count(*) FROM discovery_article_assessments WHERE information_density NOT BETWEEN 0 AND 1 OR originality NOT BETWEEN 0 AND 1 OR completeness NOT BETWEEN 0 AND 1 OR evidence NOT BETWEEN 0 AND 1 OR readability NOT BETWEEN 0 AND 1 OR depth NOT BETWEEN 0 AND 1 OR evergreen_value NOT BETWEEN 0 AND 1 OR overall_quality NOT BETWEEN 0 AND 1 OR confidence NOT BETWEEN 0 AND 1
UNION ALL SELECT 'material_state_score_out_of_range',count(*) FROM material_article_states WHERE quality_score NOT BETWEEN 0 AND 1 OR depth_score NOT BETWEEN 0 AND 1
UNION ALL SELECT 'llm_total_not_prompt_plus_completion',count(*) FROM assessment_llm_calls WHERE total_tokens<>prompt_tokens+completion_tokens
UNION ALL SELECT 'batch_item_total_not_prompt_plus_completion',count(*) FROM assessment_batch_items WHERE total_tokens<>prompt_tokens+completion_tokens
UNION ALL SELECT 'batch_item_wrong_run_for_batch',count(*) FROM assessment_batch_items i JOIN assessment_batches b ON b.id=i.batch_id WHERE i.run_id<>b.run_id
UNION ALL SELECT 'batch_item_success_without_assessment',count(*) FROM assessment_batch_items WHERE status='succeeded' AND assessment_id IS NULL
UNION ALL SELECT 'batch_item_terminal_with_active_identity',count(*) FROM assessment_batch_items WHERE status IN('succeeded','failed','skipped','reused') AND active_identity IS NOT NULL
UNION ALL SELECT 'batch_done_without_finished',count(*) FROM assessment_batches WHERE phase='done' AND finished_at IS NULL
UNION ALL SELECT 'batch_provider_count_exceeds_total',count(*) FROM assessment_batches WHERE completed+failed>total
UNION ALL SELECT 'batch_file_bytes_wrong',count(*) FROM assessment_batches WHERE file_bytes<>octet_length(input_jsonl)
UNION ALL SELECT 'llm_call_batch_item_missing',count(*) FROM assessment_llm_calls c LEFT JOIN assessment_batch_items i ON i.id=c.batch_item_id WHERE c.batch_item_id IS NOT NULL AND i.id IS NULL
UNION ALL SELECT 'llm_call_batch_material_mismatch',count(*) FROM assessment_llm_calls c JOIN assessment_batch_items i ON i.id=c.batch_item_id WHERE c.material_id IS DISTINCT FROM i.material_id
UNION ALL SELECT 'dedupe_change_candidate_missing',count(*) FROM article_dedupe_changes x LEFT JOIN discovery_candidates c ON c.id=x.candidate_id WHERE c.id IS NULL
UNION ALL SELECT 'dedupe_member_candidate_missing',count(*) FROM article_dedupe_members d LEFT JOIN discovery_candidates c ON c.id=d.candidate_id WHERE d.candidate_id<>0 AND c.id IS NULL
UNION ALL SELECT 'dedupe_member_candidate_wrong_material',count(*) FROM article_dedupe_members d JOIN discovery_candidates c ON c.id=d.candidate_id WHERE d.material_id<>c.material_id
UNION ALL SELECT 'dedupe_fingerprint_wrong_bytes',count(*) FROM article_dedupe_fingerprints WHERE octet_length(signature)<>1024
UNION ALL SELECT 'dedupe_bucket_band_out_of_range',count(*) FROM article_dedupe_buckets WHERE band<0 OR band>15;
SELECT status,count(*) n FROM recommendation_feed_batches GROUP BY 1;
SELECT action,is_current,closed_reason,count(*) n FROM recommendation_feedbacks GROUP BY 1,2,3;
SELECT source,file_count FROM archive_stats ORDER BY source;
SELECT role,count(*) FROM users GROUP BY role;
COMMIT;

-- content.sql
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL max_parallel_workers_per_gather=0;
SET LOCAL work_mem='4MB';
SET LOCAL statement_timeout='45s';
SELECT 'backfill_cursor_invalid_json' check_name,count(*) n FROM discovery_backfill_states WHERE NOT pg_input_is_valid(cursor,'jsonb')
UNION ALL SELECT 'assessment_reasons_invalid_json',count(*) FROM discovery_article_assessments WHERE coalesce(btrim(reasons),'')<>'' AND NOT pg_input_is_valid(reasons,'jsonb')
UNION ALL SELECT 'assessment_keywords_invalid_json',count(*) FROM discovery_article_assessments WHERE coalesce(btrim(keywords),'')<>'' AND NOT pg_input_is_valid(keywords,'jsonb')
UNION ALL SELECT 'dedupe_before_invalid_json',count(*) FROM article_dedupe_changes WHERE NOT pg_input_is_valid(before,'jsonb') OR NOT pg_input_is_valid(after,'jsonb')
UNION ALL SELECT 'recommendation_topics_invalid_json',count(*) FROM recommendation_items WHERE coalesce(btrim(snapshot_topics),'')<>'' AND NOT pg_input_is_valid(snapshot_topics,'jsonb')
UNION ALL SELECT 'digest_summary_topics_invalid_json',count(*) FROM recommendation_days WHERE coalesce(btrim(summary_topics),'')<>'' AND NOT pg_input_is_valid(summary_topics,'jsonb')
UNION ALL SELECT 'digest_summary_highlights_invalid_json',count(*) FROM recommendation_days WHERE coalesce(btrim(summary_highlights),'')<>'' AND NOT pg_input_is_valid(summary_highlights,'jsonb')
UNION ALL SELECT 'representation_empty_body',count(*) FROM material_representations WHERE btrim(coalesce(text,''))=''
UNION ALL SELECT 'representation_missing_hash',count(*) FROM material_representations WHERE coalesce(content_hash,'')=''
UNION ALL SELECT 'assessment_nonzero_without_version',count(*) FROM discovery_article_assessments WHERE content_version>0 AND material_version_id IS NULL
UNION ALL SELECT 'material_state_eligible_without_assessment',count(*) FROM material_article_states WHERE eligibility_state='eligible' AND current_assessment_id IS NULL
UNION ALL SELECT 'current_assessment_summary_differs_material',count(*) FROM material m JOIN material_article_states s ON s.material_id=m.id JOIN discovery_article_assessments a ON a.id=s.current_assessment_id WHERE coalesce(a.summary,'')<>'' AND a.summary IS DISTINCT FROM m.summary
UNION ALL SELECT 'material_without_provenance_candidate_or_archive',count(*) FROM material m WHERE NOT EXISTS(SELECT 1 FROM material_provenances p WHERE p.material_id=m.id) AND NOT EXISTS(SELECT 1 FROM discovery_candidates c WHERE c.material_id=m.id) AND NOT EXISTS(SELECT 1 FROM archive_documents a WHERE a.material_id=m.id)
UNION ALL SELECT 'old_candidate_feedback_missing_candidate',count(*) FROM discovery_candidate_feedbacks f LEFT JOIN discovery_candidates c ON c.id=f.candidate_id WHERE c.id IS NULL
UNION ALL SELECT 'review_signal_missing_recommendation',count(*) FROM discovery_duplicate_review_signals s LEFT JOIN recommendation_items i ON i.id=s.recommendation_item_id WHERE i.id IS NULL
UNION ALL SELECT 'schedule_decision_missing_source',count(*) FROM discovery_source_schedule_decisions d LEFT JOIN discovery_sources s ON s.id=d.source_id WHERE s.id IS NULL
UNION ALL SELECT 'site_stats_missing_site',count(*) FROM discovery_site_operational_stats s LEFT JOIN discovery_sites d ON d.id=s.site_id WHERE d.id IS NULL
UNION ALL SELECT 'fingerprint_missing_bands',count(*) FROM article_dedupe_fingerprints f LEFT JOIN (SELECT representation_id,algorithm,count(*) n FROM article_dedupe_buckets GROUP BY representation_id,algorithm) b ON b.representation_id=f.representation_id AND b.algorithm=f.algorithm WHERE coalesce(b.n,0)<>16
UNION ALL SELECT 'bucket_without_fingerprint',count(*) FROM article_dedupe_buckets b WHERE NOT EXISTS(SELECT 1 FROM article_dedupe_fingerprints f WHERE f.representation_id=b.representation_id AND f.algorithm=b.algorithm)
UNION ALL SELECT 'active_rule_empty_value',count(*) FROM user_block_rules WHERE active AND coalesce(btrim(rule_value),'')='';
SELECT 'duplicate_site_domains' check_name,count(*) n FROM(SELECT domain_key FROM discovery_sites GROUP BY domain_key HAVING count(*)>1)x
UNION ALL SELECT 'duplicate_source_urls',count(*) FROM(SELECT url FROM discovery_sources GROUP BY url HAVING count(*)>1)x
UNION ALL SELECT 'duplicate_candidate_normalized_urls',count(*) FROM(SELECT normalized_url FROM discovery_candidates WHERE coalesce(normalized_url,'')<>'' GROUP BY normalized_url HAVING count(*)>1)x
UNION ALL SELECT 'duplicate_user_material_states',count(*) FROM(SELECT user_id,material_id FROM user_material_states GROUP BY 1,2 HAVING count(*)>1)x
UNION ALL SELECT 'duplicate_current_feedback_per_item',count(*) FROM(SELECT user_id,recommendation_item_id FROM recommendation_feedbacks WHERE is_current GROUP BY 1,2 HAVING count(*)>1)x
UNION ALL SELECT 'duplicate_active_block_rules',count(*) FROM(SELECT user_id,rule_type,rule_value FROM user_block_rules WHERE active GROUP BY 1,2,3 HAVING count(*)>1)x;
SELECT rule_type,count(*) FROM user_block_rules GROUP BY 1;
SELECT current_feedback,count(*) FROM user_material_states GROUP BY 1;
SELECT earliest_covered,latest_covered,status,count(*) FROM discovery_backfill_states WHERE NOT pg_input_is_valid(cursor,'jsonb') GROUP BY 1,2,3;
SELECT id,left(title,80) title,published_at,created_at,updated_at FROM material WHERE published_at>now()+interval '1 day' ORDER BY published_at DESC;
COMMIT;

-- semantic.sql
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='30s';
SET LOCAL max_parallel_workers_per_gather=0;
SET LOCAL work_mem='4MB';
SELECT 'state_quality_differs_current_assessment' check_name,count(*) n FROM material_article_states s JOIN discovery_article_assessments a ON a.id=s.current_assessment_id WHERE abs(s.quality_score-a.overall_quality)>0.000001
UNION ALL SELECT 'state_depth_differs_current_assessment',count(*) FROM material_article_states s JOIN discovery_article_assessments a ON a.id=s.current_assessment_id WHERE abs(s.depth_score-a.depth)>0.000001
UNION ALL SELECT 'current_keywords_differs_material_topics',count(*) FROM material m JOIN material_article_states s ON s.material_id=m.id JOIN discovery_article_assessments a ON a.id=s.current_assessment_id WHERE CASE WHEN pg_input_is_valid(a.keywords,'jsonb') THEN m.topics IS DISTINCT FROM a.keywords::jsonb ELSE FALSE END
UNION ALL SELECT 'llm_reasoning_exceeds_completion',count(*) FROM assessment_llm_calls WHERE reasoning_tokens>completion_tokens
UNION ALL SELECT 'llm_cached_exceeds_prompt',count(*) FROM assessment_llm_calls WHERE cached_tokens>prompt_tokens
UNION ALL SELECT 'batch_item_assessment_token_pair_differs',count(*) FROM assessment_batch_items b JOIN assessment_llm_calls c ON c.batch_item_id=b.id AND c.status='success' WHERE b.status='succeeded' AND (b.prompt_tokens<>c.prompt_tokens OR b.completion_tokens<>c.completion_tokens OR b.total_tokens<>c.total_tokens)
UNION ALL SELECT 'batch_total_differs_assigned_items',count(*) FROM assessment_batches b WHERE b.total<>(SELECT count(*) FROM assessment_batch_items i WHERE i.batch_id=b.id)
UNION ALL SELECT 'current_feedback_material_state_disagrees',count(*) FROM recommendation_feedbacks f JOIN user_material_states s ON s.material_id=f.material_id AND s.user_id=f.user_id WHERE f.is_current AND f.action<>s.current_feedback AND s.feedback_set_at<=f.created_at
UNION ALL SELECT 'active_feed_replaced_timestamp_set',count(*) FROM recommendation_feed_batches WHERE status='active' AND replaced_at IS NOT NULL
UNION ALL SELECT 'daily_summary_count_mismatch',count(*) FROM recommendation_days WHERE coalesce(summary_text,'')<>'' AND summary_actual_count<>actual_count;
SELECT s.eligibility_state,s.eligibility_reasons,count(*) n,min(s.quality_score),max(s.quality_score) FROM material_article_states s GROUP BY 1,2 ORDER BY n DESC LIMIT 14;
SELECT status,count(*) n,count(*) FILTER(WHERE prompt_tokens=0 AND completion_tokens=0) zero_token_calls,min(created_at),max(created_at) FROM assessment_llm_calls GROUP BY 1;
SELECT jsonb_typeof(positive_embedding) positive_shape,jsonb_typeof(negative_embedding) negative_shape,exploration_rate,depth_preference FROM user_recommendation_profiles;
SELECT id,preferred_depth,exploration_rate,daily_limit,candidate_window_days,pg_input_is_valid(preferred_topics,'jsonb') topics_json,pg_input_is_valid(preferred_languages,'jsonb') languages_json,pg_input_is_valid(favorite_sources,'jsonb') sources_json FROM recommendation_settings;
COMMIT;

-- 具体异常复核；不更新原记录。
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='30s';
SET LOCAL max_parallel_workers_per_gather=0;
SELECT count(*) domains,sum(file_count) reported_files FROM archive_stats;
SELECT count(*) indexed_documents FROM archive_documents;
SELECT c.id,c.material_id,c.updated_at,c.processing_attempts,
       (SELECT count(*) FROM river_job j WHERE j.kind='discovery_process_candidate'
        AND j.state IN('available','pending','scheduled','retryable','running')
        AND (j.args->>'candidate_id')::bigint=c.id) live_processing_jobs
FROM discovery_candidates c
WHERE c.processing_state='fetching' AND c.updated_at<now()-interval '1 hour'
ORDER BY c.updated_at;
SELECT extract(year FROM published_at) published_year,count(*) n
FROM material WHERE published_at<'1970-01-01' OR published_at>now()+interval '1 day'
GROUP BY 1 ORDER BY 1;
SELECT s.material_id,s.current_assessment_id,s.quality_score,a.overall_quality,s.depth_score,a.depth,
       a.policy_version,s.updated_at,a.created_at
FROM material_article_states s JOIN discovery_article_assessments a ON a.id=s.current_assessment_id
WHERE abs(s.quality_score-a.overall_quality)>0.000001 OR abs(s.depth_score-a.depth)>0.000001;
SELECT m.id,m.updated_at,s.current_assessment_id,a.created_at,a.policy_version
FROM material m JOIN material_article_states s ON s.material_id=m.id
JOIN discovery_article_assessments a ON a.id=s.current_assessment_id
WHERE coalesce(a.summary,'')<>'' AND a.summary IS DISTINCT FROM m.summary;
SELECT a.policy_version,a.assessor,count(*) n,min(a.created_at),max(a.created_at),
       count(*) FILTER(WHERE s.eligibility_state='eligible') eligible
FROM material_article_states s JOIN discovery_article_assessments a ON a.id=s.current_assessment_id
WHERE a.assessor<>'deterministic_rules' AND coalesce(btrim(a.summary),'')=''
GROUP BY 1,2;
SELECT id,material_id,first_exposed_at,last_exposed_at,created_at,updated_at
FROM user_material_states WHERE first_exposed_at>last_exposed_at OR updated_at<created_at;
COMMIT;
