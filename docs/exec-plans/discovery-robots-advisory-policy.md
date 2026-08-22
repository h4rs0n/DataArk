# Make robots.txt advisory for manually triggered discovery

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds. This document follows `PLANS.md` in the repository root and supersedes earlier statements in `docs/exec-plans/blogroll-article-recommendation-v3.md` that described `Allow` and `Disallow` as crawl gates.

## Purpose / Big Picture

After this change, an owner who presses “Run pending tasks” can fetch a page, feed, Sitemap, or article even when the target site's `robots.txt` contains a matching `Disallow` rule. DataArk still downloads and caches `robots.txt` as advisory discovery metadata, extracts its `Sitemap` declarations as path hints, and records whether the file was available. Discovery HTTP requests identify as the configured Chrome 150 user agent. Domain-blacklist, SSRF, redirect, response-size, content-type, timeout, and per-host rate controls remain enforced.

## Progress

- [x] (2026-08-03 13:20+08:00) Inspected the shared fetch boundary, robots cache, candidate processing, flag defaults, Docker configuration, tests, and the earlier Sitemap safety policy.
- [x] (2026-08-03 13:45+08:00) Converted robots handling from an enforcement checker into an advisory inspector and retained cached Sitemap hints.
- [x] (2026-08-03 13:50+08:00) Changed the default and Docker-configured user agent to the requested Chrome 150 value.
- [x] (2026-08-03 14:00+08:00) Added focused regression coverage and updated Chinese and English configuration documentation.
- [x] (2026-08-03 14:12+08:00) Ran full backend tests and Docker Compose validation; confirmed the exact interpolated Chrome UA.
- [x] (2026-08-03 14:18+08:00) Completed final staged diff review with unrelated worktree files excluded and prepared the focused commit.

## Surprises & Discoveries

- Observation: the previous robots component failed closed, so a missing loader, network error, malformed file, or non-200/404/410 response prevented the actual discovery request.
  Evidence: the old `RobotsCache.Allowed` returned `ErrRobotsUnavailable`, and `HTTPClientFetcher.Fetch` returned that error before creating the target request.
- Observation: automatic Sitemap crawling is intentionally dormant and owner Sitemap gap fill is a separate policy.
  Evidence: `api/discovery/endpoint_discovery.go` rejects Sitemap sources and `api/discovery/site_admin.go` permits same-domain Sitemap gap fill only for a manually managed seed. This change retains that boundary; parsing a `Sitemap` declaration does not automatically activate it.

## Decision Log

- Decision: treat every robots outcome as advisory, including an explicit matching `Disallow`, parse error, HTTP failure, or network failure.
  Rationale: the owner explicitly starts network execution, so robots availability must not become an implicit authorization or availability gate.
  Date/Author: 2026-08-03 / Codex
- Decision: retain and cache only `Sitemap` declarations as useful path hints; never turn `Allow` or `Disallow` values into candidate URLs.
  Rationale: exclusion rules commonly expose administrative, search, or private-looking paths and are unsafe discovery inputs, while `Sitemap` is the robots directive intended to advertise crawlable URL indexes.
  Date/Author: 2026-08-03 / Codex
- Decision: preserve the existing explicit owner Sitemap gap-fill policy.
  Rationale: changing Sitemap activation and inventory volume is a separate product decision from removing robots exclusion enforcement.
  Date/Author: 2026-08-03 / Codex

## Outcomes & Retrospective

The shared discovery boundary now treats robots exclusions and failures as advisory, preserves cached Sitemap hints, and emits the requested Chrome 150 UA by default and through Docker. Focused and full backend tests pass, and Compose resolves the exact UA successfully. No database migration or frontend change was necessary. The focused change is ready to commit with unrelated local files excluded.

## Context and Orientation

All automatic discovery network work is staged in a queue and starts only after an owner action in the task-queue interface. `api/discovery/boundaries.go` is the common HTTP boundary used by homepage, Feed, explicit Sitemap, blogroll, archive-page, and article-body fetches. `api/discovery/robots.go` downloads one `robots.txt` per origin and caches the result. `api/config/config.go` and `api/flag/flag.go` define the runtime user-agent default. `docker/docker-compose.yml` maps values from the ignored `docker/.env`; `docker/.env.example` documents safe defaults.

