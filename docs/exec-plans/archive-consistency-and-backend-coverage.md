# Add Archive Consistency Repair and Backend Coverage

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds.

This repository includes `PLANS.md` at the repository root. This document is maintained in accordance with `PLANS.md`: it is self-contained, records decisions as they are made, and describes observable behavior, validation, and recovery.

## Purpose / Big Picture

DataArk stores each archived web page in three places: an HTML file under the archive directory, a searchable document in Meilisearch, and database rows that represent archive tasks and aggregate archive statistics. Today these three stores can drift apart. A user may see a search result whose HTML file is gone, a file may exist but be missing from search, or statistics may disagree with the files on disk. After this change, DataArk will expose a consistency check and repair path that treats HTML files as the highest-weight source, rebuilds Meilisearch from recoverable files, refreshes database statistics from disk, and reports any item that cannot be repaired on a dedicated page.

The backend unit-test work exists to make this behavior reliable. The target is 100% statement coverage for backend packages that can be unit tested without live PostgreSQL, live Meilisearch, or external SingleFile services. Where existing code is bound directly to external services, the implementation will introduce small interfaces or pure helper functions so the important behavior can be covered by deterministic unit tests.

## Progress

- [x] (2026-05-07T10:56:16Z) Read repository instructions, `PLANS.md`, and the relevant backend/frontend files before creating this plan.
- [x] (2026-05-07T10:56:16Z) Measured initial backend coverage with `cd api && go test ./... -coverprofile=/tmp/dataark-cover.out`; current package coverage is `DataArk` 0.0%, `DataArk/api` 0.0%, `DataArk/assets` 0.0%, `DataArk/backup` 25.2%, `DataArk/common` 8.0%, and `DataArk/search` 6.2%.
- [x] (2026-05-07T10:56:16Z) Created this ExecPlan before making code changes for the feature.
- [x] (2026-05-07T11:36:00Z) Implemented backend consistency checking and repair endpoints: `GET /api/archiveConsistency` and `POST /api/archiveConsistency/repair`.
- [x] (2026-05-07T11:50:00Z) Added the frontend consistency page at `web/src/views/ConsistencyView.vue`, registered `/consistency`, and added home navigation.
- [x] (2026-05-07T12:12:00Z) Added backend unit tests for the new consistency service, new API handlers, middleware, token helpers, HTML helpers, archive stats, GORM-backed database functions with SQLite, backup helper functions, and selected search helpers.
- [x] (2026-05-07T12:18:00Z) Ran backend tests and frontend build successfully.
- [x] (2026-05-07T12:20:00Z) Verified the new frontend page in the browser with chrome-devtools MCP, including screenshot, DOM snapshot, console check, and network request list.
- [x] (2026-05-07T12:19:00Z) Ran Docker Compose integration testing after network access was restored: built and started the full stack, authenticated against the running API, created a real archive HTML fixture, verified consistency drift, repaired it, verified search indexing, and rechecked the production frontend page.
- [ ] Backend total statement coverage is not yet 100%; latest measured total is 66.9%. Remaining uncovered code is concentrated in live-service paths for backup creation/restore, Meilisearch URL archive queue processing, and process/database initialization.
- [x] (2026-05-07T12:24:00Z) Recorded final outcomes, validation commands, and remaining coverage risk in this ExecPlan.

## Surprises & Discoveries

- Observation: The repository already has some uncommitted or untracked files, including `AGENTS.md`, `CONVENTIONS.md`, `PLANS.md`, and `docs/`.
  Evidence: `git status --short` showed existing modifications before implementation began. These files must not be reverted.
- Observation: The database currently has archive tasks and aggregate archive statistics, but no per-HTML-file canonical metadata table.
  Evidence: `api/common/db.go` defines `ArchiveTask` and `ArchiveStat`; no model records one row per archived HTML file.
- Observation: Deletion already removes Meilisearch documents, then removes the HTML file, then decrements database statistics.
  Evidence: `api/search/delete.go` implements `DeleteDocByHTMLPath` with that sequence.
- Observation: Rebuild logic already knows how to rebuild the Meilisearch `blogs` index from files under the archive directory.
  Evidence: `api/search/rebuild.go` walks `common.ARCHIVEFILELOACTION`, skips `Temporary`, parses `.html` and `.htm` files, and adds documents to Meilisearch.
