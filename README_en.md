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

`dataarkapi` mirrors application, job, database, HTTP access, and recovery logs to both the container console and the host's `docker/logs/` directory. Files use the server-local date and are named `dataarkapi-YYYY-MM-DD.log`. The current date and preceding six dates are retained by default; set a positive integer `LOG_RETENTION_DAYS` in `docker/.env` to change this window. The initially generated administrator password remains console-only and is never written to the log file. The API refuses to start when it cannot create the directory or current log file so persistence cannot fail silently.

**Using `make` to build**
```
make web
make build
```

## Feedback and Contributions

Suggestions and feedback are welcome via Issues, or you can directly submit a PR to participate in project development.
