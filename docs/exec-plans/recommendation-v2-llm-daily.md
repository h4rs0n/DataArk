# Build LLM daily recommendations for content discovery

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds.

This plan follows `PLANS.md` from the repository root.

## Purpose / Big Picture

DataArk already discovers candidate articles from RSS, Atom, sitemap, and same-site sources, but it does not produce a stable daily recommendation digest. After this work, an authenticated user can open the recommendation center and see today's immutable article recommendations, switch to historical daily snapshots, give five kinds of feedback, and have later recommendations shaped by that feedback. The first implementation milestone creates the durable foundation: database constraints, user-scoped recommendation records, settings, feedback, block rules, provider interfaces, and SSRF protection for future crawler work.

## Progress

- [x] (2026-06-28 18:05+08:00) Audited the existing recommendation center, discovery models, authenticated user context, and current in-process discovery scheduler.
- [x] (2026-06-28 18:10+08:00) Decided the first implementation slice must be the foundational PR1 from the design: migrations, models, feature flags, SSRF guard, provider interfaces, and user-scoped daily snapshot APIs.
- [x] (2026-06-28 18:35+08:00) Upgraded `api/go.mod` to `go 1.26.0` with `toolchain go1.26.4` and refreshed backend dependencies.
- [x] (2026-06-28 18:45+08:00) Added runtime migration support and PostgreSQL schema for recommendation v2.
- [x] (2026-06-28 18:55+08:00) Added user-scoped recommendation models and persistence helpers.
- [x] (2026-06-28 19:05+08:00) Added recommendation settings, block rule, daily snapshot, and feedback HTTP APIs.
- [x] (2026-06-28 19:12+08:00) Added SSRF guard primitives for later feed and crawler fetchers.
- [x] (2026-06-28 19:15+08:00) Added provider interfaces for embedding, enrichment, and reranking without binding business code to a model vendor.
- [x] (2026-06-28 19:20+08:00) Updated Meilisearch SDK call sites and tests for the upgraded dependency.
- [ ] Validate with a full `cd api && go test ./...` outside the restricted sandbox.

## Surprises & Discoveries

- Observation: The current API middleware already stores `user_id` in the Gin context.
  Evidence: `api/api/middleware.go` defines `GetCurrentUserID`, and authenticated route handlers can use it to scope recommendation settings, daily snapshots, feedback, and block rules.
- Observation: Existing test helpers use SQLite and `AutoMigrate`, while the target production schema needs PostgreSQL-only features such as `pgvector` and partial unique indexes.
  Evidence: `api/common/db_test.go` uses `gorm.io/driver/sqlite`; this plan keeps SQLite-compatible GORM models for tests and adds Goose SQL migrations for production-only constraints.
- Observation: Upgrading `github.com/meilisearch/meilisearch-go` from v0.32 to v0.36 changed document APIs.
  Evidence: `AddDocuments`, `AddDocumentsWithContext`, `DeleteDocument`, and `DeleteDocuments` now take document options; search hits are `map[string]json.RawMessage`; `GetDocuments` uses `POST /documents/fetch`.
- Observation: The managed sandbox blocks local listener sockets used by `httptest.NewServer`.
  Evidence: full `go test ./...` failed in `DataArk/search` with `httptest: failed to listen on a port: listen tcp6 [::1]:0: socket: operation not permitted`. A compile-only test run and focused non-listener tests passed.

## Decision Log

- Decision: Implement the plan in milestones, starting with PR1 foundation rather than UI or LLM prompts.
  Rationale: Daily snapshots, user scoping, unique constraints, and SSRF protection are prerequisites. Building feedback buttons or LLM reranking before those constraints would create behavior that cannot safely satisfy "no cross-day duplicates" or "feedback by user".
  Date/Author: 2026-06-28 / Codex
- Decision: Keep GORM `AutoMigrate` for existing simple tables and tests, but introduce Goose migrations for recommendation v2 production schema.
  Rationale: AutoMigrate cannot express pgvector extensions, HNSW indexes, or partial unique indexes reliably. The current SQLite tests still need lightweight model migration.
  Date/Author: 2026-06-28 / Codex
