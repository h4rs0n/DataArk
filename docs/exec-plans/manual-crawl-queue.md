# Add a manually executed discovery crawl queue

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds. This document follows `PLANS.md` in the repository root.

## Purpose / Big Picture

After this change, content-discovery crawl work is visible to the owner before it runs. Startup recovery, the periodic discovery scheduler, and newly added subscriptions may stage work, but they cannot perform network crawling until the owner opens the recommendation center and clicks the queue run button. One click drains currently runnable discovery work and any immediate child work, then returns the queue to manual waiting mode. Daily recommendation generation and URL-to-HTML archive jobs remain independent.

## Progress

- [x] (2026-07-19 00:00+08:00) Inspected the existing River and in-memory job runtimes, startup recovery, scheduler, API permissions, and recommendation UI.
- [x] (2026-07-19 00:35+08:00) Added the paused `discovery_crawl` River queue, asynchronous manual memory queue, safe normalized snapshots, single-run drain behavior, and dedicated daily-generation path.
- [x] (2026-07-19 00:42+08:00) Added owner-only queue status and run HTTP APIs with idempotent due-work reconciliation.
- [x] (2026-07-19 00:51+08:00) Added the queue panel, adaptive polling, safe task details, and manual execution interaction to the content-discovery tab; frontend tests and production build pass.
- [x] (2026-07-19 23:52+08:00) Updated tests and operator documentation; passed the full Go suite, focused race suite, frontend tests/type-check/build, `go vet`, embedded-assets test, and production Go build. Browser fixture verification confirmed the owner panel, two task rows, enabled run button, expected API traffic, and no visible load error.
- [x] (2026-07-19 23:56+08:00) Committed the completed focused change as `Change: make discovery crawl queue manual`; the plan's final status update is included by amend so the branch retains one implementation commit.
- [x] (2026-07-20 00:14+08:00) Built and started the full Docker Compose stack, verified PostgreSQL marked `discovery_crawl` paused after startup, and exercised the production frontend through Chrome DevTools. The owner page showed two waiting tasks with an enabled execute button; the click returned HTTP 202, drained all reconciled work to 168 successes and 3 failures, then returned to idle with the River queue paused. Final DOM, network, console, runtime-exception, and screenshot checks passed.
- [x] (2026-07-20 02:07+08:00) Fixed upgrade compatibility by reconciling unfinished discovery jobs from River's historical `default` queue before workers start and resetting interrupted `running` rows to waiting. Against the persistent Docker database, startup migrated 1,269 legacy jobs, a second restart normalized 4 interrupted jobs, and no discovery handler events ran automatically. Chrome held at 1,275 waiting and 0 running across multiple polls with no POST or browser errors.
- [x] (2026-07-20 02:28+08:00) Moved the owner-only crawl queue into its own recommendation-center tab and limited status polling to the time that tab is active. Frontend contract tests, production build, the full Go suite, Docker rebuild, PostgreSQL queue checks, and Chrome DevTools verification passed, and the result was included in the existing focused feature commit by amend.

## Surprises & Discoveries

- Observation: discovery work and daily digest generation currently share River's default queue, so pausing the existing queue would also stop unrelated recommendation generation.
  Evidence: `api/jobqueue/jobqueue.go` registers all five workers against `river.QueueDefault`.
- Observation: the SQLite fallback executes enqueue calls synchronously, so it needs a real paused staging mode rather than a presentation-only status wrapper.
  Evidence: `api/jobqueue/memory.go` calls each handler from `MemoryQueue.run` before returning.
- Observation: URL archive tasks use a separate channel and database model and are not part of this feature.
  Evidence: `api/search/add.go` owns `archiveTaskQueue` independently of `api/jobqueue`.
- Observation: an upgraded persistent database can contain thousands of unfinished discovery jobs created before the dedicated queue existed; changing new-job routing does not move these rows, and River immediately consumes them from `default` at startup.
  Evidence: the initial Docker sample contained 1,256 active candidate-processing jobs plus fetch and backfill jobs in `default`; after stopping the consumer, the upgraded startup reconciled 1,269 remaining rows while `discovery_crawl` stayed paused.
- Observation: a browser page that loaded the pre-change JavaScript continues its old background polling after the Docker image is rebuilt until that page is refreshed or closed.
  Evidence: Docker received queue GET requests before the new Chrome session loaded. The new session issued no queue GET on initial load, stopped at request 53 for two consecutive 12-second windows after leaving the queue tab, and issued request 54 immediately on returning.

