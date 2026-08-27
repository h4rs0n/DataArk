# Add content discovery and recommendations

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds. This document follows `PLANS.md` from the repository root.

## Purpose / Big Picture

After this change, DataArk users can see which archived pages are opened most often, get search keyword suggestions from prior successful searches, browse recommended archived pages, and discover new blog articles from configured RSS or same-site sources before manually adding them to the archive. The behavior is visible through a new recommendation center in the Vue UI and through authenticated API endpoints under `/api`.

## Progress

- [x] (2026-06-20T00:00:00Z) Confirmed product decisions: global rather than per-user preferences, behavior plus content recommendation signals, RSS plus bounded same-site discovery, and manual candidate-to-archive approval.
- [x] (2026-06-20T00:00:00Z) Created this ExecPlan before code changes.
- [x] (2026-06-20T00:00:00Z) Added backend database models and persistence helpers for search events, archive clicks, archive document summaries, discovery sources, and candidates.
- [x] (2026-06-20T00:00:00Z) Added backend recommendation, ranking, keyword, and discovery HTTP APIs.
- [x] (2026-06-20T00:00:00Z) Added frontend recommendation center and search keyword suggestions.
- [x] (2026-06-20T00:00:00Z) Validated with backend tests, frontend build, and browser checks.

## Surprises & Discoveries

- Observation: Existing `ArchiveDocument` only stores `domain`, `file_name`, and `source_url`, so archive recommendations need title and summary metadata to be added and backfilled.
  Evidence: `api/common/db.go` defines `ArchiveDocument` without title or summary fields.
- Observation: The existing delete path already contains careful `/archive/{domain}/{file}` path validation.
  Evidence: `api/search/delete.go` has `resolveArchiveDocumentPath`, but it is private to the `search` package.
- Observation: The local WSL environment does not have Docker available, so the full PostgreSQL/Meilisearch/SingleFile stack could not be launched for browser validation.
  Evidence: `docker compose ps` returned `The command 'docker' could not be found in this WSL 2 distro.`
- Observation: Full Docker validation later exposed that empty list endpoints must return `[]` rather than `null`, because the recommendation page reads `.length` on those arrays.
  Evidence: the real Docker page initially logged `TypeError: Cannot read properties of null (reading 'length')`; after changing empty slices and frontend fallback handling, Chrome DevTools reported no console messages.

## Decision Log

- Decision: Use global recommendation signals, not user-specific signals.
  Rationale: The current app is built around a small authenticated archive workflow and has no user preference boundaries in existing archive/search behavior.
  Date/Author: 2026-06-20 / Codex
- Decision: Treat an archived page as clicked only after the HTML viewer successfully loads the archive resource.
  Rationale: This avoids counting abandoned search result title taps where the archive file fails to load.
  Date/Author: 2026-06-20 / Codex
- Decision: Do not embed external candidate article HTML inside DataArk.
  Rationale: External pages can contain untrusted markup and scripts; the first version shows metadata and opens the original URL in a new browser tab.
  Date/Author: 2026-06-20 / Codex
- Decision: Bound same-site discovery to the configured host, shallow discovery, per-run item limits, content type checks, request timeouts, and deduplication.
  Rationale: This provides useful discovery without turning the server into an uncontrolled crawler.
  Date/Author: 2026-06-20 / Codex

## Outcomes & Retrospective

Implemented the first version of global content recommendations and discovery. Backend tests pass with `cd api && go test ./...`, frontend type-check and production build pass with `cd web && npm run build`, and Docker Compose builds and starts the full stack. Chrome DevTools verification rendered the recommendation center against the real Docker API without console messages or failed network requests after the empty-list fix.

## Context and Orientation

DataArk has a Go backend in `api/` and a Vue 3 frontend in `web/`. The backend starts in `api/api/controller.go` through `WebStarter`, initializes PostgreSQL models in `api/common/db.go`, indexes HTML archive content in Meilisearch through `api/search/add.go`, and serves archived files from `/archive`. The frontend has route views under `web/src/views`, a shared search input in `web/src/components/SearchInput/index.vue`, and authenticated routing in `web/src/router/index.ts`.

An archive document is one saved HTML file under the archive root. A discovery source is a configured feed or website that DataArk can check for new article candidates. A candidate is a discovered article that is not yet archived; the user can read it externally, ignore it, or convert it into an existing archive URL task.

## Plan of Work