- Decision: Add provider interfaces before concrete OpenAI/Ollama implementations.
  Rationale: The business layer should depend on `EmbeddingProvider`, `EnrichmentProvider`, and `RerankProvider`, so model vendors and local runtimes can be swapped without rewriting recommendation logic.
  Date/Author: 2026-06-28 / Codex
- Decision: Upgrade the backend module to Go 1.26 and allow latest dependency versions before continuing the feature work.
  Rationale: The user explicitly requested the Go version upgrade and dependency refresh. This also allows using the latest Goose release rather than pinning an older Go 1.23-compatible version.
  Date/Author: 2026-06-28 / User and Codex
- Decision: Keep production JSON/pgvector-only schema in Goose migrations while preserving SQLite-compatible model tests.
  Rationale: Existing unit tests use SQLite, but the production recommendation system needs PostgreSQL partial indexes and `vector`. Goose owns those details; GORM models provide shape and testability.
  Date/Author: 2026-06-28 / Codex

## Outcomes & Retrospective

The PR1 foundation is implemented. The backend module now targets Go 1.26, dependencies are updated, Goose migrations create recommendation v2 schema, user-scoped settings/daily/feedback/block APIs exist, SSRF guard tests pass, and provider interfaces are in place. Full runtime recommendation generation, feed/crawler replacement, embeddings, LLM enrichment, vector retrieval, MMR, River jobs, and frontend redesign remain for later milestones.

## Context and Orientation

The Go backend lives under `api/`. Existing database models are in `api/common/db.go`, the discovery code is currently in `api/common/discovery.go`, and HTTP handlers are in `api/api/controller.go`. Authentication middleware in `api/api/middleware.go` attaches the current user ID to each protected request. The frontend recommendation center is `web/src/views/RecommendationsView.vue`, but this milestone does not rewrite that UI yet.

"Daily snapshot" means a database row for one user's recommendations for a specific date, plus child rows for the recommended articles in that exact order. Historical pages read these rows directly. They are not recomputed when the user's preferences change later.

"SSRF" means server-side request forgery: a user configures a URL that tricks DataArk into fetching private network addresses such as localhost, RFC1918 ranges, link-local addresses, or cloud metadata endpoints. The crawler and feed fetchers must reject such targets before connecting and after redirects.

## Plan of Work

First, add a production migration path under `api/common` using Goose and embedded SQL files under `api/migrations`. The first SQL migration creates the pgvector extension when available, the recommendation settings, daily snapshot, daily item, feedback, block rule, and user profile tables, and the unique indexes that enforce no repeated candidate or dedupe key per user.

Second, extend `api/common/db.go` with SQLite-compatible GORM models for those same entities. Add persistence helpers for getting and updating settings, listing recommendation days, reading a specific day, creating a day idempotently, writing feedback, creating block rules, and deleting block rules.

Third, extend `api/common/config.go` and `api/common/flag.go` with recommendation and LLM configuration. The first milestone stores configuration but does not call an LLM yet.

Fourth, add `api/recommendation/provider.go` with provider interfaces and lightweight domain structs. Add `api/discovery/ssrf.go` with SSRF validation helpers that future fetchers can call before network requests.

Fifth, expose authenticated APIs in `api/api/controller.go` under `/api/recommendations/*`. These endpoints use `GetCurrentUserID` and never operate on global recommendation state. The endpoints return empty but well-formed daily snapshots until the generator milestone is implemented.

Sixth, add focused tests for settings defaults, daily snapshot idempotence, feedback validation, block rules, and SSRF rejection. Run all Go tests.

## Concrete Steps

Work from `/home/harson/sideProj/DataArk`.

The backend module now targets Go 1.26:

    cd api && go version
    go version go1.26.4 linux/amd64

Dependencies were upgraded from `api/` with:

    go mod edit -go=1.26.0 -toolchain=go1.26.4
    go get -u ./...
    go mod tidy

