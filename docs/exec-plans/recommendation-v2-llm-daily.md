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
- [x] (2026-06-28 19:35+08:00) Validated PR1 foundation with full `cd api && env GOCACHE=/tmp/dataark-go-cache go test ./...`.
- [x] (2026-06-28 19:45+08:00) Added `gofeed`, `colly/v2`, `go-domdistiller`, and `purell` dependencies for the collection layer milestone.
- [x] (2026-06-28 19:55+08:00) Replaced hand-written RSS/Atom feed parsing with `gofeed`, adding JSON Feed support.
- [x] (2026-06-28 20:05+08:00) Routed discovery HTTP fetches through the SSRF guard and added redirect validation.
- [x] (2026-06-28 20:15+08:00) Replaced site link discovery with a bounded Colly crawler that limits domain, depth, page count, concurrency, and delay.
- [x] (2026-06-28 20:25+08:00) Added article URL normalization, content hashing, and DOM Distiller-based article extraction helpers.
- [x] (2026-06-28 20:30+08:00) Validated the collection layer with full `cd api && env GOCACHE=/tmp/dataark-go-cache go test ./...`.
- [x] (2026-06-28 20:45+08:00) Added the candidate enrichment service that writes structured provider output back to `discovery_candidates`.
- [x] (2026-06-28 20:50+08:00) Added a rule-based enrichment provider for local fallback and deterministic tests.
- [x] (2026-06-28 20:55+08:00) Validated enrichment with full `cd api && env GOCACHE=/tmp/dataark-go-cache go test ./...`.
- [x] (2026-06-28 21:20+08:00) Added deterministic daily recommendation generation with candidate filtering, scoring, diversity selection, and immutable snapshot reuse.
- [x] (2026-06-28 21:30+08:00) Added feedback-derived user profile rebuilding for topic, source, style, and depth preferences.
- [x] (2026-06-28 21:35+08:00) Connected the admin generate endpoint to the real daily generation service.
- [x] (2026-06-28 21:55+08:00) Added optional reranker integration with strict candidate ID validation and deterministic fallback.
- [x] (2026-06-28 22:35+08:00) Added snapshot candidate attachment so daily recommendation cards can render article title, URL, summary, source, and topics.
- [x] (2026-06-28 22:45+08:00) Added process-local daily recommendation scheduler controlled by recommendation settings and generation time.
- [x] (2026-06-28 23:05+08:00) Added OpenAI-compatible provider for embeddings, enrichment, and reranking with fake-client tests.
- [x] (2026-06-28 23:15+08:00) Added pending candidate enrichment before daily generation, with rule-based fallback when remote enrichment fails.
- [x] (2026-06-28 23:30+08:00) Reworked `RecommendationsView.vue` so the recommendation center opens on Today, supports history, settings, block rules, sources, candidates, and five feedback actions.
- [x] (2026-06-28 23:40+08:00) Verified the recommendation page in Vite with Chrome DevTools screenshot, DOM snapshot, and console check.
- [x] (2026-06-28 23:55+08:00) Added pgvector-backed candidate embedding storage, profile embedding aggregation, and vector-boosted candidate recall for PostgreSQL installs.
- [x] (2026-06-28 23:58+08:00) Added River-backed durable daily recommendation jobs, River schema migration, scheduler enqueue path, and synchronous fallback for non-PostgreSQL/test installs.
- [x] (2026-06-28 23:59+08:00) Validated the completed backend plan with focused recommendation tests and full `go test ./...` using Go 1.26.4.

## Surprises & Discoveries

- Observation: The current API middleware already stores `user_id` in the Gin context.
  Evidence: `api/api/middleware.go` defines `GetCurrentUserID`, and authenticated route handlers can use it to scope recommendation settings, daily snapshots, feedback, and block rules.
- Observation: Existing test helpers use SQLite and `AutoMigrate`, while the target production schema needs PostgreSQL-only features such as `pgvector` and partial unique indexes.
  Evidence: `api/common/db_test.go` uses `gorm.io/driver/sqlite`; this plan keeps SQLite-compatible GORM models for tests and adds Goose SQL migrations for production-only constraints.
- Observation: Upgrading `github.com/meilisearch/meilisearch-go` from v0.32 to v0.36 changed document APIs.
  Evidence: `AddDocuments`, `AddDocumentsWithContext`, `DeleteDocument`, and `DeleteDocuments` now take document options; search hits are `map[string]json.RawMessage`; `GetDocuments` uses `POST /documents/fetch`.
