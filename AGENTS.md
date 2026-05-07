# Basic principles
**You exist to turn the user's intent into reality.** This is the single principle. Everything below is a facet of it.

## Understand intent

Never confuse the words with the goal. Seek the underlying need, not the literal request. If the domain is unfamiliar, research before acting — wrong understanding produces wrong outcomes regardless of execution quality.

## Stay available

The channel between intent and execution must remain open. Default to delegating work to background workers. Fall back to direct execution when delegation fails — don't ask permission to switch, just deliver. Every retry on a dead path is time wasted; when an approach fails, switch.

## Execute faithfully

Speed without quality is waste. Intent understood and channel open, the last mile is execution that is:

- **Consistent** — tells one coherent story with the rest of the codebase
- **Complete** — traces all downstream dependencies; if A changes, everything referencing A is accounted for
- **Verified** — never report completion without independent evidence; if it can't be proven, it didn't happen

Specific conventions (git workflow, naming, code style) are in [CONVENTIONS.md](CONVENTIONS.md).

# Repository Guidelines

## Project Structure & Module Organization

DataArk is a monorepo with a Go backend and Vue frontend. Backend code lives in `api/`: `api/api/` contains Gin controllers and middleware, `api/common/` holds config, auth, database, and HTML helpers, `api/search/` wraps Meilisearch, and `api/assets/` contains embedded web assets. Frontend code lives in `web/`: `src/views/` for route pages, `src/components/` for reusable Vue components, `src/router/` for routing, and `src/assets/` for styles and images. Deployment files are in `docker/`; Documentation files such as design documents are in `docs/`; GitHub Actions are in `.github/workflows/`.

## Documentation Map

`docs/`: Root folder for documentation.
  - `design-docs/`: Contains system design and architectural documentation (e.g., `dbDesign.md`).
  - `exec-plans/`: Contains execution plans for significant features and refactors.

## Build, Test, and Development Commands

- `make all`: build frontend, move `web/dist` into `api/assets/web`, then build the Go server.
- `make web`: install frontend dependencies and run the production Vue/Vite build.
- `make web2api`: replace embedded web assets with the latest frontend build output.
- `make api`: run `go mod tidy` and build `bin/EchoArkServer`.
- `make clean`: remove `bin/` and `web/dist/`.
- `cd web && npm run dev`: start the local Vite dev server.
- `cd web && npm run build`: type-check and build the frontend.
- `cd api && go run main.go -debug -mkey <key>`: run the API locally; provide database and Meilisearch flags as needed.
- `cd docker && docker compose up -d --build`: run the full stack.

## Coding Style & Naming Conventions

Format Go with `gofmt`; keep packages lower-case and imports using the module path, such as `DataArk/common`. Vue files should use Vue 3 Composition API with `<script setup lang="ts">` when practical. Name route views as `*View.vue`, reusable components by feature under `web/src/components/`, and CSS classes in kebab-case.
For non-trivial backend or frontend changes, add concise comments at key decision points to explain why the code is written that way; prefer decision rationale over line-by-line narration, and do not add comments for obvious operations.

## Testing Guidelines

There is no committed test suite yet. For backend changes, add Go tests as `*_test.go` near the package under test and run `cd api && go test ./...`. For frontend changes, at minimum run `cd web && npm run build`; add component tests if introducing a test framework.

## Commit & Pull Request Guidelines
Recent commits use prefixes such as `Add:`, `Fix:`, `Change:`, and `Repo:` followed by an imperative summary. Keep commits focused and avoid bundling generated files with unrelated source changes. After completing each task, create a git commit that records the work with a clear, descriptive message. Pull requests should describe the behavior change, list verification commands, link issues, and include screenshots for UI updates.

## Security & Configuration Tips

Do not commit real Meilisearch keys, database passwords, archive data, or local Docker volumes. The backend is configured through CLI flags in `api/common/flag.go`; document new required flags in `README_en.md`, Docker compose, and release workflows.

## Frontend debugging rule

When modifying frontend UI code:
1. Start or reuse the local dev server.
2. Use chrome-devtools MCP to open the affected page.
3. Take a screenshot before and after changes.
4. Check console errors and failed network requests.
5. Inspect DOM snapshot for layout or rendering issues.
6. Do not claim the UI is fixed unless it has been verified in the browser.

# ExecPlans

When writing complex features or significant refactors, use an ExecPlan (as described in PLANS.md) from design to implementation.