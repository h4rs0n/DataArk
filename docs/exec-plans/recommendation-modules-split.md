# Split recommendation into discovery, assessment, and ranking modules

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds.

This document must be maintained in accordance with `PLANS.md` at the repository root.

## Purpose / Big Picture

After this change, DataArk's recommendation center is three independently scheduled modules instead of one `discovery_process_candidate` job that fetches HTML, decides whether the page is an article, and calls the LLM.

Discovery outputs only article URLs that have already passed an adjustable rule gate, plus extracted body text, onto a pending-assessment queue. Assessment is the slow stage: it writes summary, topic keywords, and quality scores into recommendation inventory and exposes token and latency metrics. Recommendation only filters that inventory for a user and records feedback.

An operator can observe: listing and login URLs never appear in the assessment queue; a manually subscribed blog is walked through archive and pagination until the cursor is exhausted, without any Sitemap request; blogroll-discovered sites stay incremental; the owner assessment panel shows queue depth, duration percentiles, and token totals; article crawl runs automatically; LLM assessment requires an explicit queue run.

## Progress

- [x] (2026-08-18 17:35+08:00) Authored this ExecPlan from the confirmed product split and current tree.
- [x] (2026-08-18) Extract `api/discovery/articlerules` and apply URL plus body gates at ingest and before assessment enqueue.
- [x] (2026-08-18) Split `ProcessCandidate` from `AssessArticle`, add auto-running `article_assessment` queue, recover pending assessments onto that queue.
- [x] (2026-08-18) Create `api/llm` and `api/assessment`, move orchestration and LLM assessment out of `recommendation` and `discovery`.
- [x] (2026-08-18) Persist safe LLM call rows and expose owner assessment metrics API plus UI panel.
- [x] (2026-08-18) Remove Sitemap code, API, UI, and docs; unlimited ingest plus aggressive archive backfill for `user_managed` seeds; keep bounded backfill for blogroll sites.
- [x] (2026-08-18 20:50+08:00) Invert scheduling: automatic `discovery_crawl` workers, paused `article_assessment` queue with owner **执行 LLM 评估**.

## Surprises & Discoveries

- Observation: `ProcessCandidateWithAssessor` is only called from the API composition root. Tests call `ProcessCandidate` then assert `assessment_state=ready`, so a missing job queue must still run rule assessment in-process or those tests will fail for a reason unrelated to article rules.
  Evidence: `api/api/controller.go` and `api/discovery/candidate_processing_m7_test.go`.
- Observation: Goose now collects 29 migrations ending at `000029_assessment_llm_calls.sql`. `000028` must update already-paused sitemap cursors, because `000020` paused them with `sitemap_requires_owner_request` and a `status NOT IN ('paused')` filter would leave the old reason in place.
  Evidence: `TestV3GooseMigrationIsAdditiveAndParseable` and `api/migrations/000028_remove_sitemap_capability.sql`.
- Observation: `package discovery` tests cannot import `assessment` if `assessment` imports `discovery`. That would be a test import cycle. Assessment persistence therefore stays in `discovery.AssessCandidate`; `api/assessment` wraps the public entry points and owns the LLM client, auto queue, and metrics.
  Evidence: Go test import cycle when a `discovery` `_test.go` imported `assessment`.

## Decision Log

- Decision: Manual subscriptions try to collect every article via full Feed items plus archive/pagination until the cursor is exhausted. Blogroll/observing sites keep incremental Feed plus bounded archive backfill. Sitemap is deleted entirely.
  Rationale: The operator chose this split. Without Sitemap, “all articles” means walk HTML history, not fetch `sitemap.xml`.
  Date/Author: 2026-08-18 / user and Cursor
- Decision: The assessment River queue named `article_assessment` is paused until the owner clicks run. Discovery crawl on `discovery_crawl` runs automatically.
  Rationale: The operator asked to invert the previous split: network fetch should not wait for a button, while LLM spend remains an explicit action.
  Date/Author: 2026-08-18 / user and Cursor
- Decision: Keep table `discovery_article_assessments`. Do not rename it.
  Rationale: A table rename has no user-visible benefit and would churn snapshots, foreign keys, and migrations.
  Date/Author: 2026-08-18 / Cursor
- Decision: Shared HTTP chat lives in `api/llm`. Assessment owns `AssessArticle`. Recommendation keeps rerank, digest summary, and embeddings.
  Rationale: Assessment must not import recommendation. Duplicating `chatJSON` would drift usage logs.
  Date/Author: 2026-08-18 / Cursor
- Decision: Do not restore `Enrich()`. Summary and keywords remain the single assessment JSON object.
  Rationale: A second LLM hop would reintroduce unbounded cost on the old enrichment path.
  Date/Author: 2026-08-18 / Cursor
- Decision: Leave assessment row writes in `discovery` and expose them through `api/assessment`.
  Rationale: Moving the state machine would force discovery tests to import assessment, which then imports discovery.
  Date/Author: 2026-08-18 / Cursor

## Outcomes & Retrospective

