# Reduce repeated discovery crawl failures

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds. This document follows `PLANS.md` in the repository root.

## Purpose / Big Picture

After this change, an owner can run the manual discovery crawl queue without repeatedly spending most of the run on sources that have already failed for the same durable reason. Legitimate RSS 1.0 feeds served as RDF and bounded feeds larger than two MiB can be ingested, WordPress REST and oEmbed metadata are no longer mistaken for feeds, and transient versus structural failures receive different retry delays. The effect is observable in focused tests, in the queue's structured `dataark_event` records, and in persisted `next_due_at` values: a structural feed error is not eligible again after a few minutes, while timeouts still recover on a shorter schedule.

## Progress

- [x] (2026-08-11 03:27+08:00) Read the repository instructions and inspected the crawl queue, HTTP fetch boundary, endpoint discovery, source state, retry policy, migrations, tests, and current Compose services.
- [x] (2026-08-11 03:27+08:00) Aggregated the 2026-08-10 and 2026-08-11 API logs and cross-checked failures against PostgreSQL fetch runs, source state, and River jobs.
- [x] (2026-08-11 03:35+08:00) Implemented strict declared-feed media-type recognition, RDF support, bounded feed-content sniffing, and the eight MiB bounded feed limit.
- [x] (2026-08-11 03:35+08:00) Implemented safe `feed_parse` diagnostics, DNS/TLS classification, category-aware retry scheduling, legacy cooldown reconciliation, and an execution-time gate for stale River jobs.
- [x] (2026-08-11 03:35+08:00) Added Goose migration 25 to quarantine automatic WordPress metadata endpoints and reset only RDF/body-limit failures made retryable by this release.
- [x] (2026-08-11 03:42+08:00) Added focused regressions; focused, full backend, and race suites pass, `make api` succeeds with a writable Go cache, and the Compose configuration validates.
- [x] (2026-08-11 03:44+08:00) Rebuilt the API, applied migration 25, verified the corrected live queue/database state without running the manual crawl queue, completed the focused diff review, and included only the task files in the repository's single commit.

## Surprises & Discoveries

- Observation: recent work contained more failed source fetches than successful ones, and most failures were repeated executions of the same sources rather than new failures.
  Evidence: the two logs contain 570 failed and 488 completed `job_fetch_source` events. PostgreSQL reports only 156 distinct failed sources in that interval, with many sources executed five or six times.
- Observation: every recent failed source was automatically discovered rather than owner-managed.
  Evidence: joining `discovery_fetch_runs` to `discovery_sources` for failures since 2026-08-10 returned `user_managed = false` for all 570 runs.
- Observation: the owner started the queue six times, and the uniform five-minute exponential backoff made durable failures eligible for most later clicks.
  Evidence: queue POSTs occurred at 09:26, 11:28, 13:33, 14:33, 16:31, and 02:23. The policy in `api/discovery/schedule_policy.go` starts every error category at five minutes and caps every category at twenty-four hours.
- Observation: a missing MIME type is responsible for a large class of false failures.
  Evidence: 114 failures across 26 sources were `content_type`; all but one sampled source returned `application/rdf+xml`, the RSS 1.0 RDF media type, which is absent from `allowedContentType`.
- Observation: declared endpoint discovery accepts unrelated WordPress metadata as feeds.
  Evidence: `isFeedMediaType` accepts plain `application/json` and any media type containing `xml`. The database contains nine failed feed sources under `/wp-json/wp/v2/` or `/wp-json/oembed/`, responsible for 45 parse failures in the recent logs.
- Observation: the structured event currently exposes parser-provided response excerpts and disagrees with the persisted category.
  Evidence: PostgreSQL records these runs as `processing`, while worker events call them `handler` and include snippets such as WordPress `title.rendered` response content.
- Observation: nine otherwise plausible feed endpoints repeatedly exceed the two MiB feed cap.
  Evidence: 35 `body_too_large` runs came from nine XML or JSON feed-looking URLs. Article and Sitemap fetches already use an eight MiB bound.
- Observation: correcting recovery-time eligibility alone cannot suppress River jobs that were staged before a source was disabled or deferred.
  Evidence: the persistent queue outlives source state changes and workers previously called `FetchDiscoverySourceByID` directly, so already-available jobs never re-read `enabled` or `next_due_at` before issuing a network request.
