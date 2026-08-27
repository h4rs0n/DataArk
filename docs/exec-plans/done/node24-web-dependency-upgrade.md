# Upgrade the web toolchain to Node.js 24

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds. This document is maintained in accordance with `PLANS.md` in the repository root.

## Purpose / Big Picture

The DataArk frontend currently builds in a Node.js 20 container with a Vite 5 and TypeScript 5.4 toolchain. After this change, local contributors, the release workflow, and the Docker production build will all use Node.js 24, and the direct web dependencies will be upgraded to current releases whose declared runtime constraints include Node.js 24. The upgrade is observable by checking the declared runtime files, installing with `npm ci`, running the frontend tests and production build, and rebuilding the complete Docker Compose application.

## Progress

- [x] (2026-07-20 21:41+08:00) Read repository instructions and inspected the existing Node, npm, TypeScript, Vite, Docker, CI, dependency, and lockfile configuration.
- [x] (2026-07-20 21:42+08:00) Queried the npm registry for current releases and their Node and peer-dependency constraints.
- [x] (2026-07-20 21:43+08:00) Established a passing Node.js 24 baseline with the existing frontend tests and production build.
- [x] (2026-07-20 21:44+08:00) Declared Node.js 24 consistently for local development, npm, Docker, CI, and TypeScript configuration.
- [x] (2026-07-20 21:45+08:00) Upgraded the direct frontend dependencies, removed the unused webpack-only loader, and regenerated a reproducible npm lockfile.
- [x] (2026-07-20 21:47+08:00) Selected TypeScript 6 after proving the TypeScript 7 incompatibility, removed deprecated `baseUrl`, and enabled JavaScript component resolution.
- [x] (2026-07-20 21:53+08:00) Passed clean-install, frontend test, type-check/build, dependency-tree, zero-vulnerability audit, full backend regression, and Docker Compose integration validation.
- [x] (2026-07-20 21:53+08:00) Verified the production frontend with Chrome DevTools at desktop and narrow widths.
- [x] (2026-07-20 21:55+08:00) Created the single focused `Change: upgrade web toolchain to Node 24` commit without staging unrelated worktree files.

## Surprises & Discoveries

- Observation: the current host already runs Node.js 24.15.0 and npm 11.12.1, so the pre-upgrade frontend can be tested directly under the target runtime.
  Evidence: `node --version` printed `v24.15.0`, `npm --version` printed `11.12.1`, and both `npm test` and `npm run build` exited successfully before dependency changes.
- Observation: runtime selection is currently split across Docker and release CI, while the web package has no `engines`, `packageManager`, `.nvmrc`, or Node 24 TypeScript base configuration.
  Evidence: `docker/Dockerfile` and `.github/workflows/build-release.yml` both select Node 20, and `web/tsconfig.node.json` extends `@tsconfig/node20`.
- Observation: upgrading every direct dependency reaches several major versions at once.
  Evidence: registry metadata reports Vite 8.1.5, TypeScript 7.0.2, Pinia 4.0.2, Vue Router 5.2.0, Vue TypeScript 3.3.7, and npm-run-all2 9.0.2, compared with Vite 5, TypeScript 5.4, Pinia 2, Vue Router 4, Vue TypeScript 2, and npm-run-all2 6 in the lockfile.
- Observation: the published `vue-tsc` peer range is broader than its actual TypeScript compatibility.
  Evidence: `vue-tsc` 3.3.7 declares `typescript >=5.0.0`, but with TypeScript 7.0.2 the build fails before source checking with `ERR_PACKAGE_PATH_NOT_EXPORTED` for `typescript/lib/tsc`.
- Observation: TypeScript 6 turns the deprecated `baseUrl` compiler option into a build-blocking diagnostic when used without an explicit deprecation suppression.
  Evidence: the first TypeScript 6 type-check reported `TS5101` in `web/tsconfig.app.json`; DataArk's `paths` values are already relative to that configuration file and do not require `baseUrl`.
