# Repository Guidelines

## Project Structure & Module Organization

DataArk combines a Go 1.26 API and Vue 3/TypeScript UI.

- `api/api/`: Gin routes and handlers; `api/bootstrap/`: startup wiring.
- `api/discovery/`, `api/assessment/`, `api/recommendation/`: crawling, manual LLM assessment, and recommendations.
- `api/flag/flag.go` and `api/config/config.go`: flags and runtime configuration. There is no `api/common/` package.
- `api/migrations/`: numbered Goose SQL; `api/assets/web/`: embedded frontend assets.
- `web/src/`: views, components, API helpers, router, and auth store; `web/tests/`: frontend tests.
- `docker/`: deployment; `docs/operations/`: runbooks; `docs/exec-plans/done/`: historical plans, not current work.

## Build, Test, and Development Commands

Use Node `>=24.15 <25` and npm 11.12.1.

- `cd web && npm ci && npm run build`: install dependencies, type-check, and build with Vite.
- `cd web && npm run dev`: start Vite; no API proxy is configured.
- `cd web && npm test`: run Node tests.
- `cd api && go test ./...`: run backend tests.
- `make all`: build frontend, move assets into the API, and compile `bin/EchoArkServer`. There is no `make build`; `make web` changes the user-global npm registry.
- `cd docker && docker compose up -d --build`: run the full stack at `http://localhost:${DATAARK_PORT}` (default `7845`).

## Coding Style & Naming Conventions

Run `gofmt` for Go indentation and formatting; use lowercase packages and `DataArk/...` imports. Match two-space frontend indentation, use Composition API with `<script setup lang="ts">`, name pages `*View.vue`, and use kebab-case CSS classes. Consult `docs/references/frontend-pitfalls.md` before changing Arco widgets. Production schema changes require numbered Goose migrations.

## Testing Guidelines

Place Go tests in `*_test.go` with `Test...` functions; frontend tests use `*.test.mjs`. Test one Go package for isolated changes; run the full suite for cross-package/HTTP changes. Frontend copy/layout changes require tests and build. UI behavior requires Compose health verification and Chrome DevTools interaction, console/network checks, and screenshots by invoke MCP server. Node source-contract tests cannot prove interactions. SQLite tests cannot prove Postgres behavior; migration/River/pgvector changes require the opt-in Postgres integration test or Compose evidence.

## Commit & Pull Request Guidelines

Commit only when requested. Follow history: `Add:`, `Fix:`, `Change:`, or `Repo:` plus an imperative summary. Before opening a PR, squash to one logical commit and rebase onto the target branch; no merge commits. Explain behavior changes and validation; include screenshots for UI changes.

## Security & Configuration

Never commit `passwd.txt`, `docker/.env`, credentials, archives, or volumes. Configure required `-llm-base-url`, `-llm-chat-model`, and `-article-assessment-mode=active`.
