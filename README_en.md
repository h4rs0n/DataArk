# DataArk
<div align="center">
    <img src="images/GitHub_README.png" alt="logo" width="200">
</div>

**DataArk** is an offline storage and data retrieval system designed to save web pages and other data from the Internet that may become inaccessible. Currently, only HTML web page file storage and indexing are supported, with plans to support images, videos, and other file types in the future.
## Middleware
Search Engine: [Meilisearch](https://github.com/meilisearch/meilisearch)

HTML Download: [SingleFile](https://github.com/gildas-lormeau/SingleFile)

## Installation/Deployment
**Using Docker Compose (Recommended)**
```
cd docker
sudo docker compose build
sudo docker compose up -d
```
When starting for the first time, an initial username and password will be generated. Please run the command `sudo docker compose logs` to view the default username and password in the output. This is only output the first time the system is deployed.

**Using `make` to build**
```
make web
make build
```
An executable file will be generated in the `api/bin` directory. After deploying Meilisearch and PostgreSQL with pgvector available, start the service by running:
```
./api/bin/DataArk.exe -loc ./docker/archive \
                      -mhost "http://meili:7700" \
                      -mkey "RandomKey" \
                      -mdump "./docker/meili_dumps" \
                      -dbhost "127.0.0.1" \
                      -dbport "5432" \
                      -dbname "postgres" \
                      -dbuser "postgres" \
                      -dbpasswd "postgres" \
                      -discover-interval "6h" \
                      -discover-timeout "12s" \
                      -discover-max 50 \
                      -recommend-enabled=false \
                      -recommend-daily-limit 10 \
                      -recommend-timezone "Asia/Shanghai" \
                      -recommend-time "07:00" \
                      -llm-base-url "" \
                      -llm-chat-model "" \
                      -llm-embedding-model "" \
```
The backup feature depends on the `pg_dump` and `psql` commands. For manual deployments, install PostgreSQL client tools and point `-mdump` to the shared Meilisearch dump directory configured by `MEILI_DUMP_DIR` or `--dump-dir`.

Content discovery uses a Chrome 150 user agent by default. `robots.txt` is advisory: DataArk extracts Sitemap path hints and records diagnostics, but `Allow`, `Disallow`, and `Crawl-delay` never block an owner-triggered discovery request. Sitemap processing remains subject to its separate explicit owner gap-fill policy.

Content discovery uses the `-discover-interval`, `-discover-timeout`, `-discover-max`, and `-discover-ua` flags. `-discover-interval` controls how often due jobs are staged; set it to `0` to disable periodic staging. Startup recovery, the periodic scheduler, and newly added subscriptions only place work in the manual queue and never start network crawling. An owner must use **Recommendation Center → Content Discovery → Crawl Task Queue** and click **Run pending tasks**. One run drains currently due work and its immediate child jobs, then pauses again; future backoff retries require another manual run. Safe fetching and per-endpoint scheduling can be tuned with `-discover-host-concurrency`, `-discover-min-request-interval`, `-discover-robots-ttl`, `-discover-max-redirects`, `-discover-active-feed-interval`, `-discover-observing-interval`, `-discover-dormant-interval`, `-discover-backoff-base`, and `-discover-backoff-max`. Defaults keep active feeds within 24 hours, observing sites within 7 days, and reachable dormant sites within 30 days, so low historical yield never disables checks by itself. Multiple inbound sites, discovered eligible articles, and explicit positive feedback can only shorten these floors through extra budget; `-discover-schedule-min-interval` bounds that acceleration, and the owner-only site operations API explains each endpoint's base and chosen interval. Blogroll graph expansion defaults to depth 3, 50 activations per source scan, and 100 new observing sites per day; tune these bounds with `-discover-max-graph-depth`, `-discover-max-blogroll-targets`, and `-discover-daily-observing-limit`. Historical coverage processes one page per job and gives every unfinished low-yield source another batch within 7 days; tune these bounds with `-discover-backfill-batch-size` and `-discover-backfill-max-interval`. Article processing requires 120 extracted characters by default and bounds transient fetch attempts at 5; tune these rules with `-discover-article-min-chars` and `-discover-processing-max-attempts`. URL aliases, redirects, canonical links, exact bodies, and deterministic near-body fingerprints are clustered while every discovery path is retained; only the explained representative can enter recommendation selection. Article-only deterministic assessment is always available, defaults to a 0.45 eligibility threshold configurable with `-discover-article-quality-threshold`, and treats an OpenAI-compatible assessor as an optional versioned enhancement rather than a dependency.

Candidate content, processing, and eligibility are shared. Exposures, opens, reads, not-interested feedback, and personal archive intent are stored per user and do not change another user's candidate list. Adding, deleting, pausing, or manually fetching sources, safely retrying a digest, and append-only supplementation require the `owner` role; a `member` can still manage their own settings, feedback, block rules, and archives. A published digest cannot be deleted or reordered, and a normal retry returns its frozen snapshot.

The candidate inventory API reports eligible fresh, evergreen, exploration, and current-user hard-filtered counts, with `available / daily_limit` inventory days. Tune the recent window and the default 7-day warning / 3-day critical thresholds with `-discover-inventory-fresh-days`, `-discover-inventory-warning-days`, and `-discover-inventory-critical-days`.

Recommendation v3 selects only ready, eligible deduplication representatives. It publishes target N when supply is sufficient or actual M otherwise, recording every hard exclusion and any author → topic → source soft-limit relaxation. Exploration defaults to 15% and contributes at least one eligible item when N≥5 and exploration supply exists; source and primary-topic soft caps are 30% and 40%. An exposed but unopened article normally waits 75 days before competing again; tune `-recommend-reexposure-cooldown` within 60–90 days. A substantive content update may return sooner, while opened, archived, deep-read, or explicitly rated articles are excluded by default.

`-recommend-enabled` enables deterministic digest scheduling. LLM and vector support are optional enhancements: discovery, rules-based assessment, selection, and publication still work when `-llm-base-url`, `-llm-api-key`, `-llm-chat-model`, and `-llm-embedding-model` are empty. PostgreSQL may provide the `vector` extension for optional embedding storage; Docker Compose uses a pgvector-enabled image. Owners can inspect scheduling, processing, inventory, digest integrity, and long-tail gem contribution at `/api/admin/recommendations/metrics`. See the [recommendation v3 operations runbook](docs/operations/recommendation-v3-runbook.md) for upgrade, staged rollout, rollback, incident response, permissions, and retention semantics.



Content Discovery lists only sources explicitly added by an owner, keeping one manual entry per Public Suffix List registrable domain. Internal homepage and discovered feed endpoints retain their own validators, backoff, health, and provenance, but never appear as manual subscriptions. Manual seeds form the first crawl tier. Blogs found through maintained friend-link, Blogroll, Friends, Links, or recommended-blog areas form the second tier after a bounded deterministic homepage verification. Failed targets retain graph evidence but receive no further crawl; an owner can restore a false negative to `active` through the site status API.

Sitemaps are completely disabled by default. Homepage and robots declarations do not create sitemap endpoints or backfill jobs, `/sitemap.xml` is not guessed, and legacy sitemap endpoints are disabled during upgrade. An owner may enter a same-logical-domain sitemap URL on one manually subscribed seed and explicitly request historical gap fill. This creates no new blog source, and every discovered URL must still pass article fetching, page validation, deduplication, and article-level assessment before recommendation.

Set `-discover-socks5-proxy "socks5://user:pass@127.0.0.1:1080"` when discovery must use a proxy. It covers homepage, feed, sitemap, robots, Blogroll, backfill, and article requests only. An empty value connects directly, while an invalid non-empty value fails closed instead of silently bypassing the proxy. URL-encode special characters in usernames or passwords.

## Feedback and Contributions

Suggestions and feedback are welcome via Issues, or you can directly submit a PR to participate in project development.
