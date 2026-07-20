# Add safe diagnostics to structured job failure events

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds. Maintain this document in accordance with `PLANS.md` at the repository root.

## Purpose / Big Picture

Operators currently see only `error_type:"handler"` in failed `dataark_event` records, so they must query River and discovery tables to learn whether a site returned 404, 500, or another failure. After this change, failed job events expose a safe hostname and HTTP status when a response exists, plus a bounded and redacted error message for other failures. Logs must remain safe: they cannot include URL paths or queries, credentials, response bodies, article bodies, or arbitrary unbounded upstream payloads.

## Progress

- [x] (2026-07-20 17:00+08:00) Inspected the current event schema, worker logging, discovery fetch errors, queue error redaction, and operational privacy rules.
- [x] (2026-07-20 17:05+08:00) Added safe structured failure fields, final-boundary sanitation, and a diagnostic-error contract.
- [x] (2026-07-20 17:08+08:00) Propagated hostname, HTTP status, and category through discovery validation, robots, transport, response validation, and caller-created status failures.
- [x] (2026-07-20 17:10+08:00) Added observability, discovery, and jobqueue tests and updated operational documentation; the focused packages pass.
- [x] (2026-07-20 17:19+08:00) Passed the full Go and race suites, rebuilt Docker Compose, and observed five structured HTTP/response failures plus one non-HTTP failure from real queued work.
- [x] (2026-07-20 17:21+08:00) Final diff checks passed and only the focused files were staged for one `Change:` commit.

## Surprises & Discoveries

- Observation: `dataark_event` deliberately has no arbitrary details field, while River retains the full handler error and the queue API separately redacts URLs and common secret assignments.
  Evidence: `api/observability/event.go`, `api/jobqueue/control.go`, and `docs/operations/recommendation-v3-runbook.md`.
- Observation: non-success HTTP responses are returned as `FetchResult` values and converted to sentinel-wrapped errors by several discovery callers, so the final response URL is available at the point where the status error is created.
  Evidence: status checks in `api/discovery/store.go`, `blogroll_scan.go`, `backfill.go`, and `candidate_processing.go`.
- Observation: an HTTP response can have status 200 while the discovery operation fails validation, as with a Feed URL that returns HTML.
  Evidence: Docker job 44434 emitted `error_type:"content_type"`, `domain:"blog.samaltman.com"`, and `http_status:200` without exposing its URL.

## Decision Log

- Decision: Extend only failed structured job events; do not change the queue API, frontend, ordinary framework logs, database schema, or retry behavior.
  Rationale: The user selected `dataark_event` as the required scope.
  Date/Author: 2026-07-20 / Codex
- Decision: Add fixed `domain`, `http_status`, and `error_message` fields, and sanitize again at the final logging boundary.
  Rationale: Structured fields are searchable, while final-boundary sanitation prevents a future caller from accidentally emitting URL queries or credentials.
  Date/Author: 2026-07-20 / Codex
- Decision: Use the final response hostname after redirects, falling back to the requested hostname; never log scheme, port, path, query, or user information.
  Rationale: The host that produced the response is the most useful operational identity and does not expose request-specific data.
  Date/Author: 2026-07-20 / Codex

## Outcomes & Retrospective

Failed worker events now carry immediately useful diagnostics without requiring a River/database lookup. The real Compose run emitted `www.gislxz.com/500`, `h4ckm310n.com/502`, `deathsprout.github.io/404`, and a safe content-type failure, while the unrelated backfill data error emitted `error_message:"record not found"`. No structured event contained a scheme, URL path, query, credential, or response body. `go test ./...` and `go test -race ./discovery ./jobqueue/... -count=1` passed, the final image rebuilt successfully, and all four Compose services are running. No frontend, database schema, API response, or retry behavior changed.

## Context and Orientation

`api/observability/event.go` defines and serializes the fixed JSON event. River workers in `api/jobqueue/jobqueue.go` call `logWorkerEvent` after a handler returns. Discovery networking starts in `api/discovery/boundaries.go`; callers turn non-success `FetchResult` values into `ErrHTTPFetchStatus` errors. Discovery already imports observability and jobqueue both depend only on observability, so a small error metadata interface in observability can be implemented by discovery without creating an import cycle.

## Plan of Work

Extend `observability.Event` with optional diagnostic fields and add a helper that enriches failed events from an error. The helper will recognize typed metadata through `errors.As`, classify ordinary timeout and network errors, and always build a whitespace-compacted, URL-redacted, secret-redacted message of at most 300 runes. `Log` will normalize those fields again immediately before JSON marshaling.

Add a discovery diagnostic wrapper that preserves the cause through `Unwrap` and supplies hostname, status, and category. Wrap failures at the HTTP fetch boundary, including validation, robots, transport, response read, body-size, and content-type failures. Replace all non-success status constructors with one helper that uses `FetchResult.FinalURL` and falls back to the requested URL. Keep existing error text and `errors.Is` behavior so persistence, backoff, River state, and queue display remain compatible.

Change worker logging to pass failed errors through the observability enrichment helper. Update the runbook to describe the fixed safe fields and their privacy boundary. Add tests at the observability, discovery, and jobqueue layers.

## Concrete Steps

From the repository root, edit the Go packages and documentation with `apply_patch`, format changed Go files with `gofmt`, and run:

    cd api
    go test ./observability ./discovery ./jobqueue -count=1
    go test ./...

Then run `git diff --check`. From `docker/`, run `docker compose -p dataark up -d --build`, execute naturally pending crawl work if available, and inspect `dataarkapi` logs for safe structured fields. Do not inject a synthetic failure into the persistent Compose database merely to create a log line.

## Validation and Acceptance

A 502 response must emit `error_type:"http_status"`, the actual response hostname in `domain`, and `http_status:502`. A redirect followed by failure must name the final host. A timeout or DNS/connection failure must emit the hostname and a useful safe message but omit `http_status`. A non-HTTP error such as `record not found` must emit the bounded message. Tests must prove URL credentials and paths, query tokens, authorization values, Cookies, API keys, passwords, secrets, and Bearer tokens do not survive serialization. Existing discovery category checks and `errors.Is` behavior must continue to pass.

## Idempotence and Recovery

The change has no migration and can be rebuilt or restarted repeatedly. If Docker has no naturally failing task, automated `httptest` coverage is the deterministic acceptance evidence. Preserve the user's existing modifications to `docker/docker-compose.yml`, `go.work.sum`, `makefile`, `.codex`, and `passwd.txt`; do not stage or commit them.

## Artifacts and Notes

The target HTTP event shape is:

    dataark_event {"event":"job_fetch_source","job_id":"44326","source_id":128,"status":"failed","error_type":"http_status","domain":"h4ckm310n.com","http_status":502,"error_message":"http fetch response has an unsuccessful status: 502"}

No field may contain the source URL path or query.

## Interfaces and Dependencies

`observability.Event` gains `Domain string`, `HTTPStatus int`, and `ErrorMessage string` with `omitempty` JSON tags. `observability.FailureDetails` carries `ErrorType`, `Domain`, and `HTTPStatus`; an error may implement `ObservabilityFailure() FailureDetails`. `observability.WithError(Event, error) Event` is the single worker-facing enrichment function. No new third-party dependency is required.