- Observation: The managed sandbox blocks local listener sockets used by `httptest.NewServer`.
  Evidence: full `go test ./...` failed in `DataArk/search` with `httptest: failed to listen on a port: listen tcp6 [::1]:0: socket: operation not permitted`. A compile-only test run and focused non-listener tests passed.
- Observation: After committing PR1 and continuing the next milestone, the same full test command succeeded in the current environment.
  Evidence: `cd api && env GOCACHE=/tmp/dataark-go-cache go test ./...` returned `ok` for all backend packages.
- Observation: `go-domdistiller` extracts readable text from article HTML but does not always return OpenGraph metadata.
  Evidence: the extractor test initially returned article text but empty description and URL. The helper now falls back to parsing `meta property="og:*"`, `meta name="description"`, and `link rel="canonical"` directly.
- Observation: The enrichment service can be tested without a network LLM by using a deterministic provider.
  Evidence: `recommendation.RuleBasedEnrichmentProvider` implements `EnrichmentProvider`, and `TestEnrichDiscoveryCandidateUpdatesStructuredFields` verifies database updates for status, normalized URL, content hash, dedupe key, topics, entities, scores, and provider model.
- Observation: A useful first daily generator does not need to wait for pgvector or a remote LLM.
  Evidence: `GenerateDailyRecommendations` now uses ready enriched candidates, feedback-derived weights, block rules, historical recommendation identity, and diversity constraints to generate a stable daily snapshot.
- Observation: Reranking can be integrated without making LLM availability a hard dependency.
  Evidence: `GenerateDailyRecommendationsWithReranker` accepts a `RerankProvider`, validates returned IDs against the input candidate pool, ignores invalid or duplicate IDs, records rerank metadata when valid, and falls back to deterministic order on provider errors.
- Observation: Daily generation was not usable from freshly fetched RSS candidates until pending candidates were enriched.
  Evidence: fetched candidates are inserted with `enrichment_status=pending`; `TestGenerateDailyRecommendationsEnrichesPendingCandidates` now proves generation enriches a pending candidate and writes it to the daily snapshot.
- Observation: The frontend needs article data inside each recommendation item, not just `candidateId`.
  Evidence: `GetRecommendationDaySnapshot` now attaches `DiscoveryCandidate` to each `RecommendationItem`, and the browser screenshot at `/tmp/dataark-recommendations-after.png` shows the Today page renders without console errors.
- Observation: pgvector and River must remain PostgreSQL-only while the existing unit tests use SQLite.
  Evidence: `StoreCandidateEmbedding`, `loadPGVectorCandidateIDs`, `StartRecommendationJobQueue`, and River migrations check `db.Dialector.Name() == "postgres"`; SQLite tests still exercise the fallback paths.
- Observation: The shell PATH became unable to resolve a Linux `go` binary during the final validation pass.
  Evidence: `go version` returned `permission denied`, while `/home/harson/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.linux-amd64/bin/go version` returned `go version go1.26.4 linux/amd64`; final tests used that absolute toolchain path.

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
- Decision: Keep existing content discovery service entrypoints while swapping internal feed/site discovery implementation.
  Rationale: The frontend and controller APIs already call `FetchDiscoverySource`; replacing internals with `gofeed`, SSRF guard, and Colly preserves compatibility while improving correctness and safety.
  Date/Author: 2026-06-28 / Codex
- Decision: Use DOM Distiller only for local HTML body extraction, not for network fetching.
  Rationale: Its `ApplyForURL` helper would bypass DataArk's SSRF guard. Fetching remains owned by DataArk; DOM Distiller receives already-fetched bytes.
  Date/Author: 2026-06-28 / Codex
- Decision: Add a rule-based enrichment provider before adding a remote OpenAI-compatible provider.
  Rationale: It proves the provider contract and database update path deterministically in tests, gives self-hosted installs a safe fallback, and keeps remote model integration isolated for the next milestone.
  Date/Author: 2026-06-28 / Codex
- Decision: Implement deterministic retrieval, scoring, and diversity before embedding retrieval and LLM reranking.
  Rationale: This creates a fully testable recommendation loop with stable snapshots and feedback behavior. Embeddings and LLM reranking can later improve ranking quality behind the same generator boundary without changing API semantics.
  Date/Author: 2026-06-28 / Codex
- Decision: Add the reranker boundary before adding a concrete OpenAI-compatible client.
  Rationale: Provider validation and fallback behavior are business invariants independent of vendor SDK choice. Testing them with an injected provider prevents remote LLM failures from blocking the daily generator.
  Date/Author: 2026-06-28 / Codex