- Observation: Vite's dev server returns the frontend HTML shell for `/api/archiveConsistency` when the Go backend is not running, so the consistency page must reject a successful HTTP response that does not contain JSON `Data`.
  Evidence: chrome-devtools network showed `GET http://127.0.0.1:5173/api/archiveConsistency [304]`; after adding response-shape validation, the page rendered the error `一致性响应缺少数据` instead of silently showing empty counts.
- Observation: Nested archive paths were inconsistently handled by the existing rebuild code because it indexed only `entry.Name()` rather than the path below the domain.
  Evidence: `api/search/rebuild.go` previously passed `entry.Name()` to `buildDocumentFromHTML`; the implementation now indexes `strings.Join(pathParts[1:], "/")` so `domain/nested/page.html` maps to filename `nested/page.html`.
- Observation: Docker integration caught a production frontend bug that unit tests and Vite dev-server testing did not catch.
  Evidence: chrome-devtools console on `http://127.0.0.1:7845/#/consistency` reported `TypeError: Cannot read properties of null (reading 'length')` because `actions` was `null` on a check-only response. The fix made the backend return `actions: []` and made the frontend tolerate nullable list fields.

## Decision Log

- Decision: Treat HTML files on disk as the highest-weight source for recoverable archive content.
  Rationale: The HTML file is the only durable copy of the archived page content. Meilisearch can be rebuilt from it, and database statistics can be refreshed from it. If the HTML file is missing, neither Meilisearch nor database metadata can reconstruct the original page content.
  Date/Author: 2026-05-07 / Codex
- Decision: Treat Meilisearch as a derived index, not a source of truth.
  Rationale: Search documents are generated from HTML files by `buildDocumentFromHTML` and `addDocFileByPath`. If a search document is missing but the file exists, it can be recreated; if a search document exists but the file is missing, the user needs a clear unrecoverable report because the archived content cannot be opened.
  Date/Author: 2026-05-07 / Codex
- Decision: Treat database archive statistics as derived metadata and refresh it from disk during repair.
  Rationale: `ArchiveStat` stores aggregate counts, and `RefreshArchiveStatsFromDisk` already rebuilds those counts from the archive directory. This is safer than trying to reconcile aggregate counters by hand.
  Date/Author: 2026-05-07 / Codex
- Decision: Add a dedicated consistency page rather than hiding unrecoverable problems in backup or statistics pages.
  Rationale: The user explicitly asked for a new unrecoverable consistency handling page. A separate page gives operators one place to run checks, repair recoverable drift, and see what remains manual.
  Date/Author: 2026-05-07 / Codex
- Decision: Use SQLite as a test-only GORM driver to cover database functions without requiring PostgreSQL.
  Rationale: The repository's database functions use GORM directly. SQLite lets unit tests exercise create, query, update, delete, task, and statistics paths deterministically inside the test process.
  Date/Author: 2026-05-07 / Codex
- Decision: Return carried unrecoverable issues after repair even when repair removes stale derived records.
  Rationale: If Meilisearch points to a missing HTML file, rebuilding the index can remove the stale search document, but the archived content itself is still lost. The user asked to surface unrecoverable consistency problems, so the repair response keeps those losses visible.
  Date/Author: 2026-05-07 / Codex
- Decision: Leave the Docker Compose stack running after integration testing.
  Rationale: The user specifically asked to start Docker Compose and continue testing. Stopping the stack would undo that running test environment without being asked.
  Date/Author: 2026-05-07 / Codex

## Outcomes & Retrospective

Implemented the backend consistency mechanism and the operator page. The backend now checks the archive file tree, Meilisearch documents, and database statistics; it repairs recoverable drift by rebuilding the search index from existing HTML files and refreshing database statistics from disk; and it reports unrecoverable items such as missing HTML files or unparseable HTML files.

Docker Compose integration testing passed after one frontend null-handling fix. A real test HTML file at `/archive/integration.example/dataark-consistency-20260507.html` first produced recoverable Meilisearch/database issues, `POST /api/archiveConsistency/repair` rebuilt 5 documents and refreshed 5 statistic sources, a follow-up check returned `consistent:true`, and `/api/search?q=integration&p=1` returned the test page.

