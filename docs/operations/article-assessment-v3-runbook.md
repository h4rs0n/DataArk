# Article Assessment v3 Operations Runbook

This runbook is the release gate for `article-value-v3`. The evaluator judges one article from its title and already-extracted plain body text. It never receives raw HTML, source or site identity, author reputation, popularity, publication date, graph position, or user feedback. The model receives at most an approximately 6,000-token evidence package; oversized bodies retain beginning, middle, and ending excerpts. Assessment and rerank calls disable supported model reasoning modes, cap completion at 1,024 tokens, and write per-attempt stage, response mode, evidence size, duration, and provider prompt/completion/reasoning/cache token counts to retained `llm_call` logs. Payload text is intentionally never logged.

## Rollout modes and configuration

Keep the initial deployment in observe mode:

```text
ARTICLE_ASSESSMENT_MODE=observe
ARTICLE_ASSESSMENT_CONCURRENCY=2
```

`observe` persists a valid v3 model row but leaves the current assessment pointer unchanged. `active` atomically activates valid v3 rows. A model failure retains an existing active row; a new article with no previous score activates the conservative deterministic row with state `degraded`. After existing article-validity gates, only semantic quality below 20/100 is ineligible. Confidence does not independently reject an article.

The structured response has exactly three integer scores from 0 through 100 and exactly two short reasons: `qualityScore`, `depthScore`, `evergreenScore`, and `reasons`. The provider probes strict JSON Schema once per endpoint/model process and makes at most one compatible JSON-object retry. Authentication, rate-limit, and timeout failures are not format-retried.

## Build the private human gold set

The labels come from DataArk's current PostgreSQL inventory, specifically each representative candidate's immutable current `discovery_article_content_versions` row. They do not come from BlogClaw, public benchmarks, a site's reputation, or an LLM. Sampling is deterministic for a fixed seed, includes 80 Chinese/English core items across five body-length buckets plus 40 score/boundary/disagreement/overlong stress items, deduplicates content hashes, and permits at most two articles per host.

Build the tool, create a private directory outside the repository, and generate pass one:

```sh
cd api
go build -o /tmp/article-assessment-eval ./cmd/article-assessment-eval

DATAARK_ASSESSMENT_DSN='host=<host> port=5432 dbname=<db> user=<user> password=<password> sslmode=disable' \
  /tmp/article-assessment-eval sample \
  --seed article-value-v3-gold-v1 \
  --out-dir /tmp/dataark-article-assessment-gold
```

Open `pass1.html` locally with network access disabled and label all 120 articles. The self-contained page stores progress in browser local storage and exports a JSON file without article bodies. Keep `manifest.json`, HTML, exported labels, scores, logs, and reports private and outside Git; the manifest and HTML contain article text.

At least 72 hours after pass one was completed, create the blind 30-item repeat page. The command rejects an early attempt, and the page contains neither prior labels nor baseline/model scores:

```sh
/tmp/article-assessment-eval pass2 \
  --manifest /tmp/dataark-article-assessment-gold/manifest.json \
  --pass1-labels /private/path/article-assessment-pass-1.json
```

After pass two, create an adjudication page when repeat scores cross a rubric band or differ by more than 15 points:

```sh
/tmp/article-assessment-eval adjudicate \
  --manifest /tmp/dataark-article-assessment-gold/manifest.json \
  --pass1-labels /private/path/article-assessment-pass-1.json \
  --pass2-labels /private/path/article-assessment-pass-2.json
```

## Score and evaluate

Run two model passes with the production endpoint/model. Credentials are environment-only; the default concurrency is two. The command writes a private score file and a structured per-attempt token log:

```sh
LLM_BASE_URL='<openai-compatible-base-url>' \
LLM_API_KEY='<secret>' \
LLM_CHAT_MODEL='<model>' \
LLM_TIMEOUT='300s' \
  /tmp/article-assessment-eval score \
  --manifest /tmp/dataark-article-assessment-gold/manifest.json \
  --repeat 2 \
  --concurrency 2
```

Generate the gate report, adding `--adjudication <exported-json>` when conflicts were adjudicated:

```sh
/tmp/article-assessment-eval report \
  --manifest /tmp/dataark-article-assessment-gold/manifest.json \
  --pass1-labels /private/path/article-assessment-pass-1.json \
  --pass2-labels /private/path/article-assessment-pass-2.json \
  --scores /tmp/dataark-article-assessment-gold/scores.json \
  --llm-log /tmp/dataark-article-assessment-gold/llm-calls.log
```

Do not activate unless `activationReady` is true. The fixed gates cover human repeat consistency; quality/depth/evergreen rank and error; top/bottom quintiles; improvement over the stored active baseline; the 20-point eligibility gate; model repeat stability; at least 98% valid output; zero provider-reported reasoning tokens; prompt-token p95 no more than 7,500; and no prompt above 8,500 tokens.

## Activate, backfill, and recover

After the report passes, deploy `ARTICLE_ASSESSMENT_MODE=active` and observe new articles for 48 hours. Confirm `llm_call` reasoning tokens remain zero, output validity and latency are stable, and eligibility/ranking distributions are not saturated. Then use the owner-only endpoints. Always preview first:

```text
POST /api/admin/discovery/article-assessments/backfill
{"limit":250,"dryRun":true,"retryFailures":false}

POST /api/admin/discovery/article-assessments/backfill
{"limit":250,"dryRun":false,"retryFailures":false}
```

The hard maximum is 250. A stored observe-mode v3 row is activated without another model call. Otherwise the candidate is marked assessment-pending and the existing idempotent process-candidate queue is used. The old current pointer remains valid until success. Queue insertion failure leaves durable pending state for startup recovery. A v3 failure is skipped by later ordinary batches; set `retryFailures:true` only after its cause is corrected.

Rollback never deletes assessments or edits published recommendation snapshots. Preview and then reactivate the latest prior same-content assessment (or the v3 deterministic row) with:

```text
POST /api/admin/discovery/article-assessments/rollback
{"limit":250,"dryRun":true}

POST /api/admin/discovery/article-assessments/rollback
{"limit":250,"dryRun":false}
```

If model quality degrades during rollout, first return the service to observe mode so new model rows cannot become active, then run bounded rollback batches. Preserve all rows and logs for diagnosis.