- Decision: Add an in-process scheduler before River.
  Rationale: DataArk already has an in-process discovery scheduler and no River/PostgreSQL worker runtime is configured. The scheduler uses the same idempotent daily generation service and can later be replaced by River without changing the API or snapshot model.
  Date/Author: 2026-06-28 / Codex
- Decision: Implement the OpenAI-compatible provider with `net/http` instead of introducing a large orchestration framework.
  Rationale: The immediate need is embeddings, structured JSON enrichment, and rerank calls behind existing provider interfaces. A small provider is testable with a fake HTTP client and keeps LangChain-style orchestration out of the business layer.
  Date/Author: 2026-06-28 / Codex
- Decision: Gate River and pgvector behavior on PostgreSQL while keeping synchronous generation as a fallback.
  Rationale: Production needs durable jobs and vector recall, but the repository's local tests and lightweight installs use SQLite. Keeping the fallback preserves current operability and lets PostgreSQL deployments get the durable path.
  Date/Author: 2026-06-28 / Codex

## Outcomes & Retrospective

The PR1 foundation, PR2 collection-layer upgrade, PR3 enrichment data path, PR4 deterministic daily generator, PR5 reranker boundary, PR6 scheduler/provider wiring, PR7 recommendation center UI, and the infrastructure hardening slice are implemented. The backend module now targets Go 1.26, dependencies are updated, Goose migrations create recommendation v2 schema, River migrations create durable job tables for PostgreSQL, user-scoped settings/daily/feedback/block APIs exist, SSRF guard tests pass, provider interfaces are in place, feeds are parsed by `gofeed`, site discovery uses bounded Colly crawling, article normalization/extraction helpers exist, pending candidates are enriched before daily generation, OpenAI-compatible enrichment/embedding/rerank provider code exists, daily recommendation snapshots can be generated from enriched candidates with feedback-aware scoring and hard no-repeat filters, PostgreSQL installs can store pgvector embeddings and use vector recall as a ranking boost, the scheduler enqueues River jobs when available and falls back to synchronous generation otherwise, and the frontend opens on Today with history, settings, block rules, content sources, candidates, and feedback controls.

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

Seventh, upgrade the content collection internals. In `api/common/discovery.go`, feed sources use `gofeed` so RSS, Atom, and JSON Feed are handled through one parser. The existing `fetchDiscoveryURL` path validates the requested URL and redirects with `api/discovery/ssrf.go`. Site sources use a bounded Colly crawler to discover alternate feeds and same-host article links. In `api/discovery/normalizer.go` and `api/discovery/extractor.go`, add reusable helpers for URL normalization, content hashing, and DOM Distiller-based article text extraction.

Eighth, add the candidate enrichment data path. In `api/common/recommendation.go`, add `EnrichDiscoveryCandidate`, which loads a candidate, calls a `recommendation.EnrichmentProvider`, normalizes URL identity, computes content hash and dedupe key, serializes topics and entities, clamps scores, and updates `discovery_candidates` to `enrichment_status=ready` or `failed`. In `api/recommendation/rule_provider.go`, add a deterministic `RuleBasedEnrichmentProvider` so the data path can be tested and used as a fallback before remote LLM integration exists.

Ninth, add deterministic daily generation. In `api/common/recommendation.go`, add `GenerateDailyRecommendations`, which reuses an already generated snapshot, rebuilds a user profile from feedback, filters out historical candidates and dedupe keys, applies active block rules, scores candidates by quality/depth/freshness/profile weights, applies greedy diversity constraints, writes `recommendation_items`, and marks the day generated. Update the admin generate endpoint to call this service instead of creating an empty day.

Tenth, add the reranker boundary. Keep `GenerateDailyRecommendations` as the API-facing deterministic entrypoint, and add `GenerateDailyRecommendationsWithReranker` so future jobs can inject an LLM-backed `recommendation.RerankProvider`. Build a compact rerank input from candidate metadata and user profile hints, validate provider output against the supplied candidate IDs, ignore invalid or duplicate IDs, persist rerank score/model/prompt metadata, and fall back to deterministic order on provider errors or empty valid output.

Eleventh, wire generation into runtime and UI. Attach candidate data to recommendation snapshots, enrich pending candidates before generation, add an OpenAI-compatible provider implementation, add an in-process scheduler guarded by recommendation settings, and rework the recommendation center frontend so Today is the default screen with history, settings, block rules, source management, candidate management, and five feedback actions.

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