- Observation: the stricter upgraded Vue/TypeScript resolver no longer treats the one JavaScript Vue single-file component as an implicitly typed module.
  Evidence: after removing `baseUrl`, `vue-tsc` reported `TS7016` only for `web/src/views/HtmlView.vue`; every other view already uses TypeScript.
- Observation: the repository had no `.dockerignore`, so a clean Docker context traversal included local dependencies, generated frontend files, credentials, and container-owned runtime volumes.
  Evidence: after restructuring the frontend stage, BuildKit failed while opening `docker/archive/Temporary` with `permission denied`; the unfiltered context had previously transferred about 143 MB even though the final filtered context needs only repository source.
- Observation: filtering Docker inputs reduced the build context by more than fifty times.
  Evidence: the successful final Compose build transferred 2.60 MB instead of the earlier 142.77 MB, then ran `npm ci`, audited 224 packages with zero vulnerabilities, and built 1,253 modules with Vite 8.1.5.
- Observation: Chrome reports one non-runtime issue for four recommendation-setting fields without `id` or `name` attributes.
  Evidence: DevTools classified it as an autofill `issue`, not an error or warning; the affected unchanged fields are preferred topics, languages, article length, and explicit sources. There were no JavaScript runtime errors or warnings from the upgrade.

## Decision Log

- Decision: use the Node.js 24 release line everywhere and declare a minimum of 24.15.0 in `web/package.json`.
  Rationale: the latest npm-run-all2 release explicitly supports `^24.15.0`, and matching the narrowest direct dependency constraint prevents an install from appearing supported on an older Node 24 patch that the selected toolchain rejects.
  Date/Author: 2026-07-20 / Codex
- Decision: use Node 24 type definitions rather than the registry's latest Node type major.
  Rationale: compile-time Node APIs should match the runtime target; `@types/node` 26 describes APIs that do not necessarily exist in Node 24.
  Date/Author: 2026-07-20 / Codex
- Decision: remove `less-loader` while retaining `less`.
  Rationale: DataArk uses Vite's built-in Less integration and does not use webpack. `less-loader` is a webpack-only adapter, so carrying its new webpack peer dependency does not contribute to the production or development build.
  Date/Author: 2026-07-20 / Codex
- Decision: select TypeScript 6.0.3 instead of the registry-latest TypeScript 7.0.2.
  Rationale: the latest stable `vue-tsc` cannot load TypeScript 7's compiler entry point. TypeScript 6 remains a substantial supported upgrade from 5.4 and is the newest stable compiler line that can be validated with the current Vue type-checker.
  Date/Author: 2026-07-20 / Codex
- Decision: remove the deprecated `baseUrl` rather than suppress TypeScript 6's warning.
  Rationale: TypeScript resolves `paths` entries relative to `tsconfig.app.json` without `baseUrl`, so deletion preserves the `@/*` alias and avoids carrying configuration that TypeScript 7 will stop honoring.
  Date/Author: 2026-07-20 / Codex
- Decision: enable `allowJs` for the application TypeScript project.
  Rationale: `HtmlView.vue` is intentionally still a JavaScript component. Allowing JavaScript modules lets the upgraded resolver include it without adding a global `*.vue` any-type shim that would weaken type checking for every TypeScript component.
  Date/Author: 2026-07-20 / Codex
- Decision: make the Docker frontend stage install with `npm ci` from the lockfile before copying frontend source files.
  Rationale: the former `make web` path ran `npm i` after copying the entire `web` directory, including any host `node_modules`. A clean lockfile install is the same path used in release CI, cannot rewrite dependency selections during a production build, improves Docker layer reuse, and ensures host modules cannot mask Node 24 incompatibilities.
  Date/Author: 2026-07-20 / Codex
- Decision: add a root `.dockerignore` for Git/Codex metadata, local credentials, build output, web dependencies, and Docker service data.
  Rationale: none of these files are inputs to `docker/Dockerfile`; excluding them fixes container-owned volume traversal, prevents host `node_modules` from entering a Node 24 build, reduces context size, and keeps local data and credentials out of BuildKit.
  Date/Author: 2026-07-20 / Codex

