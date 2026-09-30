# Article Assessment Operations Runbook

This runbook is the release gate for `article-value-v4`, which extends the v3 reading-value rubric with a required summary and keywords. Article assessment receives only the title and already-extracted plain body text. It never receives raw HTML, source or site identity, author reputation, popularity, publication date, graph position, or user feedback. The model receives at most an approximately 6,000-token evidence package; oversized bodies retain beginning, middle, and ending excerpts. Assessment and rerank calls disable supported model reasoning modes, send `temperature=0.7`, `top_p=0.80`, `top_k=20`, `min_p=0.0`, `presence_penalty=1.5`, `repetition_penalty=1.0`, cap assessment completion at 32,768 tokens and rerank completion at 2,048 tokens, and write per-attempt stage, response mode, evidence size, duration, provider token counts, and a compact `error_message` to retained `llm_call` logs. Payload text is never logged.

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

## Use the recommendation-center human workflow

Sign in as the owner and open **推荐中心 → 评估**. The metrics panel shows pending-queue depth, 24-hour complete-job success/failure, articles/hour, token totals, p50/p95 latency, schema retry rate, output token/s, and estimated time to drain the paused assessment queue from `GET /api/admin/assessment/metrics` plus the queue snapshot. The human-labelling workflow and bounded backfill/rollback live on the same page. The page and all corresponding APIs are owner-only. A new run selects the gold set from the current PostgreSQL inventory, specifically each representative candidate's immutable current `discovery_article_content_versions` row. Labels never come from BlogClaw, public benchmarks, site reputation, or an LLM.

Click **创建标注批次** once. The server persists a frozen 120-item manifest in `article_assessment_workflow_runs` and `article_assessment_workflow_items`; it references immutable content-version rows rather than placing full article bodies in downloadable files. Sampling is deterministic for the stored seed, deduplicates content hashes, permits at most two articles per host, and contains 80 Chinese/English core articles across five body-length buckets plus 40 stress articles: eight each for low active score, quality boundary, saturated high score, quality/depth disagreement, and overlong body.

The browser requests one current article at a time. Its response contains title, clean body text, language, character count, anonymous sample ID, and the current human label only. It does not contain host, source, stratum, baseline score, rule score, or model score. Responses use `Cache-Control: no-store`.

For each article, enter integer quality, depth, and evergreen scores from 0 through 100, a concise reason, and a genre. Mark an article `extractionBad` when clean-text extraction is materially broken; mark it `unjudgeable` when no defensible scores can be assigned. The rubric bands are 0–19 no meaningful value, 20–39 weak, 40–59 ordinary, 60–74 good, 75–89 excellent, and 90–100 rare and exceptional. Judge intrinsic article content only.

The first-pass pool contains 120 articles, but only 30 saved labels are required. Every label and explicit **跳过** action is durable in PostgreSQL, so closing the browser does not lose progress. **保存并上一篇** saves a complete current label before moving back; when the current form is completely blank, it simply opens the previous article without creating or changing a label. A partially filled form still requires completion before it can be saved. Saving a previously skipped article removes its skip marker; skipping a previously labelled article removes that first-pass label. After at least 30 labels, either continue labelling more articles or click **完成第一轮**. Submission freezes the saved labels, records every remaining unlabelled article as skipped, and starts the blind interval.

The second-pass button remains disabled until at least 72 hours after pass-one completion. When the countdown reaches zero, click **开始第二轮**. The server fixes a blind 30-item subset drawn only from first-pass-labelled articles, preferring 20 core and 10 stress articles and deterministically filling a quota shortage from the other group. Thus a minimum-size first pass repeats all 30 labels; a larger first pass repeats a deterministic subset. The page does not expose pass-one answers. Complete and freeze all 30.

After pass two, the server identifies any repeated item whose score crosses a rubric band or differs by more than 15 points on any axis. Those articles enter **冲突复核**. Complete the final scores for every conflict. If there are no conflicts, the workflow moves directly to `human_complete`.

## Run model evaluation and inspect the gate

After human gold is complete, click **运行模型双跑验收**. The server uses the configured production OpenAI-compatible endpoint and model, with concurrency two, and scores every article twice. The request returns immediately while 240 score results and every payload-safe call event are persisted in workflow tables. The page polls progress; a server restart marks an interrupted evaluation retryable instead of silently treating it as complete.

Failed model outputs are retained as failed score records. After all work finishes, the server builds and persists the fixed gate report. Model protocol validity still covers all 240 model results, while human-consistency and model-versus-human accuracy use only articles with usable human labels; skipped and unjudgeable items are excluded rather than treated as scores. The recommendation-center page displays aggregate metrics and every activation check. Do not activate unless `activationReady` is true. Gates cover human repeat rank, error, and score-band agreement; model rank, error, head/tail recognition, baseline improvement, eligibility behavior, and repeat stability; at least 98% valid output; zero provider-reported reasoning tokens; prompt-token p95 no more than 7,500; and no prompt above 8,500 tokens.

The old `api/cmd/article-assessment-eval` executable, self-contained HTML generator, browser-local storage, downloadable manifests, and local JSON/log/report files have been removed. The database workflow is now the only supported human-label and evaluation path.

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

If model quality degrades, stop any active assessment run by stopping the API and restart with the queue paused. Preview and run bounded rollback batches while configuration still matches the model/policy being rolled back, then repair the model configuration. There is no queue-stop HTTP endpoint. Do not switch to observe mode or activate `deterministic_rules`. Preserve assessment rows, workflow rows, and logs for diagnosis.
