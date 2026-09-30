# Recommendation v3 Operations Runbook

This runbook covers the Blogroll discovery, article assessment, per-user recommendation, feedback, and immutable daily-digest pipeline. Chat model configuration is required: `-llm-chat-model` and `-llm-base-url` must be set or the process refuses to start. Embedding remains optional. Assessment stays on a paused manual queue.

## Upgrade prerequisites

Back up PostgreSQL and the archive directory before changing the running binary. Keep the old binary available, record counts for users, discovery sources, candidates, recommendation days, recommendation items, and feedback, and do not run Down migrations as a rollback mechanism. The current schema ends at `000035`. Migrations `000032`–`000035` move content and state into material tables and drop the old candidate columns; follow [the material migration runbook](material-migration-runbook.md) when crossing that boundary. Startup runs compatibility backfills once on PostgreSQL and records the `v3-compatibility` checkpoint; later starts skip that completed work.

Blogroll targets remain `observing` until a single homepage response supplies deterministic blog evidence. They become `active` before endpoint, Blogroll, or backfill expansion. Targets without evidence become `non_blog`, keep their graph edges and `blog_verification:<reason>` operational detail, disable the homepage endpoint, and disappear from the subscriptions projection. If an owner confirms a false negative, change the site status to `active`; this re-enables and immediately schedules its homepage without deleting the original decision evidence from fetch and graph history.

For production PostgreSQL, start the database first and then start one API instance. Startup applies Goose and River migrations, resumes `discovery_crawl`, and pauses `article_assessment` before workers accept jobs. It moves unfinished discovery work from River's historical `default` queue into `discovery_crawl`, resets interrupted crawl jobs to available, and enqueues due discovery work for automatic execution. Assessment work is recovered onto the paused manual queue. Daily recommendation generation and digest summaries remain on River's default queue. Confirm the `vector` extension when using embeddings; ranking no longer uses an embedding score formula. A representative isolated validation is:

```sh
cd docker
docker compose up -d database
docker compose up -d --build dataarkapi
docker compose restart dataarkapi
docker compose logs --no-color dataarkapi
```

After the restart, verify that migrations are current, crawl jobs resume automatically on `discovery_crawl`, published digests are unchanged, and no secret is printed in structured `dataark_event` records. Sign in as owner, inspect **Recommendation Center → Discovery → Crawl Task Queue** for automatic fetch progress, then open **Recommendation Center → Assessment** and click **执行 LLM 评估** when you are ready to spend model tokens. Never place production tokens, passwords, private Feed bodies, or user content in fixtures, Compose files, command history, or this runbook.

Before a production rollout, run the repository's opt-in database gate against a disposable empty database whose name begins with `dataark_v3_verify`:

```sh
cd api
DATAARK_POSTGRES_TEST_DSN='host=127.0.0.1 port=5432 user=<test-user> password=<test-password> dbname=dataark_v3_verify sslmode=disable' \
  go test ./bootstrap -run TestPostgresV3MigrationsRiverRestartAndPGVector -count=1 -v
```

The test refuses other database names. It applies all Goose and River migrations, repeats the migration path, checks PostgreSQL JSON fields, writes and reads a pgvector value, starts the River runtime twice, and completes deterministic synthetic jobs. Drop the disposable database after the test; never point this command at production or a database containing user data.

## Staged rollout and rollback

Use the following order and reconcile counts before advancing. An upgrade across the material migrations requires a maintenance window and backup restoration to roll back. `-discover-interval=0` disables only the periodic scheduler: startup recovery, queued jobs, and owner actions can still crawl. Pause or disable the relevant sites/sources before starting the API if discovery must remain dormant.

