# 文章搜索与索引同步

`/api/search` 使用 Meilisearch，收录当前正文版本已完成模型评估的文章，以及存在本地文件的归档。评估低分不影响搜索；发现域名黑名单和个人推荐反馈不参与搜索过滤。内容按 material 合并，多个本地文件只对应一条搜索结果。

## 上线与同步

Goose `000048` 创建持久化同步待办、初始化进度及变更触发器。API 启动后自动构建完整临时索引，按 material ID 分页保存扫描进度，追平期间新增的变更后交换到现有索引名。构建完成前旧索引继续提供查询；首次空库需要等待构建完成。

搜索同步有独立后台任务，不受爬取、评估队列暂停影响。普通评估、Batch 导入、评估回滚、正文更新、归档增删及身份合并都会在对应数据库事务中登记待办。没有新版本有效评估的文章会移除外链结果；本地归档继续保留。数据库记录不变的文件丢失会在查询当前页时检测，立即改用原文入口；归档一致性修复负责清理索引中残留的归档位置。

首次扫描与切换前追平每批最多处理 1,000 个 material，正常增量每批最多处理 100 个，空闲时每 2 秒只查询数据库待办。索引就绪后，Meilisearch 完整性检查在后台任务启动时执行一次，此后每 5 分钟检查一次，使用单个 generation 标记请求，不再每轮查询索引元数据。Meilisearch 的异步任务成功后才确认同步完成，单任务最多等待 10 分钟；并发更新通过修订号保留。失败退避重试，最长间隔不超过 5 分钟，重启后继续。搜索同步不会调用 LLM，不会为了索引重试重新评估文章。

Compose 默认将 `MEILI_MAX_INDEXING_THREADS` 设为 `12`，可用同名环境变量覆盖。调整时应按 Docker 实际可用核心数预留查询和其他服务资源；线程数是索引线程上限，不保证全部线程始终繁忙。修改该变量需重建 Meilisearch 容器；修改回填批次需重新构建并更新 API。保留原数据卷和同步进度表，即可从现有游标继续，无需清空索引或重新初始化。

## 状态与恢复

owner 可携带现有 Bearer token 请求 `GET /api/admin/search/status`。响应 `Data` 包含：

| 字段 | 含义 |
| --- | --- |
| `phase` | `prepare` 准备、`scan` 分页构建、`catchup` 追平变更、`swap` 切换、`ready` 正常增量 |
| `cursor` / `scanned` | 本轮扫描游标与已扫描 material 数，包含无需索引的内容 |
| `pending` / `failed` | 当前待同步数与曾失败、尚未恢复的待办数 |
| `lastSuccessAt` | 最近完成同步或切换的时间 |
| `error` | 最近的全局同步故障；单条故障记录在待办表与日志中 |

故障日志前缀为 `search sync`。数据库中 `search_sync_pending.error`、`attempts`、`next_attempt_at` 可用于定位具体文章的问题。Meilisearch 恢复后自动重试；不要清空待办或重新运行 LLM 评估。

Meilisearch 索引被删除或重建为空时，通过内部 generation 标记识别并自动启动完整构建。空闲期间在下一次完整性检查时发现，通常不超过 5 分钟；增量写入发现索引不存在时立即触发恢复。内部标记不参与关键词匹配，也不参与归档一致性检查。

现有 `/api/archiveConsistency/repair` 会先根据磁盘补全归档元数据，再重建完整文章与归档索引。备份恢复使用相同完整重建流程。重建与后台同步互斥，若提示同步正在运行，稍后重试。发现缺失或损坏的 HTML 会保留异常报告，不会因此把所有外链文章当作丢失文件。

## 页面行为

标题优先打开本地归档；没有本地文件时在新标签页打开 HTTP(S) 原文。未归档条目提供快速归档，复用现有 URL 归档任务。提交成功只提示已加入队列，不轮询、不跳转；再次搜索时根据文件和元数据判断是否已归档。提交失败可重试，重复提交复用已有任务。

## 验证

常规检查：`cd api && go test ./...`，以及 `cd web && npm test && npm run build`。

真实 PostgreSQL / Meilisearch 测试使用专用空数据库和独立 Meilisearch：

```sh
cd api
DATAARK_SEARCH_TEST_DSN='host=127.0.0.1 port=5432 user=... password=... dbname=dataark_search_verify sslmode=disable' \
DATAARK_SEARCH_TEST_MEILI='http://127.0.0.1:7700' \
go test ./search -run TestPostgresSearchSync -count=1 -v
```

测试拒绝数据库名不以 `dataark_search_verify` 开头的目标，清空该库的 material 及相关数据，并创建测试专用索引。不得指向业务数据库；Meilisearch 同样应使用独立实例。浏览器验收需另外验证外链、本地阅读、归档提交成功与失败、移动端、控制台和网络请求。
