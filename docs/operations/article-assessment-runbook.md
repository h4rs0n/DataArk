# Article Assessment Operations Runbook

This runbook describes production operations for `article-value-v4`, which extends the v3 reading-value rubric with a required summary and keywords. Article assessment receives only the title and already-extracted plain body text. It never receives raw HTML, source or site identity, author reputation, popularity, publication date, graph position, or user feedback. The model receives at most an approximately 6,000-token evidence package; oversized bodies retain beginning, middle, and ending excerpts. Assessment and rerank calls disable supported model reasoning modes, send `temperature=0.7`, `top_p=0.80`, `top_k=20`, `min_p=0.0`, `presence_penalty=1.5`, `repetition_penalty=1.0`, cap assessment completion at 32,768 tokens and rerank completion at 2,048 tokens, and write per-attempt stage, response mode, evidence size, duration, provider token counts, and a compact `error_message` to retained `llm_call` logs. Payload text is never logged.

## Rollout modes and configuration

Assessment mode is `active` only. `-llm-chat-model` and `-llm-base-url` are required at process start; an empty API key is still allowed for a local gateway. Missing chat configuration refuses startup. There is no observe mode and no deterministic-rule fallback.

```text
ARTICLE_ASSESSMENT_MODE=active
ARTICLE_ASSESSMENT_CONCURRENCY=2
LLM_BASE_URL=http://llm-gateway:11434
LLM_CHAT_MODEL=your-chat-model
```

These are Compose `.env` settings, passed to CLI flags by `docker/docker-compose.yml`. Direct binary startup must use `-article-assessment-mode`, `-article-assessment-concurrency`, `-llm-base-url`, and `-llm-chat-model`; the binary does not read those environment variables itself.

A model failure retains an existing **model** assessment pointer and records the error. A new article with no previous model row moves from `pending` to `review` on failure and does not receive rule scores or enter eligible inventory. After existing article-validity gates, semantic quality below the configured threshold is ineligible (`-discover-article-quality-threshold`, default 0.20 or 20/100). The production adapter sets confidence to 1.0; confidence is not a model-output field and does not independently reject an article.

The structured response has three integer scores from 0 through 100, exactly two short reasons, one summary, and 3–8 keywords: `qualityScore`, `depthScore`, `evergreenScore`, `reasons`, `summary`, and `keywords`. Reasons, summary, and keywords are requested in Simplified Chinese regardless of article language. Summary is 1–200 characters. Each reason is 1–120 characters; each keyword is 1–20 characters. A missing or invalid summary or keyword list fails that attempt. The provider then appends only the concrete parser or validator error (missing field names, unknown keys, length limits) onto the original messages and retries `json_schema` up to five times. It does not replay previous model completions. After those retries, or after a later non-retryable error such as rate limit, timeout, or context-length rejection once a retryable schema failure has already occurred, it makes one `json_object` call with the original messages plus that latest validator error. HTTP 400/404/415/422 responses that reject structured output skip remaining schema retries, fall back to `json_object` immediately, and cache that mode for the process lifetime of that endpoint and model. Invalid JSON from a schema-capable endpoint is not cached as `json_object`. A first-attempt authentication, rate-limit, or timeout failure is not format-retried.

Successful model assessments persist `summary` and `keywords` in `discovery_article_assessments` and write the live summary and keyword-derived topics to `material.summary` and `material.topics`. Candidate responses expose these through `discovery_candidate_details`; `discovery_candidates` no longer stores those fields. Metadata writeback does not change the three scores or the quality gate, but topics are used in user profiles, topic blocks, and LLM rerank input. Recommendation cards that already exist keep their frozen snapshots until a new personalized feed or daily digest is generated.

## Summary and keyword backfill

Inventory upgraded from v3 may contain assessment rows without summaries. Owner backfill selects at most 250 representative candidates that lack an active row matching the current material content version, assessor, model version, and policy. It reactivates a matching stored row without a model call or enqueues `assessment_assess_article` on the paused `article_assessment` queue. Jobs identify the material and its content version. Goose `000031` demotes any current `deterministic_rules` pointer to `assessment_state=pending` so those articles wait for LLM re-assessment. Discovery crawl runs automatically on `discovery_crawl`. LLM assessment still requires **推荐中心 → 评估 → 执行 LLM 评估**.