## Decision Log

- Decision: isolate the four discovery job kinds in a queue named `discovery_crawl`; keep daily generation on River's default queue.
  Rationale: this creates the smallest control boundary that matches the requested behavior without delaying daily recommendations.
  Date/Author: 2026-07-19 / Codex
- Decision: automatic mechanisms continue to stage due jobs while the discovery queue is paused.
  Rationale: the owner can see the real pending backlog, and existing scheduling and idempotency policies remain useful without causing network crawling.
  Date/Author: 2026-07-19 / Codex
- Decision: the owner-only content-discovery tab will show active jobs plus up to 50 recent final jobs; success and failure totals use a 24-hour window.
  Rationale: this meets current-queue observability without introducing a second long-term job-history schema.
  Date/Author: 2026-07-19 / Codex
- Decision: the in-memory fallback drains at most four tasks concurrently and waits through a 500 ms quiescence window so immediate child jobs join the same manual run.
  Rationale: this matches River's worker ceiling and prevents the queue from declaring completion between a parent handler and its staged child work.
  Date/Author: 2026-07-19 / Codex
- Decision: discovery River jobs use one River attempt; the discovery domain models remain the source of truth for retry and backoff timing.
  Rationale: River's independent automatic retry schedule could otherwise make a failed crawl runnable before the source, backfill, or candidate retry timestamp and undermine manual control.
  Date/Author: 2026-07-19 / Codex
- Decision: before River workers start, reconcile all active discovery rows from `default` into `discovery_crawl` and reset any interrupted manual-queue `running` rows to `available`.
  Rationale: new `InsertOpts` only affect newly inserted jobs. Persistent upgrade backlogs and jobs interrupted by a container stop must also remain owner-gated and must not appear permanently running.
  Date/Author: 2026-07-20 / Codex
- Decision: expose the queue as an owner-only `queue` tab immediately after content discovery and poll only while that tab is active.
  Rationale: queue operations deserve a distinct workspace, and stopping the ten-second background poll when the owner is viewing other tabs removes avoidable API access logs without reducing queue visibility.
  Date/Author: 2026-07-20 / Codex

## Outcomes & Retrospective

The owner now has a visible, manually executed crawl queue while recommendation generation and URL archives remain independent. The in-memory and River paths share the same API contract, startup no longer executes discovery work, and retry/backoff work remains visible for a later click. Static, unit, race, build, and localhost browser-fixture validation passed. A full Docker Compose integration run proved the production PostgreSQL/River path and the manual execute lifecycle. Upgrade validation then exposed and closed a persistent-data gap: unfinished jobs inserted by older releases remained on `default` and ran automatically. Startup now reconciles those legacy rows and interrupted manual work before starting River. The persistent Docker database ended with no active discovery jobs on `default`, 1,275 waiting and zero running on the paused manual queue, and zero discovery handler events after restart. The queue UI now lives in an independent owner-only tab; a fresh Docker browser session makes no queue request on initial load, polls only while that tab is selected, and stops polling after tab exit. Chrome DevTools found no console errors or warnings and no queue-run POST.

## Context and Orientation

`api/jobqueue/jobqueue.go` creates the production PostgreSQL River client and defines the shared enqueue interface. `api/jobqueue/memory.go` supplies the SQLite/test fallback. `api/discovery/recovery.go` finds due sources, sites, backfills, and candidates and stages their corresponding jobs. `api/api/controller.go` starts the queue and schedulers and exposes authenticated Gin routes. `web/src/views/RecommendationsView.vue` contains the existing owner-only subscription and content-discovery operations.

In this plan, “staging” means inserting a job in `pending` state without invoking its network or processing handler. “Runnable” means the job is pending and its scheduled time is not in the future. A “single run” begins when the owner clicks execute and ends after no runnable or running discovery job remains for a short quiescence window. Future retry jobs remain pending for a later manual run.

## Plan of Work

Extend `api/jobqueue` with public queue snapshot types and a controller installed alongside the existing default enqueuer. Route the four discovery argument types to `discovery_crawl`, ensure that River queue exists and is paused before River workers start, and monitor a manual resume until current work is drained. Refactor the memory fallback so discovery enqueue calls record pending jobs and a manual run processes them asynchronously, while daily generation retains automatic execution.

