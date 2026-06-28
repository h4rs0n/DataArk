# Repository Guidelines

## Project Structure & Module Organization

DataArk is a Go backend plus Vue frontend monorepo. Backend code lives in `api/`: handlers in `api/api/`, shared services in `api/common/`, recommendations in `api/recommendation/`, discovery in `api/discovery/`, search in `api/search/`, Goose migrations in `api/migrations/`, and embedded assets in `api/assets/web/`. Frontend code lives in `web/src/`: pages in `views/`, UI in `components/`, routing in `router/`, and styles/assets in `assets/`. Docker files are in `docker/`; design notes and ExecPlans live in `docs/`.

## Build, Test, and Development Commands

- `make all`: build Vue assets, move `web/dist` into `api/assets/web`, then build `bin/EchoArkServer`.
- `make api`: run `go mod tidy` and compile the backend.
- `make web`: install frontend dependencies and run the production Vite build.
- `cd api && go test ./...`: run backend tests.
- `cd web && npm run dev`: start Vite locally.
- `cd web && npm run build`: type-check and build the Vue app.
- `cd docker && docker compose up -d --build`: run Postgres/pgvector, Meilisearch, SingleFile, and the API.

## Coding Style & Naming Conventions

Format Go with `gofmt`; keep package names lower-case and imports on the `DataArk/...` module path. Add database changes as numbered Goose migrations. Vue code should use Vue 3 Composition API with `<script setup lang="ts">` where practical. Name route pages `*View.vue` and CSS classes in kebab-case.

## Testing Guidelines

Place Go tests as `*_test.go` beside the package under test. Backend coverage spans controllers, auth, database behavior, discovery, recommendation, backup, and search; extend the relevant package when behavior changes. Run `cd api && go test ./...` for backend work and `cd web && npm run build` for frontend work. For UI changes, verify screenshots, console/network, and DOM state.

## Commit & Pull Request Guidelines

Recent commits use `Add:`, `Fix:`, `Change:`, or `Repo:` plus an imperative summary. Keep commits focused and exclude unrelated generated files or local data. Use one commit per PR, rebased without merge commits. PRs should describe behavior changes, verification commands, linked issues, and UI screenshots when relevant.

## Security & Configuration Tips

Do not commit real Meilisearch keys, database passwords, archives, Docker volumes, or credential files. Backend flags live in `api/common/flag.go`; document new required flags in `README.md`, `README_en.md`, Docker compose, and release workflows. LLM flags are optional; without them, enrichment is rule-based.

## Agent-Specific Instructions

Follow `CONVENTIONS.md` for delegation and git workflow. For complex work, update an ExecPlan under `docs/exec-plans/` using `PLANS.md`. Prefer existing patterns, verify independently, and commit completed changes.