- Observation: GORM applies the schema's `default:true` to a zero-valued `Enabled` bool during a struct insert.
  Evidence: the execution-gate regression initially issued two requests because its supposedly disabled fixture was inserted as enabled; explicitly updating the test row to false made the expected single due-source request observable.
- Observation: the repository's default Go build cache is read-only in the workspace sandbox.
  Evidence: the first `make api` attempt failed opening `/home/harson/.cache/go-build`; rerunning the unchanged target with `GOCACHE=/tmp/dataark-go-cache` succeeded. This was an execution-environment constraint, not a product build failure.

## Decision Log

- Decision: correct false-positive and false-negative feed recognition before changing retry behavior.
  Rationale: a longer retry delay alone would hide legitimate RDF feeds and preserve the creation of invalid WordPress sources; correctness at ingestion removes both recurring classes at their origin.
  Date/Author: 2026-08-11 / Codex
- Decision: keep response handling bounded while increasing the feed cap to eight MiB and allow generic server media types only when the bounded body has an unmistakable RSS, Atom, RDF, or JSON Feed signature.
  Rationale: several real feeds exceed two MiB, but accepting every `text/html` or binary response would turn a compatibility fix into an unbounded or overly permissive fetch path. The parser remains the final format validator.
  Date/Author: 2026-08-11 / Codex
- Decision: use error-category profiles rather than disabling all repeatedly failing sources.
  Rationale: DNS, TLS, HTTP, timeout, and structural feed failures have different recovery characteristics. Longer cooldowns preserve eventual recovery and owner control without retrying known structural failures on every queue click.
  Date/Author: 2026-08-11 / Codex
- Decision: quarantine only the already persisted, automatically discovered WordPress oEmbed and core REST v2 feed endpoints in a migration; do not delete source or run history.
  Rationale: these URLs are demonstrably WordPress REST/oEmbed metadata admitted by the old classifier. Restricting the predicates to `/wp-json/oembed/` and `/wp-json/wp/v2/` avoids catching a hypothetical custom JSON Feed endpoint. Setting `enabled = false` is reversible and the stricter classifier prevents them from being re-created, while historical evidence remains queryable.
  Date/Author: 2026-08-11 / Codex
- Decision: reset retry state only for RDF and body-limit failures that the new code can now handle.
  Rationale: without a targeted data correction, old high failure counts would delay verification of the compatibility fixes for days or weeks. Other failures retain their history and are reconciled into the new cooldown policy.
  Date/Author: 2026-08-11 / Codex
- Decision: re-check source existence, enabled state, corrected cooldown, and `next_due_at` in the fetch worker immediately before network I/O.
  Rationale: recovery-time selection cannot revoke jobs already staged in River. The execution gate makes queued work harmless after disablement or rescheduling while keeping direct owner refresh behavior unchanged.
  Date/Author: 2026-08-11 / Codex

## Outcomes & Retrospective

The change is implemented and running in the local Compose environment. Migration 25 quarantined all 18 existing automatically discovered WordPress oEmbed/core REST v2 metadata endpoints, with zero still enabled. It cleared the obsolete failure state for the 24 RDF sources and 10 feeds that exceeded the former two MiB bound; exactly 34 crawl jobs are staged for those corrected sources, and the `discovery_crawl` queue remains paused. Recovery reports zero enabled, operationally crawlable failed sources still due. No fetch run was created after the restart, so verification did not generate external crawl traffic, and all 4,304 historical `discovery_fetch_runs` remain queryable.

Focused tests, the complete Go suite, and race checks pass. The production binary build, Compose configuration, image rebuild, migration, startup, and diff checks also pass. A post-change failure-rate comparison is intentionally deferred until the owner next runs the manual queue; the implementation instead establishes the immediate safety invariants: known false endpoints are disabled, newly compatible feeds are staged once, old failed work is deferred by category, and stale River jobs re-check current source state before network I/O.

## Context and Orientation

DataArk stages discovery work in River, a PostgreSQL-backed job queue. `api/jobqueue/jobqueue.go` places crawl jobs on the paused `discovery_crawl` queue and runs them only after the owner presses the queue button. `api/api/controller.go` calls `discovery.RecoverDueJobs` before resuming that queue. Recovery in `api/discovery/recovery.go` selects sources whose persisted `next_due_at` is due and enqueues one stable source identifier.

