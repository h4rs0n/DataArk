# DataArk
<div align="center">
    <img src="images/GitHub_README.png" alt="logo" width="200">
</div>

<p align="center">
<a href="README_en.md">English</a>
</p>

**DataArk - 数据方舟**是一个离线保存与数据检索系统，旨在保存互联网上可能失效的网页等数据。目前仅支持HTML网页文件存储索引，后续将支持图片视频等类型。

## 中间件
搜索引擎: [Meilisearch](https://github.com/meilisearch/meilisearch)

HTML下载: [SingleFile](https://github.com/gildas-lormeau/SingleFile)

## 安装/部署
**使用Docker Compose（推荐）**
```
cd docker
sudo docker compose build
sudo docker compose up -d
```
第一次启动会生成一个初始用户名密码，请通过执行命令 `sudo docker compose logs` 查看输出中的默认用户名密码，仅在系统第一次部署运行时输出。

**使用make编译**
```
make web
make build
```
可在 api/bin 目录下生成可执行文件。部署好 Meilisearch 和 PostgreSQL 后，运行下述命令启动服务：
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
```
备份功能依赖 `pg_dump` 与 `psql` 命令；手动部署时请安装 PostgreSQL client，并确保 `-mdump` 指向 Meilisearch 的共享 dump 目录（对应 Meilisearch 的 `MEILI_DUMP_DIR` 或 `--dump-dir`）。

内容发现可通过 `-discover-interval`、`-discover-timeout`、`-discover-max` 和 `-discover-ua` 配置；将 `-discover-interval` 设为 `0` 可关闭后台调度。安全抓取和逐端点调度还支持 `-discover-host-concurrency`、`-discover-min-request-interval`、`-discover-robots-ttl`、`-discover-max-redirects`、`-discover-active-feed-interval`、`-discover-observing-interval`、`-discover-dormant-interval`、`-discover-backoff-base` 和 `-discover-backoff-max`。默认活跃 Feed 最迟 24 小时、观察站点最迟 7 天、仍可访问的休眠站点最迟 30 天再次检查；历史低命中本身不会停抓。Blogroll 图谱自动扩展默认深度 3、每来源扫描激活 50 个目标、每天新增 100 个观察站点，可分别通过 `-discover-max-graph-depth`、`-discover-max-blogroll-targets` 和 `-discover-daily-observing-limit` 调整。历史覆盖默认每个作业处理 1 页，任何未完成的低命中来源最迟 7 天获得下一批，可通过 `-discover-backfill-batch-size` 和 `-discover-backfill-max-interval` 调整。文章处理默认要求抽取正文至少 120 个字符，并把瞬时抓取失败限制为 5 次，可通过 `-discover-article-min-chars` 和 `-discover-processing-max-attempts` 调整。URL 别名、重定向、canonical、完全相同正文和确定性近似正文指纹会聚类，同时保留全部发现路径；只有带选择理由的代表项能进入推荐选择。



## 反馈与贡献

欢迎通过 Issue 提交建议与反馈，或直接提交 PR 参与项目共建。