First, extend backend persistence. Add fields to `ArchiveDocument` for title and summary while preserving existing unique identity by `(domain, file_name)`. Add models for search events, archive click events, discovery sources, and discovery candidates. Add helper functions to record searches and clicks, query keyword suggestions and rankings, backfill archive metadata from files, and manage discovery source and candidate state.

Second, implement recommendation and discovery services. Recommendation should rank existing archived documents with global signals from recent clicks, all-time clicks, recent searches, matching title/summary/domain text, and archive recency. Discovery should parse RSS and Atom using Go XML decoding, discover feeds and links from same-site HTML using `golang.org/x/net/html`, parse sitemap XML, dedupe by URL, and store candidates with status `new`, `read`, `ignored`, or `archived`.

Third, expose authenticated HTTP APIs in `api/api/controller.go`. Existing `/api/search` should record successful searches after Meilisearch returns. New APIs should expose keyword suggestions, click recording, archive rankings, archive recommendations, source management, manual source fetch, candidate listing, and candidate actions.

Fourth, add frontend surfaces. Update the shared search input to fetch keyword suggestions as the user types. Add `web/src/views/RecommendationsView.vue` with tabs for archive click ranking, search keywords, archive recommendations, and content discovery. Add a homepage button and router entry.

Fifth, validate. Add focused Go tests where feasible, run `cd api && go test ./...`, run `cd web && npm run build`, then start the Vite dev server and inspect the recommendation and search pages with Chrome DevTools for console/network/layout issues.

## Concrete Steps

Work from `/home/harson/sideProj/DataArk`. Backend tests are run with:

    cd api && go test ./...

Frontend build is run with:

    cd web && npm run build

The dev UI is run with:

    cd web && npm run dev

## Validation and Acceptance

The change is accepted when `go test ./...` passes in `api`, `npm run build` passes in `web`, the recommendation center route renders in the browser without console errors or failed frontend asset requests, search suggestions can be requested from `/api/search/keywords`, and candidate articles can be fetched from a test RSS or same-site source and manually sent to the existing archive URL task endpoint through the new candidate archive action.

## Idempotence and Recovery

Database migrations use GORM `AutoMigrate`, so adding columns and new tables is repeatable. Discovery fetches upsert candidates by normalized URL and source, so repeated source fetches do not create duplicates. If a candidate archive action is repeated, the backend reuses existing archive URL task idempotency from `search.AddDocURLTask`.

## Artifacts and Notes

Validation evidence:

    cd api && go test ./...
    ok  	DataArk
    ok  	DataArk/api
    ok  	DataArk/assets
    ok  	DataArk/backup
    ok  	DataArk/common
    ok  	DataArk/search

    cd web && npm run build
    > web2@0.0.0 build
    > run-p type-check "build-only {@}" --

Browser screenshots were saved to `/tmp/dataark-recommendations.png` and `/tmp/dataark-search-suggestions.png` during Chrome DevTools verification.

Docker validation evidence:

    cd docker && docker compose up -d --build
    Image dataark-dataarkapi Built
    Container dataark-dataarkapi-1 Started

    GET /api/search/keywords?prefix=unlikely-empty-keyword&limit=5
    {"Data":[],"Message":"查询搜索关键词成功","Status":"1"}

    GET /api/archive/rankings?window=7d&limit=5
    {"Data":[],"Message":"查询点击排行成功","Status":"1"}

    POST /api/discovery/sources/:id/fetch
    {"Data":{"sourceId":1,"discovered":1,"stored":1,"feedsFound":1,"linksFound":0,"sourceError":""},"Message":"刷新内容源成功","Status":"1"}

Final real-stack browser screenshot was saved to `/tmp/dataark-docker-recommendations-clean.png`.

## Interfaces and Dependencies

No external search or crawler API is introduced. RSS, Atom, and sitemap parsing use `encoding/xml`; same-site HTML discovery uses the existing `golang.org/x/net/html` dependency. New authenticated endpoints:

- `GET /api/search/keywords?prefix=&window=7d|all&limit=10`
- `POST /api/archive/clicks`
- `GET /api/archive/rankings?window=7d|all&limit=20`
- `GET /api/recommendations/archives?window=7d&limit=20`
- `GET /api/discovery/sources`
- `POST /api/discovery/sources`
- `PUT /api/discovery/sources/:id`
- `DELETE /api/discovery/sources/:id`
- `POST /api/discovery/sources/:id/fetch`
- `GET /api/discovery/candidates?status=new|read|ignored|archived`
- `POST /api/discovery/candidates/:id/read`
- `POST /api/discovery/candidates/:id/archive`
- `POST /api/discovery/candidates/:id/ignore`
