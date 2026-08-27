# Repository Guidelines

DataArk is a Go API plus Vue 3 UI monorepo. This file is the current source of truth for agents. If another document disagrees (especially anything under `docs/exec-plans/done/` that still mentions `api/common/`), follow this file and the live tree.

There is **no** `api/common/` package. Flags live in `api/flag/flag.go` and write package-level vars in `api/config/config.go`. Startup wiring is `api/bootstrap`. HTTP lives in `api/api/` as same-package files: `starter.go` (`WebStarter`), `routes.go`, `deps.go`, `jobs.go`, `helpers.go`, `middleware.go`, plus domain handlers `auth.go`, `search.go`, `archive.go`, `discovery.go`, `assessment.go`, `recommendation.go`, `backup.go`.

## Project Structure

Backend (`api/`, Go module `DataArk`, Go 1.26):

- `api/api/` — Gin HTTP surface: `starter.go` (`WebStarter`, listens on `0.0.0.0:7845`), `routes.go` (`/api/*`), `deps.go` (test seams), `jobs.go`, `helpers.go`, `middleware.go`, and domain handlers (`auth.go`, `search.go`, `archive.go`, `discovery.go`, `assessment.go`, `recommendation.go`, `backup.go`)
- `api/archive/` — archived HTML metadata, stats, engagement
- `api/auth/` — users, JWT
- `api/discovery/` — crawl, feeds, blogroll graph, candidate pipeline; hands off extracted articles as `assessment_state=pending`. Uses `DiscoveryJobEnqueuer` (fetch/blogroll/backfill/process only). Assessment enqueue happens in `api/api/jobs.go` after process succeeds.
- `api/discovery/articlerules/` — URL/body hard gates
- `api/assessment/` — LLM article assessment adapter, immutable assessment rows, state machine, manual queue, backfill/rollback, metrics. Chat LLM I/O is `ChatAssessmentInput`/`ChatAssessmentResult`; the domain assessor uses `ArticleAssessmentInput`/`ArticleAssessmentResult`. Do not alias those names in other packages. Queue surface is `ArticleJobEnqueuer` (`EnqueueAssessArticle` only).
- `api/assessmenteval/` — owner gold-label workflow
- `api/articlevalue/` — shared scoring/evidence helpers
- `api/recommendation/` — daily digest (`digest.go`, `day.go`, `selection_v3.go`), settings, feedback, profile, rerank, discovery feed, inventory (`inventory.go`), item context (`item_context.go`), metrics (`metrics.go`). Candidate topics/summary come from assessment write-back, not a separate enrichment hop. Do not revive `_m3`–`_m17` filename suffixes. Daily enqueue surface is `DailyJobEnqueuer` (`EnqueueGenerateDaily` plus `EnqueueGenerateDigestSummary`). Digest summary is pre-generated after publish/supplement; GET `/recommendations/today/summary` only reads the cache.
- `api/jobqueue/` — River (Postgres) and in-memory queue. The full `JobEnqueuer` lives here as the union of the three domain enqueue interfaces. Discovery does not import this package. Digest summary jobs are `recommendation_generate_digest_summary` on the default queue.
- `api/search/` — Meilisearch index
- `api/backup/` — backup/restore
- `api/database/` — GORM connection; Goose runner
- `api/migrations/` — numbered Goose SQL (`000001`–`000029` and later)
- `api/llm/`, `api/logging/`, `api/observability/`, `api/assets/`
- `api/flag/`, `api/config/`

Frontend (`web/src/`): `views/` (`*View.vue`), `components/` (recommendation center panels live in `components/recommendations/`), `api/` (shared `client.ts` + domain helpers), `stores/` (Pinia auth store only; page data stays in views), `router/`, `assets/`. Node `>=24.15 <25`. The npm package name is still `web2`.

Ops: `docker/` (Compose + Dockerfile). Docs: `docs/operations/` runbooks, `docs/references/frontend-pitfalls.md` (Arco API traps), `docs/exec-plans/` (in-progress living plans only; finished plans live in `docs/exec-plans/done/`). `docs/design-docs/dbDesign.md` is stale (still names `api/common/db.go`).

## Build and development commands

There is **no** `make build` target. README that says otherwise is wrong.

| Command | What it actually does |
| --- | --- |
| `make all` | `web` → `web2api` → `api` |
| `make web` | `npm i` and production Vite build. **Side effect:** sets the user-global npm registry to `https://registry.npmmirror.com`. Prefer `cd web && npm ci && npm run build` in sandboxes. |
| `make web2api` | `mv web/dist/*` into `api/assets/web/` |
| `make api` | `go mod tidy` and compile `bin/EchoArkServer` (makefile name). GitHub release workflow names the binary `DataArkServer`. README's `./api/bin/DataArk.exe` is wrong. |
| `cd api && go test ./...` | Default backend suite (SQLite + GORM AutoMigrate as a **test-only** dialect stand-in) |
| `cd web && npm test` | Node test runner on `web/tests/*.test.mjs` (mostly source-string contracts, not DOM) |
| `cd web && npm run build` | `vue-tsc` + Vite production build |
| `cd web && npm run dev` | Vite only. **No proxy** to the API is configured. |
| `cd docker && docker compose up --build` | Full stack: API, Postgres/pgvector, Meilisearch, SingleFile. Site: `http://localhost:${DATAARK_PORT}` (default `7845`). |
| `cd docker && docker compose up -d --build` | Same stack, detached |

