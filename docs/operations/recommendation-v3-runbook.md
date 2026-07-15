# Recommendation v3 Operations Runbook

This runbook covers the Blogroll discovery, article assessment, per-user recommendation, feedback, and immutable daily-digest pipeline. It is intentionally safe to use without an LLM API key, vector extension, real-user fixtures, or public-network access.

## Upgrade prerequisites

Back up PostgreSQL and the archive directory before changing the running binary. Keep the old binary available, record counts for users, discovery sources, candidates, recommendation days, recommendation items, and feedback, and do not run Down migrations as a rollback mechanism. Migrations `000003` through `000018` are additive or data-preserving: they add logical sites, registrable-domain identity, endpoint and graph evidence, provenance, processing and assessment state, per-user state, immutable digest evidence, and operational metrics. Startup reruns compatibility backfills idempotently.

Blogroll targets remain `observing` until a single homepage response supplies deterministic blog evidence. They become `active` before endpoint, Blogroll, or backfill expansion. Targets without evidence become `non_blog`, keep their graph edges and `blog_verification:<reason>` operational detail, disable the homepage endpoint, and disappear from the subscriptions projection. If an owner confirms a false negative, change the site status to `active`; this re-enables and immediately schedules its homepage without deleting the original decision evidence from fetch and graph history.

For production PostgreSQL, start the database first and then start one API instance. Startup applies Goose and River migrations before workers accept jobs. Confirm the `vector` extension only when embeddings are enabled; the rules pipeline does not need it. A representative isolated validation is:

```sh
cd docker
docker compose up -d database
docker compose up -d --build dataarkapi
docker compose restart dataarkapi
docker compose logs --no-color dataarkapi
```

After the restart, verify that migrations are current, interrupted River work resumes once, published digests are unchanged, and no secret is printed in structured `dataark_event` records. Never place production tokens, passwords, private Feed bodies, or user content in fixtures, Compose files, command history, or this runbook.

Before a production rollout, run the repository's opt-in database gate against a disposable empty database whose name begins with `dataark_v3_verify`:

```sh
cd api
DATAARK_POSTGRES_TEST_DSN='host=127.0.0.1 port=5432 user=<test-user> password=<test-password> dbname=dataark_v3_verify sslmode=disable' \
  go test ./bootstrap -run TestPostgresV3MigrationsRiverRestartAndPGVector -count=1 -v
```

The test refuses other database names. It applies all Goose and River migrations, repeats the migration path, checks PostgreSQL JSON fields, writes and reads a pgvector value, starts the River runtime twice, and completes deterministic synthetic jobs. Drop the disposable database after the test; never point this command at production or a database containing user data.

## Staged rollout and rollback

Use the following order. Each step is reversible and must pass count reconciliation before advancing.

1. Deploy the additive schema and binary with `-recommend-enabled=false` and, if discovery must remain dormant, `-discover-interval=0`. Roll back by restoring the previous binary; retain all new tables and columns.
2. Allow startup compatibility backfills to create logical sites, endpoints, provenance, legacy-state review rows, feedback current keys, and frozen item evidence. Compare pre/post counts. Roll back reads to the old binary; do not delete backfilled rows.
3. Enable the shared workers and safe fetcher for a small set of owner-approved seed sites. Pause a site with the owner API to stop fetch, graph, and unfinished backfill work without deleting candidates.
4. Expand Blogroll scanning and bounded historical backfill for those seeds. If error or robots rates rise, pause the affected site or set discovery interval to zero; existing graph and provenance remain usable.
5. Enable article processing and deterministic assessment. Compare ready, review, ineligible, duplicate, and eligible counts. A remote assessor may be enabled later, but its failure must leave the rule assessment active.
6. Enable `-recommend-enabled=true` for a test deployment and test users. Validate at least two user-local dates, target N/actual M explanations, exploration, per-user blocks, immutable retries, and owner metrics.
7. Expand to all users after a stable publication window. Stop old clients that expect global candidate status writes. The current API already writes personal read, ignore, archive, exposure, and feedback state only to the authenticated user's overlay.
8. Keep compatibility columns until at least one additional stable release. Removing them requires a separate audited migration, fresh counts, and a rollback export.

There is no destructive “rebuild published digest” rollback path. `POST /api/admin/recommendations/generate` is retained as a compatibility name but performs an idempotent safe retry; `POST /api/admin/recommendations/supplement` only appends missing items with audit evidence. To roll back recommendation reads, disable scheduling or restore the previous binary while retaining published snapshots and feedback.

## Permissions and data semantics

All endpoints require authentication. The server derives user identity from authentication context; clients cannot select another user ID.