1. Stop the old API/workers, back up PostgreSQL and archives, and deploy the current schema and binary with `-recommend-enabled=false`. Across `000032`–`000035`, rollback requires stopping the new API and restoring the pre-upgrade database/archive backup with the matching old binary.
2. Allow startup material reconciliation, archive-content backfill, and checkpoint-controlled v3 compatibility backfill to finish. Compare pre/post counts and inspect `material_ingestion_issues`; preserve backfilled rows and frozen item evidence.
3. Enable the shared workers and safe fetcher for a small set of owner-approved seed sites. Pause a site with the owner API to stop fetch, graph, and unfinished backfill work without deleting candidates.
4. Expand Blogroll scanning and HTML archive/pagination backfill for those seeds. Manual subscriptions ingest all Feed items and walk archive history; Blogroll-discovered sites use bounded incremental work. Sitemap crawling and robots Sitemap hints are disabled. If failures rise, pause the affected site; existing graph and provenance remain usable.
5. Let article processing run automatically, then explicitly start LLM assessment from the owner assessment panel. Compare ready, review, ineligible, duplicate, and eligible counts. Assessment jobs have `MaxAttempts=1`; a failed assessment needs owner backfill with `retryFailures:true`, followed by another manual queue run. Provider JSON retries occur inside that single job. Rerank failure fails daily generation; no rule scores or degraded digest are substituted.
6. Enable `-recommend-enabled=true` for a test deployment and test users. Validate at least two user-local dates, target N/actual M explanations, per-user blocks, immutable retries, and owner metrics. Daily ranking and discovery feed require a successful LLM rerank; digest summaries require a successful LLM summary job.
7. Expand to all users after a stable publication window. Stop old clients that expect global candidate status writes. The current API already writes personal read, ignore, archive, exposure, and feedback state only to the authenticated user's overlay.
8. Verify candidates and archive documents reference material rows and inspect migration checkpoints. The old candidate content, assessment, and single-source columns have already been removed by `000034`; legacy extraction and provenance tables remain for audit and workflow references.

There is no destructive “rebuild published digest” rollback path. `POST /api/admin/recommendations/generate` is retained as a compatibility name but performs an idempotent safe retry; `POST /api/admin/recommendations/supplement` only appends missing items with audit evidence. Disable scheduling to stop future daily generation. Restoring a previous binary requires a compatible database; across the material migration boundary use the backup restoration procedure above.

## Permissions and data semantics

All endpoints require authentication. The server derives user identity from authentication context; clients cannot select another user ID.

| Operation | Role | Semantics |
| --- | --- | --- |
| Personalized discovery feed and candidate read/archive | member or owner | Feed reads and actions use the authenticated user's material state; shared candidate status is unchanged. |
| Settings, feedback, revert, block removal, preference reset | member or owner | User-scoped; history is retained and current state is replaceable/revertible. |
| Digest today/history/item context | member or owner | User-scoped frozen snapshots; another user's item is not visible. |
| Source list, site graph and backfill coverage | member or owner | Authenticated reads of shared discovery information. |
| Source create/update/delete/fetch, site pause/resume/block, backfill requests and site operations | owner | Shared operational state; pause/block preserves candidates and evidence. |
| Digest retry/supplement and product metrics | owner | Retry is non-destructive; supplement is append-only; metrics contain aggregate operations and safe identifiers. |

Shared data includes logical sites, endpoints, graph edges, fetched article bodies and versions, deduplication identity, article assessments, and provenance. Personal data includes exposure, open/read/deep-read/archive state, feedback, preferences, source/topic/style blocks, and daily digests. Source history never participates in article validity, article quality, or a quality cap.

## Operational diagnosis

Use `GET /api/admin/recommendations/metrics`, site graph/operations/backfill endpoints, candidate inventory, and structured `dataark_event` logs together. Events contain stable IDs, status, error category, local date, counts, and fixed failure diagnostics. Failed HTTP work may include a hostname-only `domain` and integer `http_status`; failed jobs may include a whitespace-compacted, URL-redacted, secret-redacted `error_message` of at most 300 characters. Events deliberately have no arbitrary body/details field.