The requested 100% backend coverage was not reached. The latest measured total is 66.9%. The new and refactored code has focused tests, and several packages improved substantially, but existing paths that run live PostgreSQL initialization, live Meilisearch backup/restore flows, external SingleFile archive processing, and long-running queue workers remain uncovered without deeper architectural changes.

## Context and Orientation

DataArk is a monorepo. Backend Go code lives under `api/`, and frontend Vue code lives under `web/`.

The backend entry point is `api/main.go`, which calls `common.ParseFlag()` and then `api.WebStarter(common.DEBUG)`. `api/api/controller.go` contains Gin HTTP handlers and the router setup. The authenticated API group already includes archive endpoints such as `/api/archiveByURL`, `/api/archiveTask/:taskId`, `/api/archiveStats`, `/api/archiveStats/refresh`, and `/api/archive` deletion.

The archive directory is configured by `common.ARCHIVEFILELOACTION` in `api/common/config.go` and `api/common/flag.go`. Archived files are served from `/archive` with authentication. Uploaded HTML files first land under `{archiveRoot}/Temporary`; after indexing, they are moved to `{archiveRoot}/{domain}/{filename}`.

Meilisearch is configured by `common.MEILIHOST`, `common.MEILIAPIKey`, and `common.MEILIBlogsIndex`. Search documents use fields `id`, `title`, `filename`, `domain`, and `content`. The field pair `domain` and `filename` ties a Meilisearch document back to an HTML file.

The database is managed through GORM in `api/common/db.go`. Existing archive-related models are `ArchiveTask`, which records URL archive jobs, and `ArchiveStat`, which stores aggregate counts per source domain. The database does not currently have a per-file archive table, so the consistency feature should rely on the file tree and Meilisearch document fields rather than inventing a broad new persistence model unless implementation proves one is required.

The frontend route table is `web/src/router/index.ts`. The home screen is `web/src/views/IndexView.vue`. Existing operator pages include `web/src/views/StatsView.vue`, `web/src/views/BackupView.vue`, and `web/src/views/ArchiveUrlView.vue`. The HTML viewer is `web/src/views/HtmlView.vue`.

## Plan of Work

First, add a backend consistency service in the search or common boundary, with pure helper functions that can be unit tested. The service will scan HTML files on disk into file identities of the form `{domain, filename, path}`, list Meilisearch documents into index identities of the form `{id, domain, filename}`, and read database statistics. It will compare the three stores and produce a report with recoverable and unrecoverable problems. A recoverable problem is one where the HTML file exists and either Meilisearch or database statistics can be rebuilt. An unrecoverable problem is one where the HTML file is missing but Meilisearch still has one or more documents pointing at it, or where an HTML file exists but cannot be parsed into a searchable document because it has no readable title or text.

Second, implement repair as an explicit backend operation. Repair will rebuild the Meilisearch index from existing archive HTML files using the existing `RebuildIndexFromArchive` path or a shared lower-level helper, then refresh archive statistics using `common.RefreshArchiveStatsFromDisk`, then run the consistency check again and return the remaining unrecoverable items. If rebuild fails because an HTML file cannot be parsed, the service will not delete the file. It will return an unrecoverable item with the file path and parse error so the user can inspect or delete it deliberately.

Third, expose the service through authenticated API endpoints in `api/api/controller.go` and `api/api/WebStarter`. The planned endpoints are `GET /api/archiveConsistency` for check-only and `POST /api/archiveConsistency/repair` for repair. Both endpoints return JSON with `Status`, `Message`, and `Data`. `Data` includes counts for files, Meilisearch documents, database statistic total, recoverable issue count, unrecoverable issue count, issue details, and repair actions taken.

Fourth, add a frontend page `web/src/views/ConsistencyView.vue` and a route such as `/consistency`. The page will show the three-store summary, buttons for checking and repairing, a recoverable issue list, and an unrecoverable issue list. It will use Arco components and the existing quiet operational visual style. Add a navigation button on `IndexView.vue`. The page will not claim a repair succeeded unless the API returns no unrecoverable items or clearly explains what remains.