`api/discovery/boundaries.go` is the bounded HTTP fetch boundary. A fetch kind selects accepted response types and a byte limit. `api/discovery/endpoint_discovery.go` reads `<link rel="alternate">` declarations from a homepage and creates feed sources. `api/discovery/store.go` parses fetched RSS, Atom, and JSON Feed documents into candidates. `api/discovery/fetch_state.go` persists every attempt and chooses the next retry through `api/discovery/schedule_policy.go`.

A structural failure means the request succeeded but the endpoint cannot serve a usable feed under the current contract, for example an unsupported media type, a non-feed payload, an unsafe URL, too many redirects, or a response beyond the bounded size. A transient failure means the endpoint might recover soon, for example a timeout or temporary transport error. A cooldown is the minimum time after `last_attempt_at` before recovery may enqueue the source again. River itself still performs one attempt per discovery job; the discovery source remains the retry source of truth.

## Plan of Work

First, change `api/discovery/endpoint_discovery.go` so declared feeds use an exact parsed media-type allowlist. Accept RSS, Atom, RDF/RSS 1.0, JSON Feed, and generic XML feed declarations. Reject plain JSON, oEmbed XML subtypes, RSD, and other XML metadata. Existing manually configured JSON Feed sources remain supported by the fetch and parser paths.

Second, update `api/discovery/boundaries.go`. Add `application/rdf+xml` to the feed `Accept` header and exact allowlist. Raise only the feed response cap from two MiB to eight MiB. For empty or generic `text/plain`, `text/html`, or `application/octet-stream` server declarations, accept the bounded body only when its prefix identifies RSS, Atom, RDF, or JSON Feed. Do not relax HTML, article, Sitemap, robots, redirect, SSRF, timeout, or blacklist controls.

Third, make parse failures explicit and safe. Add a stable feed-parse sentinel and wrap it with fetch diagnostics using the final hostname and HTTP status. Do not propagate parser excerpts from the upstream response. Extend transport classification to identify DNS and TLS failures where possible so structured events and retry policy use meaningful categories.

Fourth, extend `api/discovery/schedule_policy.go` with a category-aware source failure decision. Preserve stable jitter. Timeouts retain the shortest retry profile; generic network failures wait longer; DNS, TLS, and HTTP failures receive multi-hour starting delays; structural failures begin at roughly one day and cap at a multi-day interval. Keep the existing generic `NextFailure` wrapper for backfill scheduling. In `api/discovery/recovery.go`, compare old persisted failure rows against the new minimum cooldown based on `last_attempt_at`. If an old `next_due_at` is too early, update it idempotently and skip enqueueing until the corrected time.

Fifth, add `api/migrations/000025_discovery_failure_recovery.sql`. Disable only non-user-managed feed sources whose URLs contain `/wp-json/`, clear their next-fetch timestamps, and mark a bounded reason. Reset failure counters and make sources due now when their last failure was an RDF content-type rejection or the former two MiB body limit. Keep fetch-run history. The Down section restores quarantined endpoints to an enabled, due state but does not remove history or schema.

Finally, add focused tests beside the affected discovery code and migration parser tests under `api/bootstrap`. Validate format detection, safe parse errors, category delays, legacy cooldown reconciliation, and migration content. Then run focused, full, race, formatting, and Compose checks. Rebuild the API so migration 25 and recovery reconciliation run against the current persistent database, but do not automatically press the manual crawl button or trigger broad external work during validation.

## Concrete Steps