## Outcomes & Retrospective

Node.js 24 is now the single frontend runtime line for local version managers, npm engine enforcement, the release workflow, and the production Docker builder. The application toolchain moved from Vite 5 to 8, Vue 3.4 to 3.5, Vue Router 4 to 5, Pinia 2 to 4, Vue TypeScript 2 to 3, and TypeScript 5.4 to 6. Other direct dependencies were upgraded to their current compatible releases, while the Node declarations intentionally remain on major 24. The unused webpack `less-loader` was removed.

TypeScript 7 was the only registry-latest direct release rejected: the latest `vue-tsc` still loads an internal compiler entry point that TypeScript 7 no longer exports. TypeScript 6.0.3 is therefore the newest combination that passes the real Vue type-check. Its stricter diagnostics also removed deprecated `baseUrl` configuration and required explicit inclusion of the one JavaScript Vue component.

Validation passed under local Node 24.15.0 and npm 11.12.1: a clean `npm ci`, the complete dependency tree, both Node test files, standalone type-check, standalone Vite build, combined production build, and npm audit. The audit found zero vulnerabilities across 254 dependency entries, and the full Go suite passed. The final Docker build used `node:24`, performed a clean `npm ci`, and produced the same Vite assets from a 2.60 MB filtered context; all four Compose services are running and PostgreSQL is healthy.

Chrome DevTools reloaded the production recommendation route and exercised the task-queue tab. The Node 24/Vite 8 assets and all 53 observed document, script, stylesheet, fetch, and XHR requests returned HTTP 200. The tab rendered live queue data with no horizontal overflow at 760 pixels and no runtime console errors or warnings. One existing autofill issue remains on four unchanged settings fields that lack `id` or `name`; it is outside this dependency-only change.

The completed upgrade was committed as `Change: upgrade web toolchain to Node 24`. The user's unrelated Compose configuration, Go workspace sum, makefile, Codex metadata, and credential file remain uncommitted and untouched by the focused commit.

## Context and Orientation

The Vue frontend lives in `web/`. `web/package.json` contains its direct runtime and build dependencies, while `web/package-lock.json` pins the complete dependency graph used by `npm ci`. `web/tsconfig.node.json` selects the TypeScript rules and Node API types used to check `web/vite.config.ts`.

The production container is a multi-stage build in `docker/Dockerfile`; its first stage compiles the frontend before the Go backend embeds the generated files. The standalone release workflow in `.github/workflows/build-release.yml` performs the same frontend build on a GitHub Actions runner. A version declaration in only one of these places would leave contributors, CI, and production using different runtimes, so the upgrade must update all of them together. A `.nvmrc` file in `web/` gives local Node version managers an explicit target, while `engines` and `packageManager` in `web/package.json` make npm diagnose incompatible runtimes.

## Plan of Work

First, add the Node.js 24 local and package declarations. Update the Docker frontend builder and release workflow from Node 20 to Node 24. Replace the Node 20 TypeScript base with `@tsconfig/node24` and Node 24 declarations. Make the Docker frontend stage use `npm ci` from copied manifest files before copying only the source and configuration needed for the production build.

Second, upgrade all direct packages to current stable releases verified through npm registry metadata. Regenerate `web/package-lock.json` under Node.js 24 and npm 11. Remove the unused webpack-only `less-loader`. Inspect npm's resolved dependency tree for invalid peer dependencies and run the clean install path used by CI.

Third, run tests and the combined Vue type-check plus Vite production build. If new compiler or library versions expose source incompatibilities, make the smallest source changes necessary and record them here. Run the full Go suite because the generated frontend is embedded by the backend.

Finally, exclude local modules, generated output, credentials, and Compose runtime volumes from the Docker build context, then rebuild the user's existing Compose project so the frontend is compiled in the Node 24 Docker stage. Open the production page at `http://127.0.0.1:7845` with Chrome DevTools, inspect rendered DOM, console messages, failed network requests, and a screenshot. Preserve the user's unrelated changes to `docker/docker-compose.yml`, `go.work.sum`, `makefile`, `.codex`, and `passwd.txt`, and commit only the focused runtime and dependency upgrade.