The recommendation center remains three independently scheduled modules. Discovery stops at extracted article bodies with `assessment_state=pending` and crawls automatically. Assessment consumes that queue only after the owner clicks **执行 LLM 评估**, writes quality/summary/topics, and shows owner metrics from `assessment_llm_calls`. Recommendation still selects only ready eligible inventory. Manual subscriptions ingest Feed items without the 50-item cap and walk archive pages until the cursor is exhausted; blogroll sites stay bounded. Sitemap parsing, owner gap-fill, and UI are gone.

What remains outside this plan: tuning LLM throughput from the new metrics, and any later rewrite of the ranking formula.

## Context and Orientation

DataArk is a Go API in `api/` plus a Vue 3 UI in `web/`. The recommendation center is `web/src/views/RecommendationsView.vue`. Shared jobs live in `api/jobqueue`. PostgreSQL uses River; SQLite tests use `MemoryQueue`.

A discovery source is a homepage, Feed, or RSSHub endpoint. `user_managed=true` marks a human subscription (priority 2000). Blogroll-discovered sites use priority 1000 and start as `observing`. A candidate is one discovered URL in `discovery_candidates`. Processing states include `fetch_pending`, `ready`, `ineligible`. Assessment state `pending` means the body is extracted and the LLM has not finished. Eligibility `eligible` plus processing `ready` is recommendation inventory.

`ProcessCandidate` in `api/discovery/candidate_processing.go` currently fetches the article, runs `ExtractArticle` in `api/discovery/extractor.go`, marks non-articles ineligible, dedupes, and immediately calls `AssessCandidate` in `api/discovery/assessor.go`. The LLM adapter is `recommendation.EnrichmentArticleAssessor` wrapping `OpenAICompatibleProvider` in `api/recommendation/openai_provider.go`. Token logs already emit `llm_call` through `api/observability` but are not aggregated in the database.

Article URL heuristics currently live as `isNonArticleNavigation` in `api/discovery/endpoint_discovery.go`. Feed ingest in `parseFeedCandidates` does not use them. Homepage and Feed ingest are truncated by `scoreAndLimitCandidates` using `DISCOVERYMAXCANDIDATES` (default 50).

Sitemap used to be owner gap-fill: `BackfillStrategySitemap`, `POST /api/discovery/sites/:id/sitemap-backfill`, and `SiteInsightPanel.vue`. That path is deleted. Residual sitemap sources are disabled by Goose `000028`; leftover sitemap backfill rows are paused with `completion_reason=sitemap_removed`.

Import rule after the split: `discovery` imports neither `assessment` nor `recommendation`. `assessment` may import `discovery`, `articlevalue`, and `llm`. `recommendation` may import `discovery`, `assessment`, and `llm`.

## Plan of Work

First, add `api/discovery/articlerules` with a commented rule table for URL admission and a body gate that uses title, extracted text, language, `IsArticle`, and login-form evidence. Call the URL gate from Feed parsing, homepage link collection, and archive parsing. Call the URL gate before fetching in `ProcessCandidate` and the body gate after extract. Keep `upsertDiscoveryCandidate` willing to store a bad URL so processing tests can still construct fixtures.

Second, stop calling `AssessCandidate` from `ProcessCandidate`. After extract and dedupe, set `assessment_state=pending` and `EnqueueAssessArticle`. If no job queue is installed, run rule assessment in-process so existing SQLite tests keep a complete pipeline. Add job kind `assessment_assess_article` on River queue `article_assessment` with automatic workers. Memory enqueue for this kind runs the handler immediately, like `EnqueueGenerateDaily`. Discovery recovery must not enqueue `assessment_pending` onto `discovery_crawl`. Assessment recovery enqueues those rows onto the new queue. Backfill admin must enqueue assess jobs, not process-candidate jobs.

Third, create `api/llm` with the OpenAI-compatible HTTP client, `ChatJSON`, embeddings, thinking switches, and `llm_call` logging. Create `api/assessment` with `AssessCandidate`, rule and model assessors, observe/active activation, backfill/rollback, and the assess job handler. Point `bootstrap.configureDomainDatabases` at `assessment.SetDB`. Move `ConfiguredArticleAssessor` out of recommendation. Keep `discovery_article_assessments` rows.

Fourth, persist one compact `assessment_llm_calls` row per chat attempt using the same safe fields as `observability.Event` (no prompt or completion text). Add `GET /api/admin/assessment/metrics` for queue depth, 24-hour success/failure, articles per hour, token totals, p50/p95 duration, and schema retry rate. Add an owner “评估” panel. Keep the human labelling workflow.

Fifth, delete Sitemap parsing, `FetchKindSitemap`, endpoint type, owner API, UI, robots Sitemap consumption, and tests. Migration `000028` disables leftover sitemap sources and pauses sitemap backfill rows with reason `sitemap_removed`. For `user_managed` sites, do not truncate Feed/homepage candidates at 50, always start archive backfill (homepage plus historical navigation), and schedule the next archive batch immediately while the cursor has work. Observing/blogroll sites keep the 50-item cap and the 7-day empty-backfill interval. Follow Atom/RSS `rel=next` only for user-managed Feeds.