Fifth, add backend tests. Pure tests will cover archive file scanning, identity comparison, report generation, repair decision logic, JSON response shaping where feasible, safe path handling, HTML parsing edge cases, token helpers, archive stat helpers, backup helper functions, and controller helper functions. Where current functions directly call PostgreSQL, Meilisearch, or external SingleFile services, introduce narrow interfaces and fake implementations for unit tests. The coverage target is 100% for backend packages after excluding code that cannot be executed without starting the process or binding a live server only if Go tooling makes that unavoidable. If a package remains impossible to cover without changing production behavior, record the exact residual gap here with `go tool cover -func` evidence.

Sixth, validate. Run `cd api && go test ./... -coverprofile=/tmp/dataark-cover.out` and `cd api && go tool cover -func=/tmp/dataark-cover.out`. Run `cd web && npm run build`. Because this touches frontend UI code, start or reuse the Vite dev server with `cd web && npm run dev`, open the new page through chrome-devtools MCP, take before/after screenshots where possible, check console errors and failed network requests, and inspect the DOM snapshot.

## Concrete Steps

From the repository root `/home/harson/DataArk`, inspect the files that define the current data contracts:

    sed -n '1,320p' api/common/db.go
    sed -n '1,260p' api/common/archive_stats.go
    sed -n '1,260p' api/search/rebuild.go
    sed -n '1,260p' api/search/delete.go
    sed -n '1,760p' api/api/controller.go

Create new backend code. Preferred file locations are:

    api/search/consistency.go
    api/search/consistency_test.go
    api/api/controller_test.go
    api/common/html_oper_test.go
    api/common/auth_test.go

Add API handlers to `api/api/controller.go` and register them inside the authenticated `/api` group:

    protected.GET("/archiveConsistency", GetArchiveConsistency)
    protected.POST("/archiveConsistency/repair", RepairArchiveConsistency)

Create frontend UI:

    web/src/views/ConsistencyView.vue

Register the route in `web/src/router/index.ts`:

    {
      path: '/consistency',
      name: 'consistency',
      component: () => import('@/views/ConsistencyView.vue')
    }

Add a button on `web/src/views/IndexView.vue` that routes to `/consistency`.

Run backend validation:

    cd api
    go test ./... -coverprofile=/tmp/dataark-cover.out
    go tool cover -func=/tmp/dataark-cover.out

Latest observed output:

    ok  	DataArk	0.015s	coverage: 100.0% of statements
    ok  	DataArk/api	0.025s	coverage: 95.2% of statements
    ok  	DataArk/assets	0.003s	coverage: 100.0% of statements
    ok  	DataArk/backup	0.134s	coverage: 54.9% of statements
    ok  	DataArk/common	0.558s	coverage: 80.1% of statements
    ok  	DataArk/search	0.027s	coverage: 52.5% of statements
    total:					(statements)				66.9%

Run frontend validation:

    cd web
    npm run build
    npm run dev

Use chrome-devtools MCP against the Vite URL to open `/consistency`, capture screenshots, inspect the DOM snapshot, and check console and network failures.

## Validation and Acceptance

Backend acceptance is met when `GET /api/archiveConsistency` returns a JSON report without mutating data, and `POST /api/archiveConsistency/repair` rebuilds recoverable stores from disk and reports remaining unrecoverable items. A human can verify this by creating a temporary archive directory with a valid HTML file and an inconsistent Meilisearch fake in tests; the check should identify the missing index document as recoverable, and repair should remove that recoverable issue.

User-facing acceptance is met when an authenticated user can open `/consistency`, click a check button, see current counts for HTML files, Meilisearch documents, and database statistics, click a repair button, and see clear remaining unrecoverable issues. If a Meilisearch document points to a missing HTML file, the page must show that as unrecoverable because the archived content cannot be reconstructed from the derived search record.

Coverage acceptance is measured with:

    cd api && go test ./... -coverprofile=/tmp/dataark-cover.out
    cd api && go tool cover -func=/tmp/dataark-cover.out

The desired result is 100.0% total backend statement coverage. If the final total cannot honestly reach 100.0% without replacing process entry points or live external service calls with artificial code, this plan must record the exact uncovered functions and the reason they were left uncovered.

Frontend acceptance is measured with:

    cd web && npm run build

Browser acceptance is measured by opening the new page with chrome-devtools MCP, taking screenshots, checking that the DOM renders the summary and issue lists without overlap, checking console errors, and checking failed network requests.

