# Article Assessment v3 Operations Runbook

This runbook is the release gate for `article-value-v3`. Article assessment receives only the title and already-extracted plain body text. It never receives raw HTML, source or site identity, author reputation, popularity, publication date, graph position, or user feedback. The model receives at most an approximately 6,000-token evidence package; oversized bodies retain beginning, middle, and ending excerpts. Assessment and rerank calls disable supported model reasoning modes, cap completion at 1,024 tokens, and write per-attempt stage, response mode, evidence size, duration, and provider token counts to retained `llm_call` logs. Payload text is never logged.

## Rollout modes and configuration

Keep the initial deployment in observe mode:

```text
ARTICLE_ASSESSMENT_MODE=observe
ARTICLE_ASSESSMENT_CONCURRENCY=2
```

`observe` persists a valid v3 model row but leaves the current assessment pointer unchanged. `active` atomically activates valid v3 rows. A model failure retains an existing active row; a new article with no previous score activates the conservative deterministic row with state `degraded`. After existing article-validity gates, only semantic quality below 20/100 is ineligible. Confidence does not independently reject an article.

The structured response has exactly three integer scores from 0 through 100 and exactly two short reasons: `qualityScore`, `depthScore`, `evergreenScore`, and `reasons`. The provider probes strict JSON Schema once per endpoint/model process and makes at most one compatible JSON-object retry. Authentication, rate-limit, and timeout failures are not format-retried.

## Use the recommendation-center human workflow

Sign in as the owner and open **推荐中心 → 人工标注工作流**. The page and all corresponding APIs are owner-only. A new run selects the gold set from the current PostgreSQL inventory, specifically each representative candidate's immutable current `discovery_article_content_versions` row. Labels never come from BlogClaw, public benchmarks, site reputation, or an LLM.

Click **创建标注批次** once. The server persists a frozen 120-item manifest in `article_assessment_workflow_runs` and `article_assessment_workflow_items`; it references immutable content-version rows rather than placing full article bodies in downloadable files. Sampling is deterministic for the stored seed, deduplicates content hashes, permits at most two articles per host, and contains 80 Chinese/English core articles across five body-length buckets plus 40 stress articles: eight each for low active score, quality boundary, saturated high score, model/rule disagreement, and overlong body.

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

After the page reports `activationReady=true`, deploy `ARTICLE_ASSESSMENT_MODE=active` and observe new articles for 48 hours. Confirm `llm_call` reasoning tokens remain zero, output validity and latency are stable, and eligibility/ranking distributions are not saturated. Then use the owner-only endpoints, always previewing first:

```text
POST /api/admin/discovery/article-assessments/backfill
{"limit":250,"dryRun":true,"retryFailures":false}

POST /api/admin/discovery/article-assessments/backfill
{"limit":250,"dryRun":false,"retryFailures":false}
```

The hard maximum is 250. A stored observe-mode v3 row is activated without another model call. Otherwise the candidate is marked assessment-pending and the existing idempotent process-candidate queue is used. The old current pointer remains valid until success. Queue insertion failure leaves durable pending state for startup recovery. A v3 failure is skipped by later ordinary batches; set `retryFailures:true` only after its cause is corrected.

Rollback never deletes assessments or edits published recommendation snapshots. Preview and then reactivate the latest prior same-content assessment, or the v3 deterministic row, with:

```text
POST /api/admin/discovery/article-assessments/rollback
{"limit":250,"dryRun":true}

POST /api/admin/discovery/article-assessments/rollback
{"limit":250,"dryRun":false}
```

If model quality degrades during rollout, first return the service to observe mode so new model rows cannot become active, then run bounded rollback batches. Preserve assessment rows, workflow rows, and logs for diagnosis.