Add owner-only GET and POST handlers under `/api/admin/discovery/crawl-queue`. GET returns the normalized snapshot. POST first invokes discovery recovery to stage all currently due work, then starts an idempotent single run and returns HTTP 202. Do not expose raw job arguments or arbitrary metadata; decode only the numeric source, site, or candidate identity and a content version.

Add an owner-only queue tab immediately after content discovery in the recommendation center. It displays queue mode, state, pending/running and recent final counts, task rows, errors, and an execute button. Poll every two seconds while running and every ten seconds while waiting, but only while the queue tab is active. Stop the timer on tab exit and component teardown. Refresh sources and candidates when a run finishes.

Update README files and the v3 operations runbook so operators understand that the discovery interval stages work and that an owner must start execution from the UI.

## Concrete Steps

From the repository root, edit the backend runtime and tests, then run:

    cd api
    go test ./jobqueue ./discovery ./api -count=1
    go test -race ./jobqueue ./discovery ./api -count=1

Edit the Vue view and its contract test, then run:

    cd web
    npm test
    npm run build

For the independent queue tab extension, both commands passed, and the production Docker build repeated the frontend type-check and Vite build successfully.

Complete repository validation with:

    cd api && go test ./... -count=1
    make web2api
    GOCACHE=/tmp/dataark-go-cache make api

The implementation used an equivalent direct production build command instead of `make api` because the user already had unrelated changes in `makefile` and `go.work.sum`, and `make api` runs `go mod tidy`. The successful command was:

    cd api
    env CGO_ENABLED=0 GOCACHE=/tmp/dataark-go-cache go build -trimpath -ldflags '-s -w' -o ../bin/EchoArkServer main.go

## Validation and Acceptance

With pending discovery work, start the API and wait longer than a normal worker fetch interval. The queue API must report `waiting`, handlers must not run, and source/candidate timestamps must remain unchanged. As an owner, open `/#/recommendations`, select the task queue tab, and observe pending counts and safe task identities. Leaving the tab must stop queue GET polling, and returning must fetch a fresh snapshot and resume polling. Click execute; the API returns 202, task states move through running to succeeded or failed, immediate child work is processed, and the queue returns to waiting or idle. Restart during a run and confirm no work resumes until another click.

A member receives HTTP 403 from both queue endpoints and does not see the queue panel. Daily recommendation jobs continue running on the default queue. URL archive behavior remains unchanged.

## Idempotence and Recovery

Discovery argument uniqueness prevents duplicate active River jobs. Repeated run clicks return the same running state and do not create duplicate work. Startup always pauses the discovery queue before starting workers, so a crash cannot cause an automatic crawl on restart. A failed task retains its compact error and domain backoff schedule; when it becomes due, automatic staging makes it visible for a later manual run.

## Artifacts and Notes

The user worktree already contains unrelated modifications in `docker/docker-compose.yml`, `go.work.sum`, and `makefile`, plus untracked local files. They were not modified or staged. The temporary validation binary was moved to `/tmp/dataark-bin-validation-20260719`. Browser artifacts used isolated Chrome profiles and screenshots under `/tmp`; the original Docker UI screenshots are `/tmp/dataark-docker-crawl-queue-before.png`, `/tmp/dataark-docker-crawl-queue-after.png`, and `/tmp/dataark-docker-crawl-queue-final.png`. Upgrade compatibility validation produced `/tmp/dataark-legacy-fix-queue.png`. The independent-tab verification screenshot is `/tmp/dataark-dedicated-queue-tab.png`. Its PostgreSQL check showed only `discovery_crawl|available|1275`, and `river_queue` reported the queue as paused.

## Interfaces and Dependencies

The normalized snapshot contains `mode`, `state`, `canRun`, `updatedAt`, `counts`, and `tasks`. Counts contain `pending`, `running`, `succeeded24h`, and `failed24h`. Each task contains a safe ID, kind, target type and numeric ID, optional content version, normalized status, attempts, timestamps, and a compact error. The implementation uses the pinned River v0.39 APIs already present in the module and adds no external dependency.

Revision note (2026-07-20): extended the completed manual-queue plan for the requested independent owner tab and active-tab-only polling, then recorded automated, Docker, database, and Chrome evidence so the result remains restartable from this document.