- Source failure: inspect the latest fetch runs, HTTP/error category, validator, next due time, and failure backoff. A failure for one endpoint must not stop other jobs. Resume by fixing reachability and waiting for or re-requesting the owner fetch; do not reset candidate data.
- robots status: robots.txt is advisory metadata for the shared fetcher. `Allow`, `Disallow`, `Crawl-delay`, and robots unavailability do not stop a request; Sitemap declarations are not consumed. Domain blacklist, SSRF, redirects, size/type limits, timeout, and host rate limits still apply.
- Processing backlog: compare discovered/fetch-pending/ready/review/failed counts and the owner-only crawl queue panel. Restarting the API stages due crawl jobs onto the automatic `discovery_crawl` queue. LLM assessment stays on the paused `article_assessment` queue until the owner clicks **执行 LLM 评估**. Persistent article-type, language, or short-body cases belong in review, not forced eligible.
- Inventory shortage: inspect fresh/evergreen/exploration inventory days and the digest's excluded counts. Hard eligibility, user blocks, cooldown, and duplicate identity are never relaxed. Increase legitimate discovery/backfill coverage or use owner supplement after new eligible items arrive; do not insert ineligible fillers.
- Model failure: a missing chat model refuses process start. Assessment and digest-summary jobs have `MaxAttempts=1`; daily-generation jobs use River's default retry policy. Repair the cause, then explicitly re-enqueue failed assessments and run the manual queue. Missing or stale digest summaries are re-enqueued by startup/scheduler recovery; summary failure leaves published articles available without a template summary. Rerank failure cannot publish a degraded digest. Preserve published item snapshots.
- Missing, draft, or failed digest: startup and the scheduler recover the user/local-date job. Owner safe retry may be used. Published or supplemented days are returned unchanged. A later inventory increase may be handled only by append-only supplement.
- Low-hit starvation: `scheduleFloorViolations` must remain zero. Low historical eligible rate is computed only for reporting; it cannot extend the active/observing/dormant maximum interval. Inspect the saved schedule decision when a violation appears.
- Integrity alarms: `hardFilterViolations`, `duplicateClusterViolations`, and `activeBlockViolations` target zero and use publication-time snapshot evidence. Stop rollout and inspect the affected item/day before changing data; do not delete the digest.

The long-tail gem contribution rate counts distinct positively received recommendation items (`valuable`, `deep_read`, or archive) from a reporting-only low-hit site, divided by all distinct positively received items. A low-hit site currently means at least five discovered candidates and at most ten percent eligible. This grouping is not persisted as a source score.

## Retention and privacy

The system currently performs no automatic destructive retention of discovery bodies, immutable content versions, provenance, assessments, digests, feedback history, legacy-review rows, or operational evidence. This supports audit, retry, and rollback. Operators must treat article bodies and user feedback as retained application data and include them in access control, backups, export, and erasure procedures.

Structured events must never contain full URLs, URL paths or queries, article bodies, Cookies, authorization headers, access tokens, passwords, model keys, response bodies, or arbitrary upstream error payloads. Failure diagnostics are normalized again at the final logging boundary: `domain` is hostname-only, invalid status codes are omitted, and `error_message` is bounded and redacted. Error summaries stored for fetch/processing are bounded operational text; inspect them under owner access. Preference reset starts a new personalization boundary but deliberately keeps historical feedback, digests, and explicit safety blocks. Any future physical deletion requires a separate reviewed migration and referential-integrity reconciliation.

## Release verification

Run the backend and frontend suites (SQLite tests use fake LLM providers; they do not require public Internet or model credentials):

```sh
cd api
go test ./discovery ./recommendation ./jobqueue ./bootstrap ./api ./observability -run 'Test(DeterministicSiteWorld|BlogrollGraphFixture|BackfillFinds|ProcessCandidate|LowHitSource|EndToEndM17|DailyDigestM14|ObservabilityM16|RecoverDueJobs|V3Goose|V3SQLiteMigration)' -count=1
go test -race ./discovery ./recommendation ./jobqueue/... -count=1
go test ./... -count=1
cd ../web
npm test
npm run build
```

Also build the embedded frontend and backend binary. The root `makefile` target `make web` changes the user-global npm registry and uses `npm i`. To use the lockfile, run `npm ci && npm run build` from `web/`, then `make web2api && make api` from the repository root. `make web2api` moves `web/dist` into `api/assets/web`; `make api` runs `go mod tidy` before compiling. PostgreSQL/River/pgvector evidence is required before production rollout; SQLite evidence alone is not a substitute.
