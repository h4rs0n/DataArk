# Add an owner-managed discovery domain blacklist

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds. This document follows `PLANS.md` in the repository root.

## Purpose / Big Picture

After this change, the owner can add and remove blocked crawl domains from the task-queue tab. DataArk will retain discovery metadata but will not perform DNS lookup or HTTP access for a blocked host or any of its subdomains. Existing pending candidates are paused reversibly, and the migration installs `csdn.net` as the first rule to stop the current stream of HTTP 521 failures.

## Progress

- [x] (2026-07-20 17:20+08:00) Inspected the discovery fetch boundary, queue recovery, candidate processing, owner API conventions, migrations, and dedicated queue tab.
- [x] (2026-07-20 18:05+08:00) Added the persistent rule model, host-key migration, matching service, reversible candidate reconciliation, and default `csdn.net` rule.
- [x] (2026-07-20 18:18+08:00) Enforced rules before DNS, redirects, enqueue recovery, source/site/backfill execution, and article body processing; pre-existing blocked jobs become no-ops.
- [x] (2026-07-20 18:32+08:00) Added owner-only CRUD APIs and the task-tab management panel with affected-candidate feedback.
- [x] (2026-07-20 15:56+08:00) Completed focused and full Go tests, frontend tests and production build, Docker Compose rebuild/migration checks, database assertions, and Chrome DevTools DOM/network/console validation.
- [x] (2026-07-20 15:58+08:00) Created the focused implementation commit without staging unrelated worktree files.

## Surprises & Discoveries

- Observation: existing site `blocked` status does not protect candidate article fetches or redirect targets.
  Evidence: `api/discovery/site_admin.go` checks site-backed source and backfill operations, while `api/discovery/candidate_processing.go` fetches the candidate URL directly.
- Observation: River crawl jobs have one attempt, so allowing a blocked queued job to return an error would recreate noisy discarded jobs.
  Evidence: `crawlUniqueByArgsOpts` in `api/jobqueue/jobqueue.go` sets `MaxAttempts = 1`.
- Observation: the bootstrap migration contract test intentionally pins the latest Goose version.
  Evidence: adding migration 21 required advancing `TestV3GooseMigrationIsAdditiveAndParseable` from 20 to 21 and asserting the new DDL markers.
- Observation: the live database contained 898 pending CSDN candidates eligible for policy blocking, while 28 historical ready or ineligible rows did not need mutation.
  Evidence: after Compose startup the state counts were `domain_blocked=898`, `ineligible=14`, and `ready=14`; the due-candidate query returned zero matching CSDN rows.
- Observation: the task queue already contained older River jobs for candidates that are now blocked.
  Evidence: recovery no longer emits these jobs, and handler-level preflight lets already-persisted jobs complete as successful no-ops without opening a network connection.

## Decision Log

- Decision: store exact normalized host suffix rules; a rule matches itself and label-boundary subdomains.
  Rationale: `blog.example.com` can be blocked without blocking all of `example.com`, while `example.com` naturally covers every child host.
  Date/Author: 2026-07-20 / Codex
- Decision: blocked pending candidates use `domain_blocked`, retain their metadata, and return to `fetch_pending` only after no remaining rule matches them.
  Rationale: blacklist removal must be reversible without deleting discovery evidence.
  Date/Author: 2026-07-20 / Codex
- Decision: pre-existing queued blocked work completes as a no-op, while new recovery passes omit it.
  Rationale: this prevents network access and avoids recording policy skips as crawl failures.
  Date/Author: 2026-07-20 / Codex
- Decision: already-extracted candidates may continue local dedupe and assessment.
  Rationale: the blacklist controls network access, not use of content already stored locally.
  Date/Author: 2026-07-20 / Codex

## Outcomes & Retrospective

The owner now has a dedicated domain-blacklist panel in the existing task-queue tab. The rule model, owner-only API, queue recovery filters, handler preflight, and HTTP redirect boundary all use the same label-aware matching behavior. The default `csdn.net` rule migrated the live pending candidates to a reversible policy state and reduced due CSDN work to zero without rewriting already-ready or ineligible history.

Automated verification passed with `go test ./discovery ./api ./jobqueue`, `go test ./...`, `npm test`, and `npm run build`. A clean Compose rebuild applied Goose migration 21. Chrome DevTools verified the rendered default rule, create/delete feedback for a temporary rule, successful API status codes, the absence of runtime console errors or warnings, and rejection of a CSDN source before persistence. The temporary rule and source were removed; the live blacklist ends with only `csdn.net`.

## Context and Orientation

`api/discovery/boundaries.go` owns bounded HTTP access, including robots checks and redirects. `api/discovery/recovery.go` stages due discovery work. `api/api/controller.go` exposes authenticated routes and wires River handlers. `web/src/views/RecommendationsView.vue` contains the owner-only task queue tab. PostgreSQL schema changes are embedded Goose files under `api/migrations/`.

## Plan of Work

Add a blacklist entry model and indexed crawl-host columns for sources and candidates. Implement strict host normalization, label-boundary matching, CRUD, and candidate reconciliation. Install a dynamic blocker in the configured HTTP fetcher before URL validation and on redirects. Filter queue recovery and add handler-level preflight so jobs queued before a rule was added become successful no-ops. Mark newly discovered matching candidates as blocked rather than enqueueing them.

Expose owner-only list, create, and delete routes under `/api/admin/discovery/domain-blacklist`. Add the form and rule list below the crawl queue in its dedicated tab, with a reason field and affected-candidate messages. Load rules on tab entry and explicit mutations, not on the rapid queue poll.

## Concrete Steps

Run focused and full backend tests from `api` with `go test ./discovery ./api ./jobqueue` and `go test ./...`. Run frontend contract tests and the production build from `web` with `npm test` and `npm run build`. Rebuild the Compose stack from `docker` with `docker compose -p dataark up -d --build`, then inspect the migrated database, API, logs, and browser behavior.

## Validation and Acceptance

The default rule list contains `csdn.net`. A blocked initial URL never calls the validator or target server, and an allowed URL redirecting to a blocked host never reaches that host. Due blocked sources, sites, backfills, and body-fetch candidates are absent from recovery; already queued equivalents perform no HTTP work and do not fail. Removing the final matching rule makes policy-blocked candidates due again.

The owner can list, add, and delete rules in the task queue tab. Invalid and duplicate domains show useful errors. A member cannot access the endpoints or see the panel. Docker startup applies the migration, and Chrome DevTools shows correct DOM and network state with no console errors.

## Idempotence and Recovery

The migration uses conditional DDL and conflict-safe default insertion. Rule creation is unique by normalized domain. Reconciliation only changes candidates carrying the blacklist processing marker, so repeated calls and overlapping rules are safe. Deleting a rule does not delete candidates, sources, sites, backfills, or history.

## Artifacts and Notes

The worktree already contains unrelated changes in `docker/docker-compose.yml`, `go.work.sum`, and `makefile`, plus untracked `.codex` and `passwd.txt`; they must not be staged or edited.

## Interfaces and Dependencies

The API entry has `id`, `domain`, `reason`, `createdAt`, and `updatedAt`. Create accepts `domain` and optional `reason`; create and delete return the rule plus `affectedCandidates`. Host normalization uses the already-pinned `golang.org/x/net/idna` package through the existing `x/net` module. No new external service or configuration flag is introduced.