```text
POST /api/admin/discovery/article-assessments/backfill
{"limit":250,"dryRun":true,"retryFailures":false}

POST /api/admin/discovery/article-assessments/backfill
{"limit":250,"dryRun":false,"retryFailures":false}
```

## Use the recommendation-center assessment panel

Sign in as the owner and open **推荐中心 → 评估**. The panel shows pending-queue depth, 24-hour job success/failure, articles/hour, token totals, p50/p95 latency, schema retry rate, output token/s, and estimated time to drain the paused assessment queue from `GET /api/admin/assessment/metrics` and `GET /api/admin/assessment/queue`. Click **执行 LLM 评估** to start queue consumption with `POST /api/admin/assessment/queue/run`. Bounded backfill and assessment-pointer rollback remain on the same page. The page and these APIs are owner-only.

## Human-workflow removal in Goose 000036

The human-label workflow and its model double-run acceptance reports have been removed. The former `/api/admin/recommendations/article-assessment-workflow` endpoint and all its subpaths return 404. Goose `000036` deletes the five `article_assessment_workflow_*` tables and all stored batches, labels, model scores, reports, and call audit records. Production article assessments, content versions, recommendation snapshots, and `assessment_llm_calls` are retained.

Stop the old API and workers before starting the new version so an old evaluation cannot write while tables are dropped. Keep a pre-upgrade PostgreSQL backup and the old binary/image if recovery is required. The migration refuses Down; recovering the removed records requires restoring that backup with the matching old version. Published migrations remain in the repository to support both existing databases and fresh installations.

## Activate, backfill, and recover

After the page reports `activationReady=true`, keep `ARTICLE_ASSESSMENT_MODE=active` and watch new articles for 48 hours. Confirm `llm_call` reasoning tokens remain zero, output validity and latency are stable, and eligibility/ranking distributions are not saturated. Then use the owner-only endpoints, always previewing first:

```text
POST /api/admin/discovery/article-assessments/backfill
{"limit":250,"dryRun":true,"retryFailures":false}

POST /api/admin/discovery/article-assessments/backfill
{"limit":250,"dryRun":false,"retryFailures":false}
```

The hard maximum is 250. A stored model row matching the current material content version, assessor, model version, and policy is activated without another model call, and its summary/topics are written back to material. Otherwise the material article state is marked assessment-pending and the paused `article_assessment` queue is used; the owner must click **执行 LLM 评估** before those jobs call the model. Backfill preserves the current pointer while marking assessment pending. On model failure, only an existing model row can remain the active assessment; rule rows are not retained as a fallback. Queue insertion failure leaves durable pending state for startup recovery. A later failure for the current policy is skipped by ordinary batches; set `retryFailures:true` after its cause is corrected, then click **执行 LLM 评估** again. Assessment jobs have `MaxAttempts=1`, so River does not retry a failed assessment job automatically.

Rollback never deletes assessments or edits published recommendation snapshots. Preview and then reactivate another same-content **model** assessment with the endpoints below. Rollback selects candidates whose active row matches the currently configured assessor/model/policy, prefers a different policy, then takes the highest row ID. It excludes the current row and deterministic rules; it does not require the target row ID to be older.

```text
POST /api/admin/discovery/article-assessments/rollback
{"limit":250,"dryRun":true}

POST /api/admin/discovery/article-assessments/rollback
{"limit":250,"dryRun":false}
```

If model quality degrades, stop any active assessment run by stopping the API and restart with the queue paused. Preview and run bounded rollback batches while configuration still matches the model/policy being rolled back, then repair the model configuration. There is no queue-stop HTTP endpoint. Do not switch to observe mode or activate `deterministic_rules`. Preserve production assessment rows and logs for diagnosis.
