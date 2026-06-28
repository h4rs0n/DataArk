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

Content discovery uses the `-discover-interval`, `-discover-timeout`, `-discover-max`, and `-discover-ua` flags. Set `-discover-interval 0` to disable the background RSS/site discovery scheduler.

LLM daily recommendations are prepared behind `-recommend-enabled`. PostgreSQL should provide the `vector` extension for embedding storage; Docker Compose uses a pgvector-enabled image. LLM calls are configured through `-llm-base-url`, `-llm-api-key`, `-llm-chat-model`, and `-llm-embedding-model`.



## Feedback and Contributions

Suggestions and feedback are welcome via Issues, or you can directly submit a PR to participate in project development.