From `/home/harson/sideProj/DataArk/api`, format and run focused tests:

    gofmt -w discovery/boundaries.go discovery/endpoint_discovery.go discovery/fetch_diagnostics.go discovery/fetch_state.go discovery/recovery.go discovery/schedule_policy.go discovery/*_test.go bootstrap/*_test.go
    env GOCACHE=/tmp/dataark-go-cache go test ./discovery ./jobqueue ./bootstrap -count=1

Run the complete backend and race-sensitive crawl tests:

    env GOCACHE=/tmp/dataark-go-cache go test ./... -count=1
    env GOCACHE=/tmp/dataark-go-cache go test -race ./discovery ./jobqueue -count=1

From the repository root, validate the diff and production build:

    git diff --check
    make api
    docker compose -f docker/docker-compose.yml config --quiet
    docker compose -f docker/docker-compose.yml up -d --build dataarkapi

After the container is healthy, query the migration version and source aggregates read-only. Expect Goose version 25, zero enabled non-user-managed `/wp-json/` feed sources, RDF/body-limit sources made due with cleared failure counts, and old durable failures moved no earlier than their category cooldown. Inspect fresh startup logs for migration or queue errors. Do not run the queue solely to manufacture external failure events.

## Validation and Acceptance

The endpoint-discovery regression must present one valid RSS alternate plus WordPress `application/json`, `application/json+oembed`, and `text/xml+oembed` links and observe only the RSS URL in `links.feeds`. A fetch-boundary regression must accept `application/rdf+xml`, parse a representative RDF/RSS 1.0 item, and admit a feed slightly above the former two MiB limit while retaining the eight MiB ceiling. A generic media type must pass only with a recognizable feed body.

A feed parser failure must produce `feed_parse` diagnostics with hostname and status but no response excerpt. Scheduling tests must show increasing, stable category delays and a structural delay materially longer than timeout delay. Recovery must skip and persistently defer a legacy due source whose old timestamp violates the new policy, while still enqueueing an overdue source. Existing success must clear failure state as before.

All Go packages and focused race tests must pass. `make api`, Compose configuration, and `git diff --check` must exit zero. The rebuilt live database must report migration 25 and the targeted data correction without deleting sources or fetch history. No unrelated worktree file may be staged or committed.

## Idempotence and Recovery

Endpoint recognition, body sniffing, and scheduling are pure decisions and are safe to repeat. Recovery only moves `next_due_at`, `next_fetch_at`, and `backoff_until` later when an old value violates the new minimum; repeated scheduler ticks become no-ops. The migration uses predicates and fixed assignments so a repeated Goose application cannot create duplicates. Its Down action can re-enable the specifically marked WordPress endpoints, although rolling back the binary would restore the classifier that admitted them.

If the Compose rebuild fails, the existing containers and persistent data remain usable; rerun the build after correcting the code. Do not remove Docker volumes. The migration preserves all `discovery_fetch_runs`, so before/after analysis remains possible.

## Artifacts and Notes

Baseline evidence from the production-like local Compose database and logs:

    job_fetch_source completed: 488
    job_fetch_source failed:    570
    failed source categories:
      network 227 / http_status 129 / content_type 114
      processing 45 / body_too_large 35 / timeout 9
      too_many_redirects 6 / unsafe_url 5

    persisted distinct failed sources: 156
    enabled sources with failure_count >= 6: 154
    enabled sources with any failure: 419

The worktree already contains unrelated user changes in `go.work.sum`, `makefile`, and `web/public/favicon.ico`, plus untracked `.codex` and `passwd.txt`. They must remain untouched and unstaged.

## Interfaces and Dependencies

`api/discovery/schedule_policy.go` will retain:

    func (policy SourceSchedulePolicy) NextFailure(sourceID uint, failureCount int) time.Time

for existing generic callers, and add category-aware source scheduling methods that return `SourceScheduleDecision` and expose the deterministic delay needed by recovery. `api/discovery/fetch_diagnostics.go` will continue implementing `observability.FailureDetailer` and add an internal category accessor so persisted and logged classifications agree.

No new third-party dependency is required. MIME parsing uses Go's `mime` package, body signatures use bounded byte/string inspection, and RSS/RDF parsing continues through the existing `github.com/mmcdole/gofeed` parser.

Revision note (2026-08-11 03:27+08:00): created this plan after correlating recent structured logs, persisted fetch runs, source state, River state, and the six manual queue runs.

Revision note (2026-08-11 03:35+08:00): recorded the implemented compatibility, diagnostic, scheduling, migration, and stale-job execution-gate changes plus the passing focused regression suite.

Revision note (2026-08-11 03:43+08:00): recorded full/race/build/Compose validation and the measured migration result: 18 false endpoints quarantined, 34 corrected feeds staged on a paused queue, zero due failed sources, and zero restart-triggered fetch runs.

Revision note (2026-08-11 03:44+08:00): closed the plan after the focused task commit; unrelated pre-existing worktree changes remain unstaged.
