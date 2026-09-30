# Persist DataArk API logs with daily retention

> 归档说明（2026-09-30 核对）：本文保留实施当时的背景、决策、路径与验证记录，部分内容已被后续变更取代。现行行为与操作入口见 [文档索引](../../README.md)。历史测试输出不代表当前部署状态。

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds. Maintain this document in accordance with `PLANS.md` at the repository root.

## Purpose / Big Picture

DataArk currently sends application and HTTP access logs only to the process console, so container recreation can make older diagnostics inconvenient to retrieve. After this change, the API writes the same operational logs to a host-mounted daily file while preserving `docker compose logs`. Operators can configure how many local calendar days are retained; the default is seven. No log-reading API or frontend is added.

## Progress

- [x] (2026-08-04 12:13+08:00) Inspected standard-library, Gin, GORM, structured-event, startup, Docker, release, documentation, and test logging paths.
- [x] (2026-08-04 12:22+08:00) Implemented and focused-tested the daily file writer and global logging setup.
- [x] (2026-08-04 12:22+08:00) Wired logging configuration through startup, GORM, Gin, and operational messages.
- [x] (2026-08-04 12:23+08:00) Updated Docker, release, and bilingual deployment documentation.
- [x] (2026-08-04 12:27+08:00) Passed focused, full, race, development/release Compose, image build, live HTTP, dual-output, and restart-append validation.
- [x] (2026-08-04 12:28+08:00) Reviewed and committed only feature-related changes with subject `Add: persist API logs with daily retention`.

## Surprises & Discoveries

- Observation: the service uses three independent logging outputs: the Go standard logger, Gin's global default writers, and GORM's own logger created with `os.Stdout`.
  Evidence: `api/observability/event.go`, Gin v1.12.0 `mode.go`, and GORM v1.31.2 `logger/logger.go`.
- Observation: the first administrator password is intentionally printed with `fmt.Printf` rather than the standard logger.
  Evidence: `api/auth/model.go`; keeping that output on the console provides a clean way to avoid persisting the credential.
- Observation: the bind-mounted file is `root:root` inside the container and appears as `nobody:nogroup` under this host's Docker user mapping, so its deliberate `0640` mode requires Docker or elevated privileges for host-side inspection.
  Evidence: `ls -l` showed `-rw-r-----` in both views; `docker compose exec dataarkapi tail /logs/dataarkapi-2026-08-04.log` read it successfully.

## Decision Log

- Decision: Use one file per server-local calendar date and retain the current date plus the preceding `N-1` dates.
  Rationale: The user selected integer-day retention and natural-day rotation; for seven days on August 4 this retains July 29 through August 4.
  Date/Author: 2026-08-04 / Codex
- Decision: Mirror logs to both the file and console, but leave the startup banner and generated administrator password console-only.
  Rationale: Existing container-log operations continue to work without expanding credential persistence.
  Date/Author: 2026-08-04 / Codex
- Decision: Implement rotation with the Go standard library rather than adding a third-party dependency.
  Rationale: Date naming and exact calendar-day cleanup are small, deterministic requirements and can be tested with an injected clock.
  Date/Author: 2026-08-04 / Codex
- Decision: Fail startup when the log directory or current file cannot be prepared, and reject retention values below one.
  Rationale: Silent console-only fallback would violate the requested persistence guarantee.
  Date/Author: 2026-08-04 / Codex

## Outcomes & Retrospective

The API now mirrors standard application logs, structured events, GORM diagnostics, Gin access logs, and Gin recovery output to the console and a daily retained file. Focused and full Go tests passed, as did the race detector for logging and its integration packages. Both development and generated release Compose definitions validate. The production image rebuilt and served `/favicon.ico` with HTTP 200; the same Gin line appeared in Docker logs and the mounted file. The file grew from 310 bytes to 530 bytes across a same-day restart and to 620 bytes after the next request, proving append rather than truncation. The persistent database and all Compose services remain intact. The focused result is committed without the user's unrelated worktree changes.

## Context and Orientation