## Concrete Steps

From `web/`, inspect and update the dependency graph, then validate it with:

    node --version
    npm --version
    npm ci
    npm ls --all
    npm test
    npm run build
    npm audit

The Node version must be 24.15.0 or newer within major 24, npm must be major 11, `npm ls` must report no invalid dependency edges, both test files must pass, and the Vue production build must exit successfully.

From `api/`, run:

    env GOCACHE=/tmp/dataark-go-cache go test ./...

From the repository root, rebuild the existing application with:

    docker compose -f docker/docker-compose.yml -p dataark up -d --build

Inspect the build transcript for a Node 24 frontend base, confirm the services are running, and load the production web page through Chrome DevTools.

## Validation and Acceptance

The change is accepted when `web/.nvmrc`, `web/package.json`, `web/tsconfig.node.json`, `docker/Dockerfile`, and `.github/workflows/build-release.yml` all agree on Node.js 24; a fresh `npm ci` succeeds under the declared runtime; the dependency tree has no invalid peer edges; frontend tests and production compilation pass; and the complete Go test suite remains green.

The Docker image must build the frontend from a Node 24 base and start successfully with the existing PostgreSQL, Meilisearch, and SingleFile services. In Chrome, the production frontend must render after the rebuilt container, frontend asset/API requests must not fail unexpectedly, and the console must contain no runtime errors caused by the dependency upgrade.

## Idempotence and Recovery

Package installation and lockfile generation are repeatable from `web/package.json`. If a major dependency proves incompatible, inspect its published peer constraints and either repair the small DataArk API usage or select the newest compatible release; record the choice in this plan rather than bypassing peer validation. Docker Compose rebuilds are safe to repeat and retain the user's existing service volumes. Do not delete or recreate database data.

## Artifacts and Notes

The worktree began with unrelated modifications in `docker/docker-compose.yml`, `go.work.sum`, and `makefile`, plus untracked `.codex` and `passwd.txt`. These files are outside the dependency-upgrade patch and must not be staged, overwritten, or disclosed.

Registry inspection on 2026-07-20 found Vite 8.1.5 with Node constraint `^20.19.0 || >=22.12.0`, Vue 3.5.40, Vue Router 5.2.0, Pinia 4.0.2, TypeScript 7.0.2, and npm-run-all2 9.0.2 with Node constraint `^22.22.2 || ^24.15.0 || >=26.0.0`. TypeScript 7 was tested and rejected because the latest Vue type-checker cannot load it; the selected compiler is TypeScript 6.0.3.

Chrome screenshots are saved at `/tmp/dataark-node24-web-narrow.png`, `/tmp/dataark-node24-web-desktop.png`, and `/tmp/dataark-node24-web-final.png`. The final Docker image ID is `ad4889b987bf5a00179370ed01704b154c07765f4ab88c3149ac2e7edd78a436`.

## Interfaces and Dependencies

No application API or backend interface changes are intended. `web/package.json` will expose `engines.node` for the supported Node 24 range, `engines.npm` for npm 11, and `packageManager` for the exact lockfile-generating npm release. `web/tsconfig.node.json` will extend `@tsconfig/node24/tsconfig.json`. The application continues to use Vue, Vue Router, Pinia, Axios, Arco Design, Less, Vite, Vue TypeScript, and the existing Node test runner.

Revision note (2026-07-20): created this plan after auditing every runtime declaration and querying current npm package metadata; the plan records the clean Node 24 baseline, the expected major upgrades, and the validation needed to prove the Docker production frontend still works. Updated at 21:47+08:00 with the TypeScript 7 incompatibility and required TypeScript 6 configuration changes. Updated at 21:53+08:00 with clean-install, dependency-tree, audit, Go, Docker, Chrome, screenshot, network, console, and responsive-layout evidence, plus the Docker context filtering discovery. Updated at 21:55+08:00 with the final focused commit status.
