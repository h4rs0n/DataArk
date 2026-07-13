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

Content discovery uses the `-discover-interval`, `-discover-timeout`, `-discover-max`, and `-discover-ua` flags. Set `-discover-interval 0` to disable the background RSS/site discovery scheduler. Safe fetching and per-endpoint scheduling can be tuned with `-discover-host-concurrency`, `-discover-min-request-interval`, `-discover-robots-ttl`, `-discover-max-redirects`, `-discover-active-feed-interval`, `-discover-observing-interval`, `-discover-dormant-interval`, `-discover-backoff-base`, and `-discover-backoff-max`. Defaults keep active feeds within 24 hours, observing sites within 7 days, and reachable dormant sites within 30 days, so low historical yield never disables checks by itself. Blogroll graph expansion defaults to depth 3, 50 activations per source scan, and 100 new observing sites per day; tune these bounds with `-discover-max-graph-depth`, `-discover-max-blogroll-targets`, and `-discover-daily-observing-limit`. Historical coverage processes one page per job and gives every unfinished low-yield source another batch within 7 days; tune these bounds with `-discover-backfill-batch-size` and `-discover-backfill-max-interval`.

LLM daily recommendations are prepared behind `-recommend-enabled`. PostgreSQL should provide the `vector` extension for embedding storage; Docker Compose uses a pgvector-enabled image. LLM calls are configured through `-llm-base-url`, `-llm-api-key`, `-llm-chat-model`, and `-llm-embedding-model`.



## Feedback and Contributions

Suggestions and feedback are welcome via Issues, or you can directly submit a PR to participate in project development.
