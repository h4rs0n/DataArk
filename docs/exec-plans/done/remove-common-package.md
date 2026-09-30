# Remove the Go common package

> 归档说明（2026-09-30 核对）：本文保留实施当时的背景、决策、路径与验证记录，部分内容已被后续变更取代。现行行为与操作入口见 [文档索引](../../README.md)。历史测试输出不代表当前部署状态。

This ExecPlan is a living document. It follows the repository instructions in `PLANS.md` and must keep `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` current.

## Purpose / Big Picture

The backend currently has a `DataArk/common` package that mixes configuration, database setup, authentication, archive metadata, discovery, recommendation, and HTML helpers. After this change, each responsibility lives in a package named for its domain, so future contributors can find code by feature and imports communicate intent. The observable result is that `rg 'DataArk/common|package common' api` returns no matches and `cd api && go test ./...` passes.

## Progress

- [x] (2026-06-29 10:20Z) Read repository guidance, `PLANS.md`, and the initial `api/common` inventory.
- [x] (2026-06-29 10:35Z) Created feature-focused packages and moved files out of `api/common`.
- [x] (2026-06-29 10:50Z) Rewrote imports and package references across the backend.
- [x] (2026-06-29 11:05Z) Ran `gofmt -w api` and `env GOCACHE=/tmp/dataark-go-cache go test ./...`.
- [x] (2026-06-29 11:08Z) Confirmed `rg 'DataArk/common|package common|common\.' api -n` returns no matches.
- [x] (2026-06-29 11:10Z) Recorded final outcome and remaining risks.

## Surprises & Discoveries

- Observation: `api/common/db.go` contains both shared database initialization and business-domain models for archive, discovery, and recommendation.
  Evidence: `db.go` defines `User`, `ArchiveTask`, `DiscoverySource`, `RecommendationDay`, and `InitDB` in one file.
- Observation: The sandbox blocks tests that use `httptest.NewServer`.
  Evidence: the sandboxed `go test ./...` run failed in `DataArk/search` with `listen tcp6 [::1]:0: socket: operation not permitted`, then the same command passed with elevated permissions.

## Decision Log

- Decision: Split by existing domain packages where they already exist, and create narrow packages only for responsibilities that lack an existing home.
  Rationale: The user explicitly called out `recommendation.go` moving to recommendation and `flag.go` moving to a new flag folder; following that pattern keeps code discoverable without inventing broad replacement utility packages.
  Date/Author: 2026-06-29 / Codex.
- Decision: Keep `api/database` limited to connection and migration plumbing, and add `api/bootstrap` for application startup wiring.
  Rationale: If `database` imported every domain model while domain packages imported `database`, Go would reject the cycle. `bootstrap` can import all domain packages and inject the shared `*gorm.DB` without creating a cycle.
  Date/Author: 2026-06-29 / Codex.
- Decision: Use a type alias in `api/recommendation` for `discovery.DiscoveryCandidate`.
  Rationale: Recommendation code consumes discovery candidates but should not duplicate the database model. The alias keeps existing recommendation code readable while making ownership explicit.
  Date/Author: 2026-06-29 / Codex.

## Outcomes & Retrospective

The `api/common` package was removed. Runtime configuration now lives in `api/config`, command-line parsing in `api/flag`, database connection and migrations in `api/database`, startup database wiring in `api/bootstrap`, auth code in `api/auth`, archive code in `api/archive`, discovery persistence and scheduling in `api/discovery`, and recommendation services in `api/recommendation`.

Validation passed with:

    cd api
    env GOCACHE=/tmp/dataark-go-cache go test ./...

The final search for `DataArk/common`, `package common`, and `common.` under `api/` returned no matches. The main remaining risk is package-level database state: this refactor preserves the existing global `*gorm.DB` pattern by giving each domain package a `SetDB` hook, rather than redesigning persistence injection.

## Context and Orientation

The Go module is rooted at `api/` and uses import paths like `DataArk/search`. The package to remove is `api/common`. It currently provides global configuration values, command-line flag parsing, GORM database initialization, user and token helpers, archive task and metadata helpers, discovery source/candidate helpers, recommendation generation helpers, and HTML parsing helpers. Callers are in `api/api`, `api/search`, `api/backup`, `api/main.go`, and tests.

The target package layout is:

- `api/config` for global runtime configuration values.
- `api/flag` for command-line flag parsing that writes into `api/config`.
- `api/database` for opening the database, running migrations, and exposing the shared GORM handle.
- `api/auth` for user models, password helpers, JWT helpers, and user persistence.
- `api/archive` for archive tasks, archive metadata, archive statistics, path resolution, and HTML helpers.
- `api/discovery` for discovery source and candidate models plus discovery fetching/scheduling logic, extending the existing package.
- `api/recommendation` for recommendation models, generation, queueing, scheduling, vector helpers, and providers, extending the existing package.

## Plan of Work

First, split `api/common/db.go` into model and repository files owned by the packages above. `api/database` will keep the single GORM connection and expose `DB()` for packages that need database access. Then move the remaining `api/common/*.go` files to their target packages and replace package-local global configuration references with `config.<Name>`. Finally, rewrite all callers from `common.<Name>` to the appropriate domain package and remove `api/common`.

## Concrete Steps

Run these commands from the repository root:

    rg 'DataArk/common|common\.' api
    gofmt -w api
    cd api && go test ./...
    rg 'DataArk/common|package common' api

The final search should print no matches.

Actual validation result:

    ok  	DataArk/search	0.031s
    ok  	DataArk/recommendation	(cached)
    ok  	DataArk/discovery	(cached)
    ok  	DataArk/archive	(cached)

## Validation and Acceptance

The change is accepted when the backend tests compile and pass with `cd api && go test ./...`, and when no Go file imports or declares `common`. Because this is an internal package reorganization, the main proof is that existing behavior remains covered by the current backend test suite.

## Idempotence and Recovery

Moving files and replacing imports is safe to repeat if each step is checked with `rg` and `go test`. If a cycle appears, move only the smallest shared dependency to the package that owns the underlying concept rather than creating a new generic package.

## Artifacts and Notes

Important transcripts will be added after validation.

## Interfaces and Dependencies

At completion, these representative APIs must exist:

- `config.DEBUG`, `config.MEILIHOST`, `config.ARCHIVEFILELOACTION`, and other runtime variables.
- `flag.ParseFlag()`.
- `database.InitDB()` and `database.DB() *gorm.DB`.
- `auth.User`, `auth.RegisterWithToken`, `auth.LoginWithToken`, `auth.ValidateToken`.
- `archive.ArchiveTask`, `archive.ResolveArchiveDocumentPath`, `archive.GetArchiveStats`, `archive.RefreshArchiveStatsFromDisk`.
- `discovery.DiscoverySource`, `discovery.ListDiscoverySources`, `discovery.FetchDiscoverySourceByID`, `discovery.StartDiscoveryScheduler`.
- `recommendation.RecommendationSettings`, `recommendation.GenerateDailyRecommendations`, `recommendation.StartRecommendationScheduler`, `recommendation.StartRecommendationJobQueue`.