`api/main.go` parses process flags and starts the web service. `api/flag/flag.go` copies command-line values into `api/config/config.go`. `api/api/controller.go` constructs `gin.Default()`, whose logger and recovery middleware use Gin global writers. Most domain packages use Go's package-level `log`, structured task events pass through that logger, and `api/database/database.go` currently lets GORM create a separate stdout logger. `docker/docker-compose.yml` is the development/production Compose definition, while `.github/workflows/build-release.yml` generates another Compose file for binary releases.

A daily writer is an `io.Writer` that appends bytes to the file named for the current local date. Rotation means closing yesterday's file and opening today's file when the date changes. Retention cleanup parses only DataArk's strict date filenames and removes dates older than the configured boundary.

## Plan of Work

Create `api/logging` with a mutex-protected daily writer. On construction it validates configuration, creates the directory, opens the current file in append mode, cleans expired matching files, and starts a local-midnight rotation loop. Writes also check the date so clock changes and delayed timers remain safe. Closing stops the loop and closes the file. A runtime wrapper mirrors output to stdout, installs the writer into Go and Gin globals, and restores the prior writers when closed.

Add `-log-dir` and `-log-retention-days`, defaulting to `./logs` and `7`. Initialize logging after flags and before database or router startup. Configure GORM with a non-colored logger built on the already-installed standard-log writer. Convert operational `fmt` messages in web startup to standard log messages while leaving the banner and generated administrator credential on stdout.

Mount `docker/logs` at `/logs`, pass the two flags to the API container, and default Compose retention to seven even when an older `.env` lacks the new value. Apply the same volume and flags to the release Compose template. Document paths, retention semantics, permissions, and configuration in both READMEs and `.env.example`; exclude runtime logs from Docker build input.

## Concrete Steps

From the repository root, edit files with `apply_patch` and format Go files with `gofmt`. Run:

    cd api
    go test ./logging ./flag ./auth ./database ./api
    go test -race ./logging ./api
    go test ./...

Then run `git diff --check`, `docker compose config --quiet` from `docker/`, and rebuild the API service with `docker compose -p dataark up -d --build dataarkapi`. Request a public route and inspect both `docker compose logs dataarkapi` and `docker/logs/dataarkapi-YYYY-MM-DD.log`.

## Validation and Acceptance

A write on one date must create or append to that date's file; the first write after a date change must use a new file. With retention seven, the current date and six predecessors must remain and an eighth-day file must be removed. Future-dated and unrelated files must remain untouched. Concurrent writes must pass the race detector without corruption.

Standard logs, structured `dataark_event` records, Gin access/recovery output, and GORM warnings/errors must use the mirrored output. The generated administrator password must remain console-only. Invalid retention or an unusable log destination must prevent `startWeb` from running. Docker restart must append rather than truncate, and existing `docker compose logs` behavior must remain available.

## Idempotence and Recovery

Directory creation, file opening, cleanup, and Compose rebuilds are repeatable. Files use append mode. Cleanup never uses a wildcard and removes only strict `dataarkapi-YYYY-MM-DD.log` names older than the cutoff. If initialization fails, correcting the directory ownership or selecting another `-log-dir` and restarting is sufficient; no database rollback is involved.

## Artifacts and Notes

The default Docker artifact is:

    docker/logs/dataarkapi-2026-08-04.log

The file preserves existing human-readable standard, Gin, GORM, and structured-event formats rather than converting all entries to a new JSON schema.

Live validation evidence:

    HTTP 200
    [GIN] 2026/08/04 - 12:26:08 | 200 | 92.162µs | 172.18.0.1 | GET "/favicon.ico"
    310 -> 530 -> 620 bytes across restart and a second request

## Interfaces and Dependencies

The executable gains `-log-dir string` and `-log-retention-days int`. Compose gains `LOG_RETENTION_DAYS`, defaulting to `7`, and a fixed `./logs:/logs` mount. The logging package exposes a configuration function returning an `io.Closer`; no HTTP, database, frontend, or third-party dependency interface changes.

Revision note (2026-08-04 12:28+08:00 / Codex): completed implementation and recorded automated, Compose, live dual-output, permissions, restart-append, and focused-commit evidence so the plan can independently describe the delivered behavior.