Latest browser evidence:

    Vite URL: http://127.0.0.1:5173/#/consistency
    Screenshot: /tmp/dataark-consistency-page-error.png
    Console: no console messages found
    DOM: rendered heading `归档一致性`, summary counters, `可恢复问题`, and `无法恢复`
    Network: frontend assets loaded; `/api/archiveConsistency` was served by Vite because the Go backend was not running, and the UI surfaced `一致性响应缺少数据`

Docker Compose integration evidence:

    docker compose up -d --build
    Result: dataarkapi, database, meili, and singlefile-webservice were running; database was healthy.

    GET /api/archiveConsistency before repair:
    Result: consistent=false, htmlFiles=5, meiliDocuments=4, databaseStatTotal=4, recoverableIssues included integration.example missing search index and database stats.

    POST /api/archiveConsistency/repair:
    Result: consistent=true, indexedDocuments=5, refreshedStatSources=5, recoverableIssues=[], unrecoverableIssues=[].

    GET /api/search?q=integration&p=1:
    Result: Status=1, TotalHits=2, and one result was `DataArk Consistency Integration 20260507` from `integration.example/dataark-consistency-20260507.html`.

    Browser: http://127.0.0.1:7845/#/consistency
    Screenshot: /tmp/dataark-docker-consistency-page-fixed.png
    Console after fix: no console messages found
    Network after fix: `/api/authChecker` 200, `/api/archiveConsistency` 200; `/favicon.ico` 404 is an existing static asset gap unrelated to this feature.

## Idempotence and Recovery

The consistency check endpoint is read-only and safe to run repeatedly. The repair endpoint is designed to be idempotent because it rebuilds Meilisearch from the same HTML files and refreshes database statistics from the same disk scan. If repair fails midway, running it again should retry the same reconstruction from disk. The repair endpoint must not delete HTML files automatically when they cannot be parsed, because the HTML file is the highest-weight source and may be manually recoverable.

The implementation must not revert unrelated working-tree changes. If a generated frontend build updates embedded assets, keep those changes only if they are part of the explicit build output needed by the repository workflow.

## Artifacts and Notes

Initial backend coverage before implementation:

    DataArk          coverage: 0.0% of statements
    DataArk/api      coverage: 0.0% of statements
    DataArk/assets   coverage: 0.0% of statements
    DataArk/backup   coverage: 25.2% of statements
    DataArk/common   coverage: 8.0% of statements
    DataArk/search   coverage: 6.2% of statements

Existing search document shape:

    {
      "id": "...",
      "title": "...",
      "filename": "...",
      "domain": "...",
      "content": "..."
    }

## Interfaces and Dependencies

In `api/search/consistency.go`, define types similar to these final public interfaces:

    type ArchiveConsistencyIssue struct {
        Severity string `json:"severity"`
        Store string `json:"store"`
        Domain string `json:"domain"`
        Filename string `json:"filename"`
        Path string `json:"path"`
        Message string `json:"message"`
        Recoverable bool `json:"recoverable"`
    }

    type ArchiveConsistencyReport struct {
        CheckedAt string `json:"checkedAt"`
        HTMLFiles int `json:"htmlFiles"`
        MeiliDocuments int `json:"meiliDocuments"`
        DatabaseStatTotal int `json:"databaseStatTotal"`
        RecoverableIssues []ArchiveConsistencyIssue `json:"recoverableIssues"`
        UnrecoverableIssues []ArchiveConsistencyIssue `json:"unrecoverableIssues"`
        Actions []string `json:"actions"`
    }

    func CheckArchiveConsistency(ctx context.Context) (*ArchiveConsistencyReport, error)
    func RepairArchiveConsistency(ctx context.Context) (*ArchiveConsistencyReport, error)

If direct Meilisearch calls make tests brittle, define a small internal interface whose fake implementation can return documents and record rebuild calls. Do not expose broad Meilisearch client types to the rest of the application unless existing code already requires them.

Revision note 2026-05-07 / Codex: Created the plan after the user reminded that the ExecPlan must be written before implementation. The plan records pre-implementation discoveries and the intended sequence before any feature files are changed.

Revision note 2026-05-07 / Codex: Updated progress after implementing the consistency feature, adding the frontend page, adding tests, and running validation. This revision records that backend coverage improved but did not reach the requested 100% total.

Revision note 2026-05-07 / Codex: Added Docker Compose integration results and the frontend null-list fix discovered by production-page testing.