`makefile` and `docker/Dockerfile` pin `GOPROXY=https://goproxy.cn`. If module download fails outside that network, set `GOPROXY` yourself (for example `https://proxy.golang.org,direct`) rather than editing the makefile unless the task is to change it.

Optional Postgres proof (Goose, JSONB, River, pgvector): set `DATAARK_POSTGRES_TEST_DSN` and run `cd api && go test ./bootstrap -run TestPostgresV3MigrationsRiverRestartAndPGVector`. SQLite green does not prove production schema.

Discovery/search tests that call `httptest.NewServer` need a local listen. Isolated sandboxes that block `listen tcp` will fail those packages; re-run them outside the sandbox rather than deleting the tests.

## Coding style

Format Go with `gofmt`. Package names are lower-case; imports use the `DataArk/...` path. Production schema changes go **only** in a numbered Goose migration. SQLite tests may `AutoMigrate` models as a dialect stand-in; that does not define production schema. Vue 3 Composition API with `<script setup lang="ts">` where practical. Route pages are `*View.vue`; CSS classes are kebab-case. Check `docs/references/frontend-pitfalls.md` before assuming Arco props match Ant Design Vue.

## Verification (match the change radius)

Stop when the matching row's **Minimum proof** is done. Do not escalate to Docker, Postgres, or MCP "to be safer." Subagents without browser MCP should run the commands that row allows and say they did not do browser verification.

Do not rebuild Docker for a Go unit-test fix. Do not treat `npm test` greps as a substitute for clicking a flow you actually changed.

| Change radius | Minimum proof | Do not |
| --- | --- | --- |
| One Go package, no SQL | `cd api && go test ./thatpkg` | `docker compose` |
| Several Go packages / HTTP | `cd api && go test ./...` | assume Postgres behavior |
| Goose / JSONB / River / pgvector | opt-in Postgres test or Compose logs | trust SQLite AutoMigrate alone |
| Vue copy, layout, non-interactive CSS | `cd web && npm test && npm run build` | rebuild the API image for padding |
| User-visible UI behavior, Arco widgets, authz, embedded `api/assets/web` | From `docker/`: `docker compose up --build` (or `-d --build` if a stack is already needed in the background). Confirm `dataarkapi` is healthy at `http://localhost:${DATAARK_PORT}`. Then Chrome DevTools MCP (`cursor-ide-browser`, including `browser_cdp`): open the changed pages, interact, check DOM, console, network, screenshot. | report UI work complete from `npm test` only |

If Compose is already running in a terminal, do not start a second foreground `up --build` that fights for the same ports. Rebuild that existing stack or use `-d`.

## Git

This table is the git policy. If `PLANS.md` or `CONVENTIONS.md` disagrees, follow this file.

| Situation | Do | Do not |
| --- | --- | --- |
| Coding session / implementing an ExecPlan | Use the plan's Progress section as the checkpoint | `git commit` unless the user asked |
| User asked to commit | One logical commit; message `Add:`, `Fix:`, `Change:`, or `Repo:` plus an imperative summary | `passwd.txt`, `docker/.env`, archives, volumes, Meilisearch keys, database passwords |
| User asked to open a PR | Squash to exactly one commit, rebase onto the target branch, no merge commits | Leave session WIP commits on the PR |

Squash happens when opening a PR, not after each local change. PR shape details also live in `CONVENTIONS.md`.

## Configuration and secrets

CLI flags: `api/flag/flag.go`. Runtime vars: `api/config/config.go`. New required flags must be documented in `README.md`, `README_en.md`, `docker/docker-compose.yml`, and release workflows. LLM flags are optional; without them, assessment/rerank/digest summary stay rule-based.

Do not commit real credentials. `passwd.txt` at the repo root is a local admin-password note and must stay untracked.

## ExecPlans and workers

Follow `CONVENTIONS.md` for delegation: worker outputs are untrusted; verify with an independent command; at most one delegation retry, then do the work directly.

Default to a short design note or a Cursor plan. Write a new ExecPlan under `docs/exec-plans/` following `PLANS.md` only when the user asks for a living spec. Checkpoints go in that plan's Progress section, not git (see Git above).

Active ExecPlans stay in `docs/exec-plans/`. When Progress is fully checked and `Outcomes & Retrospective` is written, **move the file to `docs/exec-plans/done/`** in the same change that marks it complete. Do not leave finished plans in the live directory. **Do not execute anything under `docs/exec-plans/done/` as current work.** Those files are historical; plans that name `api/common/`, sitemap owner gap-fill, or `make build` describe past trees, not today's.

Current recommendation-center split: discovery crawls automatically, LLM assessment is a paused manual queue, recommendation reads ready eligible inventory. Ops detail: `docs/operations/recommendation-v3-runbook.md` and `docs/operations/article-assessment-v3-runbook.md`.