| Operation | Role | Semantics |
| --- | --- | --- |
| Candidate list/read/ignore/archive | member or owner | Reads/writes only the authenticated user's overlay; shared candidate status is unchanged. |
| Settings, feedback, revert, block removal, preference reset | member or owner | User-scoped; history is retained and current state is replaceable/revertible. |
| Digest today/history/item context | member or owner | User-scoped frozen snapshots; another user's item is not visible. |
| Source CRUD/fetch, site pause/resume/block, graph/backfill operations | owner | Shared operational state; pause/block preserves candidates and evidence. |
| Digest retry/supplement and product metrics | owner | Retry is non-destructive; supplement is append-only; metrics contain aggregate operations and safe identifiers. |

Shared data includes logical sites, endpoints, graph edges, fetched article bodies and versions, deduplication identity, article assessments, and provenance. Personal data includes exposure, open/read/deep-read/archive state, feedback, preferences, source/topic/style blocks, and daily digests. Source history never participates in article validity, article quality, or a quality cap.

## Operational diagnosis

Use `GET /api/admin/recommendations/metrics`, site graph/operations/backfill endpoints, candidate inventory, and structured `dataark_event` logs together. Events contain only stable IDs, status, error category, local date, and counts; they deliberately have no arbitrary body/details field.

- Source failure: inspect the latest fetch runs, HTTP/error category, validator, next due time, and failure backoff. A failure for one endpoint must not stop other jobs. Resume by fixing reachability and waiting for or re-requesting the owner fetch; do not reset candidate data.
- robots denial or unavailability: an explicit denial stops the disallowed request. An unavailable robots file is conservative and retryable. Do not bypass robots to restore throughput; correct the site/endpoint or wait for the bounded retry.
- Processing backlog: compare discovered/fetch-pending/ready/review/failed counts and River pending jobs. Restarting the API should re-enqueue due versions idempotently. Persistent article-type, language, or short-body cases belong in review, not forced eligible.
- Inventory shortage: inspect fresh/evergreen/exploration inventory days and the digest's excluded counts. Hard eligibility, user blocks, cooldown, and duplicate identity are never relaxed. Increase legitimate discovery/backfill coverage or use owner supplement after new eligible items arrive; do not insert ineligible fillers.
- Model degradation: an absent model is normal deterministic mode. A configured assessor/reranker failure records degradation while rules continue. Remove or repair model configuration, then allow future assessments/digests to use it; never rewrite already-published snapshots.
- Missing, draft, or failed digest: startup and the scheduler recover the user/local-date job. Owner safe retry may be used. Published or supplemented days are returned unchanged. A later inventory increase may be handled only by append-only supplement.
- Low-hit starvation: `scheduleFloorViolations` must remain zero. Low historical eligible rate is computed only for reporting; it cannot extend the active/observing/dormant maximum interval. Inspect the saved schedule decision when a violation appears.
- Integrity alarms: `hardFilterViolations`, `duplicateClusterViolations`, and `activeBlockViolations` target zero and use publication-time snapshot evidence. Stop rollout and inspect the affected item/day before changing data; do not delete the digest.

The long-tail gem contribution rate counts distinct positively received recommendation items (`valuable`, `deep_read`, or archive) from a reporting-only low-hit site, divided by all distinct positively received items. A low-hit site currently means at least five discovered candidates and at most ten percent eligible. This grouping is not persisted as a source score.

## Retention and privacy

The system currently performs no automatic destructive retention of discovery bodies, immutable content versions, provenance, assessments, digests, feedback history, legacy-review rows, or operational evidence. This supports audit, retry, and rollback. Operators must treat article bodies and user feedback as retained application data and include them in access control, backups, export, and erasure procedures.

Structured events must never contain article bodies, Cookies, authorization headers, access tokens, passwords, model keys, or arbitrary upstream error payloads. Error summaries stored for fetch/processing are bounded operational text; inspect them under owner access. Preference reset starts a new personalization boundary but deliberately keeps historical feedback, digests, and explicit safety blocks. Any future physical deletion requires a separate reviewed migration and referential-integrity reconciliation.

## Release verification

Run the deterministic acceptance suite without public Internet or model credentials:

```sh
cd api
go test ./discovery ./recommendation ./jobqueue ./bootstrap ./api ./observability -run 'Test(DeterministicSiteWorld|BlogrollGraphFixture|BackfillFinds|ProcessCandidate|LowHitSource|EndToEndM17|DailyDigestM14|ObservabilityM16|RecoverDueJobs|V3Goose|V3SQLiteMigration)' -count=1
go test -race ./discovery ./recommendation ./jobqueue/... -count=1
go test ./... -count=1
cd ../web
npm test
npm run build
```

Also build the embedded frontend and backend binary. If the repository `make all` has local modifications or global package-manager side effects, run the equivalent repository-scoped `make web2api` and `make api` steps and record the deviation. PostgreSQL/River/pgvector evidence is required before production rollout; SQLite evidence alone is not a substitute.