Run backend tests:

    cd api && env GOCACHE=/tmp/dataark-go-cache go test ./...

Commit only files related to this milestone. Preserve existing unrelated working tree changes in `AGENTS.md`, `makefile`, `.codex`, and `passwd.txt`.

## Validation and Acceptance

The PR1 milestone is accepted when the backend compiles and, in an environment that allows `httptest` local listeners, `cd api && env GOCACHE=/tmp/dataark-go-cache go test ./...` passes. In the restricted sandbox used during this implementation, the following commands passed:

    cd api && env GOCACHE=/tmp/dataark-go-cache go test ./... -run '^$'
    ok  	DataArk/search	0.006s [no tests to run]

    cd api && env GOCACHE=/tmp/dataark-go-cache go test ./common -run 'TestRecommendation|TestParseFlag'
    ok  	DataArk/common	0.022s

    cd api && env GOCACHE=/tmp/dataark-go-cache go test ./discovery
    ok  	DataArk/discovery

    cd api && env GOCACHE=/tmp/dataark-go-cache go test ./search -run 'TestDocumentString|TestNormalizeArchiveURL|TestResolveArchiveDocumentPath'
    ok  	DataArk/search	0.007s

The new tests prove:

- default recommendation settings return `dailyLimit=10`;
- a recommendation day is scoped by `user_id` and date;
- duplicate daily items cannot be created for the same user and candidate through the service helper;
- feedback actions accept only the five supported semantics;
- block rules can be created and deactivated per user;
- SSRF guard rejects localhost, private IPv4, link-local, IPv6 local/private, and cloud metadata URLs.

The full feature is accepted only after later milestones implement feed/crawler replacement, enrichment, embeddings, vector retrieval, MMR, LLM reranking, River jobs, and frontend verification.

## Idempotence and Recovery

Goose migrations are idempotent because they run once per database version. Daily generation will later rely on `UNIQUE(user_id, recommendation_date)` and `UNIQUE(user_id, dedupe_key)` so retries and concurrent workers cannot create duplicate daily recommendations. In this milestone, helper functions should treat existing settings and day rows as reusable state rather than errors where that behavior is user-visible.

## Artifacts and Notes

Validation evidence:

    cd api && env GOCACHE=/tmp/dataark-go-cache go test ./... -run '^$'
    ok  	DataArk	0.007s [no tests to run]
    ok  	DataArk/api	0.007s [no tests to run]
    ok  	DataArk/assets	0.003s [no tests to run]
    ok  	DataArk/backup	0.005s [no tests to run]
    ok  	DataArk/common	0.005s [no tests to run]
    ok  	DataArk/discovery	0.003s [no tests to run]
    ok  	DataArk/search	0.006s [no tests to run]

    Full go test in the sandbox:
    FAIL DataArk/search
    panic: httptest: failed to listen on a port: listen tcp6 [::1]:0: socket: operation not permitted

This failure is environmental; the compile-only run and focused non-listener tests passed.

## Interfaces and Dependencies

In `api/recommendation/provider.go`, define:

    type EmbeddingProvider interface {
        Embed(ctx context.Context, texts []string) ([][]float32, error)
    }

    type EnrichmentProvider interface {
        Enrich(ctx context.Context, input EnrichmentInput) (EnrichmentResult, error)
    }

    type RerankProvider interface {
        Rerank(ctx context.Context, input RerankInput) (RerankResult, error)
    }

In `api/discovery/ssrf.go`, define a guard function that validates HTTP and HTTPS URLs, rejects unsafe resolved IPs, and can be reused by future feed and crawler fetchers.

## Revision Notes

2026-06-28: Created the implementation ExecPlan from the combined user plan and repository audit. The first milestone is deliberately limited to foundation work because the complete feature spans multiple independently verifiable changes.

2026-06-28: Revised the plan after the user requested Go 1.26 and dependency updates. The first milestone now includes dependency upgrade work and Meilisearch SDK adaptation.