The term “advisory” means the robots file can supply metadata but cannot allow, deny, delay, or fail a target request. The domain blacklist remains a mandatory block and is unrelated to robots policy. The ignored `docker/.env` is updated locally for immediate deployment but must never be committed because it contains real credentials.

## Plan of Work

In `api/discovery/robots.go`, replace the URL permission result with a cached `RobotsInspection` containing a status and copied Sitemap hints. Return `available`, `missing`, `invalid`, or `unavailable` without propagating a blocking error. In `api/discovery/boundaries.go`, record that status and always continue to the normal safety validator and target request.

Define the requested browser string once in `api/config/config.go`; reuse it in the command-line flag and the fetcher's empty-value fallback. Pass `DISCOVERY_USER_AGENT` through Docker Compose, document it in `.env.example`, and apply it to the ignored local `.env`. Update both READMEs to explain the advisory policy and the independent explicit Sitemap policy.

Revise the robots fetch test so a `Disallow: /private` document is fetched once, its Sitemap is retained, the private article is requested, and the outgoing user agent is exact. Add a failure-path test proving an unavailable robots endpoint still permits the article request.

## Concrete Steps

From `api/`, format and run focused tests:

    gofmt -w config/config.go flag/flag.go discovery/boundaries.go discovery/robots.go discovery/fetch_test.go flag/flag_test.go
    env GOCACHE=/tmp/dataark-go-cache go test ./discovery ./flag -count=1

From the repository root, validate all Go packages and Compose interpolation:

    cd api && env GOCACHE=/tmp/dataark-go-cache go test ./... -count=1
    cd ../docker && docker compose config --quiet
    cd .. && git diff --check

Expect all tests to print `ok`, Compose validation to exit zero without output, and `git diff --check` to exit zero.

## Validation and Acceptance

The regression test `TestRobotsRulesAreAdvisoryAndExposeSitemapHints` must demonstrate that a server receives both `/robots.txt` and `/private/article` even though the former disallows `/private`; the article request must carry the exact Chrome 150 user agent, and a second robots inspection must use the cache while returning the declared Sitemap. `TestRobotsUnavailableDoesNotBlockManualDiscoveryRequest` must demonstrate that HTTP 503 from `/robots.txt` records `unavailable` but still reaches `/article`.

Full acceptance requires all backend packages to pass, Docker Compose to accept the new variable with spaces and punctuation, no whitespace errors, and a commit containing only files related to this policy. Existing unrelated changes to `go.work.sum`, `makefile`, `web/public/favicon.ico`, `.codex`, and `passwd.txt` must remain unstaged.

## Idempotence and Recovery

Formatting and tests are repeatable. The robots cache is in memory and expires normally, so no database migration or data rewrite is needed. Rolling back the focused commit restores exclusion enforcement. The local `.env` is ignored and can be restored manually to a different UA without affecting Git history.

## Artifacts and Notes

Focused validation after the first implementation pass:

    ok DataArk/discovery
    ok DataArk/flag

The sandboxed first run failed only because `httptest` could not bind a loopback socket; the same command passed when local test sockets were permitted.

Full validation:

    ok DataArk/discovery 0.730s
    ok DataArk/recommendation 0.765s
    ok DataArk/jobqueue 3.043s
    # all remaining Go packages also passed
    docker compose config --quiet  # exit 0
    -discover-ua
    Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36

## Interfaces and Dependencies

`api/discovery/robots.go` defines:

    type RobotsInspection struct {
        Status   string
        Sitemaps []string
    }

    func (cache *RobotsCache) Inspect(ctx context.Context, rawURL string) RobotsInspection

`api/discovery/boundaries.go` defines `RobotsInspector` with that method and stores `Inspection.Status` in `FetchResult.RobotsStatus`. The existing `github.com/temoto/robotstxt` dependency remains because it parses Sitemap declarations reliably; its group matching and crawl-delay values are intentionally not consulted.

Revision note (2026-08-03): created this focused plan because owner-triggered discovery changes the robots authorization model while the existing large discovery plan retains historical milestone descriptions of the former enforcement behavior.

Revision note (2026-08-03 14:12+08:00): recorded full Go and Docker Compose validation after implementation completed.

Revision note (2026-08-03 14:18+08:00): recorded the final staged review and completion state before creating the repository commit.