The PR1 and PR2 milestones are accepted when the backend compiles and `cd api && env GOCACHE=/tmp/dataark-go-cache go test ./...` passes. The following focused commands were also used during implementation:

    cd api && env GOCACHE=/tmp/dataark-go-cache go test ./... -run '^$'
    ok  	DataArk/search	0.006s [no tests to run]

    cd api && env GOCACHE=/tmp/dataark-go-cache go test ./common -run 'TestRecommendation|TestParseFlag'
    ok  	DataArk/common	0.022s

    cd api && env GOCACHE=/tmp/dataark-go-cache go test ./common -run 'TestParseFeedCandidatesSupportsRSSAtomAndJSON|TestRecommendation|TestParseFlag'
    ok  	DataArk/common	0.031s

    cd api && env GOCACHE=/tmp/dataark-go-cache go test ./discovery
    ok  	DataArk/discovery	0.008s

    cd api && env GOCACHE=/tmp/dataark-go-cache go test ./search -run 'TestDocumentString|TestNormalizeArchiveURL|TestResolveArchiveDocumentPath'
    ok  	DataArk/search	0.007s

    cd api && env GOCACHE=/tmp/dataark-go-cache go test ./common -run 'TestEnrichDiscoveryCandidate|TestRecommendation|TestParseFeed'
    ok  	DataArk/common	0.036s

    cd api && env GOCACHE=/tmp/dataark-go-cache go test ./common -run 'TestGenerateDailyRecommendations|TestRecommendation|TestEnrichDiscoveryCandidate'
    ok  	DataArk/common	0.060s

    cd api && env GOCACHE=/tmp/dataark-go-cache go test ./recommendation
    ok  	DataArk/recommendation	0.004s

    cd web && npm run build
    > web2@0.0.0 build
    > run-p type-check "build-only {@}" --

The new tests prove:

- default recommendation settings return `dailyLimit=10`;
- a recommendation day is scoped by `user_id` and date;
- duplicate daily items cannot be created for the same user and candidate through the service helper;
- feedback actions accept only the five supported semantics;
- block rules can be created and deactivated per user;
- SSRF guard rejects localhost, private IPv4, link-local, IPv6 local/private, and cloud metadata URLs.
- RSS, Atom, and JSON Feed parse into one discovered candidate each through `gofeed`;
- URL normalization removes known tracking parameters while preserving business query parameters;
- content hashes are stable across whitespace differences;
- article extraction returns readable text and metadata fallback values from HTML fixtures.
- candidate enrichment writes provider results to database fields and changes status to `ready`;
- failed enrichment records `enrichment_status=failed` and the error message.
- daily generation excludes previously recommended candidates and dedupe keys;
- daily generation applies user block rules before writing items;
- generated daily snapshots are reused unchanged on repeated generation calls;
- feedback-derived profiles can change the next day's ranking order.
- reranker output cannot introduce candidate IDs outside the supplied pool;
- reranker provider failures fall back to deterministic ordering.
- daily generation enriches pending candidates before selecting recommendations;
- OpenAI-compatible embedding, enrichment, and rerank responses parse through fake HTTP client tests;
- recommendation snapshots include candidate data for frontend rendering;
- the recommendation center renders in browser with no console messages.

The full planned backend and frontend feature is accepted for the repository implementation. Production PostgreSQL deployments should still validate River worker throughput, pgvector index performance, and real model quality against live data before broad rollout.

## Idempotence and Recovery

Goose migrations are idempotent because they run once per database version. Daily generation will later rely on `UNIQUE(user_id, recommendation_date)` and `UNIQUE(user_id, dedupe_key)` so retries and concurrent workers cannot create duplicate daily recommendations. In this milestone, helper functions should treat existing settings and day rows as reusable state rather than errors where that behavior is user-visible.

## Artifacts and Notes

Validation evidence:

    cd api && env GOCACHE=/tmp/dataark-go-cache go test ./...
    ok  	DataArk	0.017s
    ok  	DataArk/api	0.021s
    ok  	DataArk/assets	(cached)
    ok  	DataArk/backup	(cached)
    ok  	DataArk/common	0.642s
    ok  	DataArk/discovery	(cached)
    ok  	DataArk/recommendation	(cached)
    ok  	DataArk/search	(cached)