Sixth, leave selection, digest, feedback, and inventory algorithms in `api/recommendation`. Regroup the Vue tabs: 推荐 (feed, today, history, settings), 发现 (subscriptions, graph, crawl queue, blacklist), 评估 (metrics, labelling workflow, backfill). Update `README.md`, `README_en.md`, and `docs/operations/article-assessment-v3-runbook.md`. New Go comments are in Chinese. Files are UTF-8.

## Concrete Steps

From the repository root, after each milestone, format and test:

    cd api
    gofmt -w $(git diff --name-only -- '*.go')
    go test ./discovery ./discovery/articlerules ./assessment ./llm ./recommendation ./jobqueue ./bootstrap ./api ./assessmenteval -count=1

Then:

    go test ./... -count=1
    go test -race ./discovery ./assessment ./recommendation ./jobqueue -count=1

From `web/`:

    npm test
    npm run build

Expected: packages pass; `TestV3GooseMigrationIsAdditiveAndParseable` reports the new migration count; frontend contract tests no longer mention Sitemap gap fill.

## Validation and Acceptance

`TestArticleRulesRejectTagLoginAndFeedListingURLs` fails before the rule table exists and passes after: `/tag/`, `/login`, and a Feed item whose link is `/category/go` are rejected at the URL gate. `TestProcessCandidateDoesNotEnqueueAssessmentForNonArticle` shows `assessment_state` never becomes a model call path for those URLs.

After the job split, `ProcessCandidate` on a valid article leaves `processing_state=ready` and `assessment_state=pending` when a queue is installed, and a following `AssessArticle` job writes quality, summary, and topics. Crawl snapshot kinds do not include `assessment_assess_article`.

After Sitemap removal, creating a source whose URL ends in `sitemap.xml` is rejected or ignored as a sitemap type, `requestSitemapBackfill` is gone, and the deterministic world still finds the archive-only high-value article through `archive` backfill with zero `FetchKindSitemap` requests.

Owner `GET /api/admin/assessment/metrics` as a member returns 403; as owner returns JSON with `pendingQueue` and `llmUsage`. Crawl runs automatically. LLM assessment requires `POST /api/admin/assessment/queue/run`.

Existing feedback tests `Test(FeedbackM13|...)` continue to pass: recommendation still reads eligible assessed inventory.

## Idempotence and Recovery

Goose Down statements are data-preserving no-ops. Re-running assess jobs hits the immutable assessment unique key and reloads the stored row. Enqueue uniqueness remains `ByArgs`. Restart recovery stages due crawl jobs onto the automatic discovery queue and pending assessments onto the paused assessment queue. Sitemap rows are disabled, not deleted, so historical candidates remain.

## Artifacts and Notes

Intended assess enqueue after a ready body:

    assessment_state=pending
    job kind=assessment_assess_article
    queue=article_assessment

Intended metrics event dual-write: one `dataark_event` `llm_call` log line plus one `assessment_llm_calls` row with integer token counts only.

## Interfaces and Dependencies

In `api/discovery/articlerules`, define:

    func RejectURL(rawURL, anchorText string) (reason string, rejected bool)
    func RejectPage(page Page, minimumCharacters int) (reason, category string, review bool)

In `api/jobqueue`, extend `JobEnqueuer` and `Handlers` with:

    EnqueueAssessArticle(context.Context, uint, string) error
    AssessArticle func(context.Context, uint, string) error

Job kind `assessment_assess_article`. Queue name `article_assessment`. Insert options: unique by args, max attempts 1, not the paused crawl queue.

In `api/llm`, define:

    type Client struct { BaseURL, APIKey, ChatModel, EmbeddingModel string; Timeout time.Duration; HTTPClient HTTPDoer; CallObserver func(observability.Event) }
    func (Client) ChatJSON(ctx context.Context, messages []map[string]string, output interface{}, options ChatOptions) (string, error)
    func (Client) Embed(ctx context.Context, texts []string) ([][]float32, error)

In `api/assessment`, define:

    type ArticleAssessor interface { Name() string; Version() string; PolicyVersion() string; Assess(context.Context, ArticleAssessmentInput) (ArticleAssessmentResult, error) }
    func AssessCandidate(ctx context.Context, candidateID uint, enhanced ArticleAssessor) error
    func RecoverDueJobs(ctx context.Context, queue jobqueue.JobEnqueuer, now time.Time) error
    func ConfiguredArticleAssessor() ArticleAssessor

`api/api/controller.go` `startApplicationJobQueue` wires `ProcessCandidate` to `discovery.ProcessCandidate` and `AssessArticle` to `assessment` with `ConfiguredArticleAssessor`. Recovery joins `discovery.RecoverDueJobs` and `assessment.RecoverDueJobs` and `recommendation.RecoverDueJobs`.

Revision note (2026-08-18): living ExecPlan after the three-module split landed. Goose count is 29. Assessment persistence stays in discovery to avoid a test import cycle.
