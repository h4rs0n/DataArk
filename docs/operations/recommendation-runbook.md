# 推荐 v3 运维手册

本手册覆盖 Blogroll 发现、文章评估、个人推荐、反馈与不可变日报管道。必须配置聊天模型：未设置 `-llm-chat-model` 和 `-llm-base-url` 时进程拒绝启动。Embedding 仍为可选。评估保持在暂停的手动队列上。

## 升级前置条件

更换运行中的二进制前，先备份 PostgreSQL 和归档目录。保留旧二进制，记录用户、发现来源、候选、推荐日、推荐条目与反馈的数量，不要把 Down 迁移当作回滚机制。迁移 `000032`–`000035` 把内容与状态移入 material 表并删除旧候选列；跨该边界时遵循 [Material 升级手册](material-migration-runbook.md)。Goose `000036` 删除人工标注工作流表及其记录；参见[文章评估手册](article-assessment-runbook.md#human-workflow-removal-in-goose-000036)。启动时在 PostgreSQL 上执行一次兼容回填并记录 `v3-compatibility` checkpoint；之后启动会跳过已完成的工作。

Blogroll 目标在单个首页响应提供确定性博客证据前保持 `observing`。它们在端点、Blogroll 或回溯扩展前转为 `active`。无证据的目标转为 `non_blog`，保留图边与 `blog_verification:<reason>` 运营细节，禁用首页端点，并从订阅投影中消失。若 owner 确认误判，把站点状态改为 `active`；这会重新启用并立即调度其首页，但不会删除抓取与图历史中的原始判定证据。

生产 PostgreSQL 先启动数据库，再启动单个 API 实例。启动时应用 Goose 与 River 迁移，把保存的自动启动设置应用到 `discovery_crawl`（默认暂停），并在 worker 接受作业前暂停 `article_assessment`。它把未完成的发现工作从 River 历史 `default` 队列移入 `discovery_crawl`，把中断的抓取作业重置为 available，并按该设置把到期发现作业入队执行。评估工作回收到暂停的手动队列。每日推荐生成与日报摘要仍在 River 的 default 队列。使用 embedding 时确认 `vector` 扩展；排序不再使用 embedding 评分公式。一次有代表性的隔离验证：

```sh
cd docker
docker compose up -d database
docker compose up -d --build dataarkapi
docker compose restart dataarkapi
docker compose logs --no-color dataarkapi
```

重启后确认迁移已到位、抓取作业遵守 `discovery_crawl` 上保存的自动启动设置（默认关）、已发布日报不变，且结构化 `dataark_event` 记录中没有打印任何密钥。以 owner 登录，查看 **推荐中心 → 抓取任务队列**，点击 **开始** 执行一轮或开启 **自动启动** 连续抓取；然后打开 **推荐中心 → 评估**，准备好消耗模型 token 时点击 **执行 LLM 评估**。绝不要把生产 token、密码、私有 Feed 正文或用户内容放进 fixtures、Compose 文件、命令历史或本手册。

生产上线前，在名称以 `dataark_v3_verify` 开头的一次性空数据库上运行仓库的 opt-in 数据库门禁：

```sh
cd api
DATAARK_POSTGRES_TEST_DSN='host=127.0.0.1 port=5432 user=<test-user> password=<test-password> dbname=dataark_v3_verify sslmode=disable' \
  go test ./bootstrap -run TestPostgresV3MigrationsRiverRestartAndPGVector -count=1 -v
```

该测试拒绝其他数据库名。它应用全部 Goose 与 River 迁移，重复迁移路径，检查 PostgreSQL JSON 字段，写入并读取一个 pgvector 值，两次启动 River 运行时，并完成确定性合成作业。测试后丢弃一次性数据库；绝不要把该命令指向生产或含用户数据的数据库。

## 分阶段上线与回滚

按以下顺序推进，并在进入下一步前核对数量。跨 material 迁移的升级需要维护窗口，回滚需恢复备份。`-discover-interval=0` 只关闭周期调度器：启动恢复、已排队作业和 owner 操作仍可抓取。若发现必须保持静默，在启动 API 前暂停或禁用相关站点/源。

1. 停止旧 API/worker，备份 PostgreSQL 与归档，部署当前 schema 与二进制，带 `-recommend-enabled=false`。跨 `000032`–`000036` 回滚需要停止新 API，并用匹配的旧二进制恢复升级前数据库/归档备份。
2. 等待启动 material 归并、归档内容回填和 checkpoint 控制的 v3 兼容回填完成。对比前后数量并检查 `material_ingestion_issues`；保留回填行和冻结的条目证据。
3. 为少量 owner 批准的种子站点启用共享 worker 与安全抓取器。用 owner API 暂停站点可停止抓取、图和未完成回溯工作，而不删除候选。
4. 为这些种子扩展 Blogroll 扫描和 HTML 归档/分页回溯。手动订阅摄取全部 Feed 条目并遍历归档历史；Blogroll 发现的站点使用有界增量工作。Sitemap 抓取与 robots Sitemap 提示已禁用。失败上升时暂停受影响站点；既有图和来源证据仍可用。
5. 启动一轮抓取或开启自动抓取，然后从 owner 评估面板显式启动 LLM 评估。对比 ready、review、ineligible、duplicate 与 eligible 数量。评估作业 `MaxAttempts=1`；失败的评估需要 owner 以 `retryFailures:true` 回填，然后再次手动运行队列。供应商 JSON 重试发生在该单次作业内。重排失败使当日生成失败；不会用规则分数或降级日报替代。
6. 以 `-recommend-enabled=true` 部署测试环境与测试用户。验证至少两个用户本地日期、目标 N/实际 M 说明、每用户屏蔽、不可变重试与 owner 指标。每日排序与发现流需要成功的 LLM 重排；日报摘要需要成功的 LLM 摘要作业。
7. 稳定发布窗口后扩展到全部用户。停止预期写入全局候选状态的旧客户端。当前 API 只把个人已读、忽略、归档、曝光与反馈状态写入认证用户的覆盖层。
8. 验证候选与归档文档引用 material 行，并检查迁移 checkpoint。旧候选内容、评估和单一来源列已由 `000034` 删除；历史抽取与来源证据表保留供审计与工作流引用。

不存在破坏性的「重建已发布日报」回滚路径。`POST /api/admin/recommendations/generate` 保留为兼容名称，但执行幂等安全重试；`POST /api/admin/recommendations/supplement` 只追加缺失条目并附审计证据。停止调度即可停止未来的每日生成。恢复旧二进制需要兼容的数据库；跨 material 迁移边界时使用上述备份恢复流程。

## 权限与数据语义

所有端点要求认证。服务器从认证上下文推导用户身份；客户端不能选择其他用户 ID。

| 操作 | 角色 | 语义 |
| --- | --- | --- |
| 个性化发现流与候选读取/归档 | member 或 owner | 发现流读取与操作使用认证用户的 material 状态；共享候选状态不变。 |
| 设置、反馈、撤销、解除屏蔽、偏好重置 | member 或 owner | 用户作用域；历史保留，当前状态可替换/可撤销。 |
| 日报今日/历史/条目上下文 | member 或 owner | 用户作用域的冻结快照；其他用户的条目不可见。 |
| 来源列表、站点图与回溯覆盖 | member 或 owner | 共享发现信息的认证读取。 |
| 来源创建/更新/删除/抓取，站点暂停/恢复/屏蔽、回溯请求与站点操作 | owner | 共享运营状态；暂停/屏蔽保留候选与证据。 |
| 日报重试/补充与产品指标 | owner | 重试非破坏性；补充只追加；指标包含聚合操作与安全标识符。 |

共享数据包括逻辑站点、端点、图边、抓取的文章正文与版本、去重身份、文章评估和来源证据。个人数据包括曝光、打开/已读/深读/归档状态、反馈、偏好、来源/主题/风格屏蔽和每日日报。来源历史绝不参与文章有效性、文章质量或质量上限。

## 运维诊断

组合使用 `GET /api/admin/recommendations/metrics`、站点图/运营/回溯端点、候选库存和结构化 `dataark_event` 日志。事件包含稳定 ID、状态、错误类别、本地日期、计数和固定的失败诊断。失败的 HTTP 工作可能包含仅主机名的 `domain` 和整数 `http_status`；失败的作业可能包含经空白压缩、URL 与密钥脱敏、至多 300 字符的 `error_message`。事件刻意不设任意 body/details 字段。

- 来源失败：查看最新抓取运行、HTTP/错误类别、验证器、下次到期时间与失败退避。单个端点失败不得阻止其他作业。通过修复可达性并等待或重新请求 owner 抓取来恢复；不要重置候选数据。
- robots 状态：robots.txt 是共享抓取器的 advisory 元数据。`Allow`、`Disallow`、`Crawl-delay` 和 robots 不可用不会阻止请求；不消费 Sitemap 声明。域名黑名单、SSRF、重定向、大小/类型限制、超时和主机速率限制仍然生效。
- 处理积压：对比 discovered/fetch-pending/ready/review/failed 计数与 owner 专属抓取队列面板。重启 API 会把到期抓取作业编排到 `discovery_crawl`。**自动启动**关闭时，已编排和中断的作业等待 **开始**；一轮清空可运行作业及其后续任务后暂停，未来计划工作等待下一轮。开启时 worker 跨重启自动继续；关闭会停止新认领，已认领作业继续完成。来源 **获取** 操作只入队一次抓取，同样受此控制。LLM 评估保持在暂停的 `article_assessment` 队列，直到 owner 点击 **执行 LLM 评估**。持续的文章类型、语言或短正文案例属于 review，不要强制 eligible。
- 库存不足：查看新鲜/常青/探索库存天数与日报的排除计数。硬资格、用户屏蔽、冷却和去重身份绝不放宽。增加合法发现/回溯覆盖，或在新 eligible 条目到达后使用 owner 补充；不要插入不合格填充。
- 模型失败：缺少聊天模型会拒绝进程启动。评估与日报摘要作业 `MaxAttempts=1`；每日生成作业使用 River 默认重试策略。修复原因后显式重新入队失败的评估并运行手动队列。缺失或过期的日报摘要由启动/调度恢复重新入队；摘要失败时已发布文章仍可正常阅读，只是缺少模板摘要。重排失败不能发布降级日报。保留已发布条目快照。
- 缺失、草稿或失败的日报：启动与调度器恢复该用户/本地日期作业。可使用 owner 安全重试。已发布或已补充的日期原样返回。之后库存增加只能通过只追加的补充处理。
- 低命中饥饿：`scheduleFloorViolations` 必须保持为零。历史低 eligible 率只用于报告；不能延长 active/observing/dormant 的最大间隔。出现违例时查看保存的调度决策。
- 完整性告警：`hardFilterViolations`、`duplicateClusterViolations` 和 `activeBlockViolations` 以零为目标，并使用发布时快照证据。停止上线并在改动数据前检查受影响条目/日期；不要删除日报。

长尾珍宝贡献率统计来自仅报告用途低命中站点的不同正面反馈推荐条目（`valuable`、`deep_read` 或归档）数量，除以全部不同正面反馈条目数量。低命中站点当前指至少 5 个发现候选且至多 10% eligible。该分组不作为来源分数持久化。

### 发现流刷新延迟

`POST /api/recommendations/discovery-feed/refresh` 同步等待一次完整 LLM 重排：有候选时重排失败直接报错，不会降级为规则排序。2026-09-30 本地 Compose 调查中，12 秒总耗时约 75–78% 来自 LLM 重排（8.1–9.4 秒，模型已禁 thinking、cached tokens 为 0），其余约 2.65 秒主要是组池候选查询与历史推荐过滤。诊断要点：

- 刷新等待时间首先受模型耗时支配：即使消除全部数据库开销，接口耗时仍约为模型耗时。要明显低于数秒，需改为异步任务（返回 202 并让前端查询生成状态）或预生成下一批快照；仅压缩数据库不够。
- 组池按「每来源一篇」凑 `poolSize`（默认 100）个不同来源，每页读取 `poolSize*4` 行；高产来源集中或用户屏蔽较多时，可能连读数页（调查中为 5 页 2,000 行）才凑齐来源目标，而被屏蔽来源仍先占据 SQL 分页结果。
- 历史推荐更新例外条件（`recommendation_items` 相关子查询）在该数据形态下会退化为对历史行的重复顺序扫描；`discovery_candidate_details` 复合视图的 `SELECT *` 还逐行附带正文等投影，`OFFSET` 翻页需先处理再丢弃前若干行。
- 排查时用 `llm_call` 事件的 `duration_ms` 与 token 用量分离模型环节，用 SQL 日志分离分页查询。该调查为两样本证据，不能据此推导 P95/P99。

完整调用链、执行计划定位与只读优化实验见[discovery-feed 刷新延迟调查](../investigation/2026-09-30-discovery-feed-refresh-latency.md)。

## 保留与隐私

系统当前不对发现正文、不可变内容版本、来源证据、评估、日报、反馈历史、历史复核行或运营证据执行自动破坏性保留。这支持审计、重试与回滚。运维者必须把文章正文和用户反馈当作保留中的应用数据，纳入访问控制、备份、导出和清除流程。

结构化事件绝不能包含完整 URL、URL 路径或查询、文章正文、Cookie、授权头、访问令牌、密码、模型密钥、响应体或任意上游错误负载。失败诊断在最终日志边界再次归一化：`domain` 仅主机名、无效状态码省略、`error_message` 有界且脱敏。抓取/处理存储的错误摘要是有界运营文本；在 owner 权限下查看。偏好重置开启新的个性化边界，但刻意保留历史反馈、日报和显式安全屏蔽。未来任何物理删除需要单独评审的迁移和引用完整性归并。

## 发布验证

运行后端与前端套件（SQLite 测试使用模拟 LLM 供应商；不需要公网或模型凭据）：

```sh
cd api
go test ./discovery ./recommendation ./jobqueue ./bootstrap ./api ./observability -run 'Test(DeterministicSiteWorld|BlogrollGraphFixture|BackfillFinds|ProcessCandidate|LowHitSource|EndToEndM17|DailyDigestM14|ObservabilityM16|RecoverDueJobs|V3Goose|V3SQLiteMigration)' -count=1
go test -race ./discovery ./recommendation ./jobqueue/... -count=1
go test ./... -count=1
cd ../web
npm test
npm run build
```

同时构建内嵌前端与后端二进制。根 `makefile` 的 `make web` 会更改用户全局 npm 注册表并使用 `npm i`。要使用 lockfile，在 `web/` 下运行 `npm ci && npm run build`，然后在仓库根运行 `make web2api && make api`。`make web2api` 把 `web/dist` 移入 `api/assets/web`；`make api` 编译前先运行 `go mod tidy`。生产上线前需要 PostgreSQL/River/pgvector 证据；仅 SQLite 证据不能替代。
