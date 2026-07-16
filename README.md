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

内容发现可通过 `-discover-interval`、`-discover-timeout`、`-discover-max` 和 `-discover-ua` 配置；将 `-discover-interval` 设为 `0` 可关闭后台调度。安全抓取和逐端点调度还支持 `-discover-host-concurrency`、`-discover-min-request-interval`、`-discover-robots-ttl`、`-discover-max-redirects`、`-discover-active-feed-interval`、`-discover-observing-interval`、`-discover-dormant-interval`、`-discover-backoff-base` 和 `-discover-backoff-max`。默认活跃 Feed 最迟 24 小时、观察站点最迟 7 天、仍可访问的休眠站点最迟 30 天再次检查；历史低命中本身不会停抓。多入链、已发现的合格文章和明确正反馈只能通过额外预算缩短检查间隔，不能延长这些基础下限；最短额外预算间隔可用 `-discover-schedule-min-interval` 调整，owner 可从站点 operations API 查看每个端点的基础间隔、实际间隔和理由。Blogroll 图谱自动扩展默认深度 3、每来源扫描激活 50 个目标、每天新增 100 个观察站点，可分别通过 `-discover-max-graph-depth`、`-discover-max-blogroll-targets` 和 `-discover-daily-observing-limit` 调整。历史覆盖默认每个作业处理 1 页，任何未完成的低命中来源最迟 7 天获得下一批，可通过 `-discover-backfill-batch-size` 和 `-discover-backfill-max-interval` 调整。文章处理默认要求抽取正文至少 120 个字符，并把瞬时抓取失败限制为 5 次，可通过 `-discover-article-min-chars` 和 `-discover-processing-max-attempts` 调整。URL 别名、重定向、canonical、完全相同正文和确定性近似正文指纹会聚类，同时保留全部发现路径；只有带选择理由的代表项能进入推荐选择。纯文章输入的确定性评估始终可用，默认资格门槛为 0.45，可用 `-discover-article-quality-threshold` 调整；OpenAI-compatible 评估只作为可选的版本化增强，不是运行依赖。

候选文章正文、处理和资格是共享数据；曝光、打开、已读、不感兴趣和个人归档写入当前用户的独立状态，不会改变其他用户的候选列表。来源增删、暂停／恢复、手动抓取、日报安全重试与 append-only 补充只允许 `owner` 角色执行，普通 `member` 仍可管理自己的设置、反馈、屏蔽和归档。已发布日报不能删除或重排，普通重试返回原冻结快照。

候选库存 API 分别返回 eligible 新鲜、常青、探索和当前用户硬过滤后的可用数量，并以 `可用数 / daily_limit` 计算库存天数。新鲜窗口及 7 天预警、3 天严重预警可分别通过 `-discover-inventory-fresh-days`、`-discover-inventory-warning-days` 和 `-discover-inventory-critical-days` 调整。

推荐 v3 只从 ready、eligible 的当前去重代表中选择：足量时发布目标 N，不足时发布实际 M，并在日报记录各类硬排除和按作者→主题→来源放宽的软约束。默认探索比例为 15%，当 N≥5 且存在合格探索文章时至少包含 1 篇；同来源和同主主题软上限分别为 30% 和 40%。仅曝光未打开的文章默认冷却 75 天后才能再次竞争，可通过 `-recommend-reexposure-cooldown` 在 60–90 天范围内调整；正文实质更新可提前重现，已打开、归档、深读或有明确评价的文章默认不再推荐。

`-recommend-enabled` 开启确定性日报调度；LLM 与向量能力始终是可选增强，留空 `-llm-base-url`、`-llm-api-key`、`-llm-chat-model` 和 `-llm-embedding-model` 仍可完成抓取、规则评估、选文和发布。owner 可通过 `/api/admin/recommendations/metrics` 查看调度、处理、库存、日报完整性和长尾精品贡献指标。升级、灰度、回滚、故障处理、权限和数据保留说明见 [推荐 v3 运维手册](docs/operations/recommendation-v3-runbook.md)。



“内容发现－订阅源”只显示 owner 主动添加的来源，并按 Public Suffix List 的可注册域名保留一个人工入口；主页和自动发现的 Feed 等内部端点继续逐 URL 保存条件请求、退避、健康与溯源，但不会冒充人工订阅。人工种子是第一抓取优先级；从其友情链接、Blogroll、Friends、Links、友邻或推荐博客区域发现且通过确定性主页验证的博客是第二优先级。未通过验证者保留图谱证据但不继续抓取，误判站点可通过站点状态 API 恢复为 active。

Sitemap 默认完全关闭：主页和 robots 声明不会创建 Sitemap 端点或回溯任务，也不会猜测 `/sitemap.xml`，旧 Sitemap 端点在升级后停用。只有 owner 可在某个人工订阅种子的站点详情中输入同一逻辑域的 Sitemap URL，显式执行历史补漏；该操作不创建新博客来源，解析出的 URL 仍须经过正文抓取、文章页判定、去重和文章级质量评估后才能进入推荐。

内容发现需要代理时可设置 `-discover-socks5-proxy "socks5://user:pass@127.0.0.1:1080"`。它只代理主页、Feed、Sitemap、robots、Blogroll、回溯和文章请求；留空时直连，无效配置会拒绝抓取而不会静默绕过代理。用户名或密码中的特殊字符需要使用 URL 编码。

## 反馈与贡献

欢迎通过 Issue 提交建议与反馈，或直接提交 PR 参与项目共建。