Final backend validation after adding pgvector recall and River jobs:

    cd api && env GOCACHE=/tmp/dataark-go-cache /home/harson/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.linux-amd64/bin/go test ./common -run 'TestRecommendation|TestGenerateDailyRecommendation|TestEmbedDiscoveryCandidate|TestRecommendationVector'
    ok  	DataArk/common	0.072s

    cd api && env GOCACHE=/tmp/dataark-go-cache /home/harson/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.linux-amd64/bin/go test ./... -count=1
    ok  	DataArk	0.016s
    ok  	DataArk/api	0.022s
    ok  	DataArk/assets	0.003s
    ok  	DataArk/backup	0.140s
    ok  	DataArk/common	0.686s
    ok  	DataArk/discovery	0.009s
    ?   	DataArk/migrations	[no test files]
    ok  	DataArk/recommendation	0.006s
    ok  	DataArk/search	0.035s

Browser verification:

    cd web && npm run dev -- --host 127.0.0.1
    Chrome DevTools opened http://127.0.0.1:5173/#/recommendations
    Console messages: none
    Screenshot: /tmp/dataark-recommendations-after.png

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

In `api/discovery/normalizer.go`, define:

    func NormalizeArticleURL(rawURL string) (string, error)
    func ContentHash(text string) string

In `api/discovery/extractor.go`, define:

    type ExtractedArticle struct {
        Title string
        Text string
        WordCount int
        Description string
        CanonicalURL string
    }

    func ExtractArticle(rawURL string, body []byte) (*ExtractedArticle, error)

In `api/common/recommendation.go`, define:

    func EnrichDiscoveryCandidate(ctx context.Context, candidateID uint, provider recommendation.EnrichmentProvider) (*DiscoveryCandidate, error)

In `api/recommendation/rule_provider.go`, define:

    type RuleBasedEnrichmentProvider struct {
        PromptVersion string
    }

In `api/common/recommendation.go`, define:

    func GenerateDailyRecommendations(ctx context.Context, userID uint, date string) (*RecommendationDaySnapshot, error)
    func GenerateDailyRecommendationsWithReranker(ctx context.Context, userID uint, date string, reranker recommendation.RerankProvider) (*RecommendationDaySnapshot, error)
    func EnrichPendingDiscoveryCandidates(ctx context.Context, limit int, provider recommendation.EnrichmentProvider) (int, error)
    func RebuildUserRecommendationProfile(userID uint) (*UserRecommendationProfile, error)

In `api/recommendation/openai_provider.go`, define:

    type OpenAICompatibleProvider struct { ... }

In `api/common/recommendation_vector.go`, define:

    func EmbedDiscoveryCandidate(ctx context.Context, candidateID uint, provider recommendation.EmbeddingProvider, model string) error
    func EmbedReadyDiscoveryCandidates(ctx context.Context, limit int, provider recommendation.EmbeddingProvider, model string) (int, error)
    func StoreCandidateEmbedding(ctx context.Context, candidateID uint, model string, vector []float32) error

In `api/common/recommendation_jobs.go`, define:

    type GenerateDailyRecommendationArgs struct { ... }
    type GenerateDailyRecommendationWorker struct { ... }
    func StartRecommendationJobQueue(ctx context.Context) (func(), error)
    func EnqueueDailyRecommendation(ctx context.Context, userID uint, date string) (bool, error)

## Revision Notes

2026-06-28: Created the implementation ExecPlan from the combined user plan and repository audit. The first milestone is deliberately limited to foundation work because the complete feature spans multiple independently verifiable changes.

2026-06-28: Revised the plan after the user requested Go 1.26 and dependency updates. The first milestone now includes dependency upgrade work and Meilisearch SDK adaptation.

2026-06-28: Added the collection-layer milestone. Feed parsing now uses `gofeed`, site discovery uses Colly, and article extraction/normalization helpers are available for later enrichment and embedding jobs.

2026-06-28: Added the enrichment data-path milestone. Candidate enrichment is provider-driven and currently has a deterministic rule-based fallback for tests and local operation.

2026-06-28: Added the deterministic daily generation milestone. The manual generate API now writes recommendation items and preserves generated snapshots instead of only registering empty days.

2026-06-28: Added the reranker boundary milestone. The generator can now accept a reranker provider, validate its output, persist rerank metadata, and fall back when reranking fails.

2026-06-28: Added scheduler/provider/UI milestones. Daily generation now enriches pending candidates first, can use an OpenAI-compatible provider when configured, runs from a settings-aware in-process scheduler, and has a browser-verified recommendation center UI.

2026-06-28: Completed the infrastructure hardening milestone. PostgreSQL installs now run River migrations and can use durable daily recommendation jobs; pgvector embeddings can be written and used for vector-boosted recall while SQLite tests retain deterministic fallback behavior.
