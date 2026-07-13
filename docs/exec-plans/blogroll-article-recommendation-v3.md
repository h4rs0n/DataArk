# DataArk：友情链接图谱、文章级质量与每日推荐 v3 执行计划

本文档是一份可执行的 ExecPlan，存放于：

    docs/exec-plans/blogroll-article-recommendation-v3.md

本文档必须在实施期间持续维护。执行者每次开始工作时都必须先阅读仓库根目录的 `PLANS.md`、`AGENTS.md`、`CONVENTIONS.md` 和本文档，然后从 `Progress` 中第一个未完成项继续。本文档中的 `Progress`、`Surprises & Discoveries`、`Decision Log`、`Outcomes & Retrospective` 都是活内容，不能只在最后补写。

除非遇到缺失外部凭据、无法访问必要基础设施、会不可逆地破坏用户数据，或者产品约束发生直接冲突，Codex 不应在里程碑之间停下来询问“下一步做什么”。完成一个里程碑、运行验证、更新本文档并形成可恢复的提交后，应直接进入下一个未完成里程碑。对于可以通过安全默认值解决的细节，选择最保守、可回滚的实现并在 `Decision Log` 中记录，不等待人工确认。

## Purpose / Big Picture

实施完成后，DataArk 应当从用户或管理员确认的博客种子出发，识别这些博客中的友情链接、Blogroll、Friends、Links、推荐阅读等关系，构建可追溯且有边界的博客关系图谱。新发现的博客不需要先证明“整体质量高”才获得抓取机会，而是进入低预算的观察状态，持续发现其新文章并逐步回溯历史文章。

每篇文章都必须独立经历正文抓取、文章页识别、规范化、去重、内容评估和用户相关性判断。来源历史平均质量、优质文章比例或者某一篇文章上的用户负反馈，不得成为该来源其他文章的硬性淘汰条件，也不得给单篇文章设置不可突破的质量上限。来源级数据只允许用于访问安全、抓取健康、调度频率、额外资源预算、图谱遍历和日内多样性约束。

推荐系统应按用户本地日期每日发布目标 `N` 篇文章。候选不足时发布实际的 `M` 篇并给出原因，不得用重复、被屏蔽、无正文、低于文章级质量门槛或其他不合格内容强行补足。已发布日报是不可变快照；用户反馈从后续日报开始生效，并且只有用户明确选择“屏蔽来源”时才产生来源级硬过滤。

用户可以观察到以下完整行为：

1. 添加一个种子博客后，系统立即验证并抓取其 Feed、站点地图和文章，同时扫描友情链接。
2. 管理界面能够展示“种子 A → 友情链接页 → 观察博客 B → Blogroll → 博客 C”的发现路径、图谱深度和证据。
3. 一个大多数文章普通、只有少数精品的博客仍会获得非零抓取和历史回溯预算；其中的精品文章可以独立进入候选池并进入日报。
4. 同一文章从 Feed、Sitemap、友情链接或多个博客发现时只形成一个逻辑文章，但保留全部发现来源。
5. 两个用户的打开、已读、不感兴趣、归档、深读和屏蔽状态相互隔离。
6. 日报在用户时区按时生成，显示目标数量、实际数量、候选池类别、推荐理由和不足原因；刷新、重试和服务重启不会改变已发布内容。
7. 远程 LLM、向量扩展或单个来源暂时失败时，系统仍能依靠确定性规则和已有文章评估继续运行。

## Progress

以下时间均使用 ISO 8601。每完成一个可验证的子项，执行者必须更新勾选状态、时间、验证命令和相关提交哈希。不要把整批工作留到最后一次性勾选。

- [x] 2026-07-13T00:00:00-07:00 审阅 `dev` 分支现有发现、正文抽取、推荐、反馈、调度、迁移和前端结构。
- [x] 2026-07-13T00:00:00-07:00 确认产品不变量：友情链接扩展、文章级质量、来源非零探索预算、共享候选与用户状态分离、不可变日报。
- [x] 2026-07-13T00:00:00-07:00 编写本执行计划的初始版本。
- [x] 2026-07-13T21:08:04+08:00 M0：固定基线、建立确定性测试站点和可替换的时钟、抓取器、任务队列测试接口。基线为 `9602a91`、Go 1.26.4、Node 24.15.0、迁移 `000001`–`000002`；聚焦测试 `go test ./discovery ./recommendation -run 'Test(Deterministic|HTTPClientFetcher|DiscoverySourceFetchAndCandidateState|RecommendationDayAndItemDeduplication|RecommendationGenerationDueUsesSettingsTime|GenerateDailyRecommendationsRerankerValidationAndFallback)' -count=1` 通过，仓库验证 `go test ./...`、`npm run build`、`make web2api` 和 `GOCACHE=/tmp/dataark-go-cache make api` 通过。检查点提交：`979a17e`。
- [x] 2026-07-13T21:28:44+08:00 M1：完成增量数据库模型和兼容迁移，保留现有数据并建立逻辑博客、来源端点、图谱边、文章溯源、处理状态、用户状态和日报快照结构。聚焦验证 `go test ./auth ./bootstrap ./discovery ./recommendation -count=1` 通过；`TestV3SQLiteMigrationPreservesAndBackfillsLegacyData` 验证重复迁移、计数、默认值、唯一键、外键、旧端点映射、溯源、角色、快照和跨日报历史，`TestV3GooseMigrationIsAdditiveAndParseable` 验证三份 Goose 迁移可解析。仓库验证 `go test ./...`、`npm run build`、`make web2api` 和 `GOCACHE=/tmp/dataark-go-cache make api` 通过。检查点提交：`494a27a`；当前环境缺少 `docker` 命令，真实 PostgreSQL 执行留待具备容器基础设施时补验。
- [x] 2026-07-13T21:40:20+08:00 M2：抽取共享持久任务运行时，支持发现抓取、图谱扫描、历史回溯、文章处理和日报生成的幂等作业。聚焦测试 `go test ./jobqueue ./discovery ./recommendation ./api -count=1` 通过，race 验证 `go test -race ./discovery ./recommendation ./jobqueue/...` 通过；仓库验证 `go test ./...`、`npm run build`、`make web2api` 和 `GOCACHE=/tmp/dataark-go-cache make api` 通过。检查点提交：`929f032`。
- [x] 2026-07-13T22:56:00+08:00 M3：完成统一安全 HTTP 抓取层、条件请求、robots、SSRF／重定向复验、逐域并发与速率限制、有限指数退避和逐来源调度。聚焦验证 `go test ./discovery ./bootstrap -count=1` 通过，race 验证 `go test -race ./discovery -count=1` 通过；本地验收覆盖 `200 + ETag → 304` 零新增、robots 禁止零正文请求、私网重定向拒绝、超大／错误类型拒绝、失败后恢复、低命中有限下次时间和确定性域名限制。仓库验证 `go test ./... -count=1`、`npm run build`、`make web2api` 和 `GOCACHE=/tmp/dataark-go-cache make api` 通过。检查点提交：`f499077`。
- [x] 2026-07-13T23:17:21+08:00 M4：完成多证据 Blogroll 识别、保留证据的博客关系图谱、有界循环／深度／单站／每日激活、新来源观察状态、恢复调度和只读发现路径 API。聚焦验证 `go test ./discovery ./api ./bootstrap -count=1` 通过，race 验证 `go test -race ./discovery ./api -count=1` 通过；本地 A→B→C→A 验收只生成 3 个规范站点和 5 条预期边，普通正文／广告／社交外链不入图，B 自动创建 observing 节点与立即到期主页端点，API 返回从 A 的 Links 页发现 B 的最短路径和结构化证据，深度／单站／每日剩余目标均以 pending 保留并可恢复。仓库验证 `go test ./... -count=1`、`npm run build`、`make web2api` 和 `GOCACHE=/tmp/dataark-go-cache make api` 通过。检查点提交：`dc2fb59`。
- [x] 2026-07-13T23:32:41+08:00 M5：完成主页 Feed／JSON Feed／Sitemap／RSSHub 配置端点发现、常见路径安全探测、Sitemap index 子端点和最新文章增量入池，并保留多来源 provenance。聚焦验证 `go test ./discovery ./bootstrap ./api ./recommendation -count=1` 通过，race 验证 `go test -race ./discovery ./api -count=1` 通过；本地验收证明同一站点同时拥有立即到期的主页、Feed、Sitemap，一个 Feed+Sitemap URL 只产生 1 个候选和 2 条溯源，首次来源／高可信标题不被低可信空元数据覆盖，标题-only 候选为 `fetch_pending`，重复 200 和 304 均不重复安排处理或评估。仓库验证 `go test ./... -count=1`、`npm run build`、`make web2api` 和 `GOCACHE=/tmp/dataark-go-cache make api` 通过。检查点提交：`4cffd40`。
- [x] 2026-07-13T23:48:32+08:00 M6：完成 Sitemap index／子 Sitemap、归档页、分页和站内文章链接驱动的持久化历史回溯。聚焦验证 `go test ./discovery ./api ./bootstrap -run 'Test(Backfill|GetDiscoveryBackfill|V3Goose|RecoverDue)' -count=1` 通过，模块验证 `go test ./discovery ./api ./bootstrap -count=1` 和 race 验证 `go test -race ./discovery ./api -count=1` 通过；本地 B 站验收发现 RSS 窗口外的 `high-sitemap.html` 与 archive-only `high-archive.html`，并证明失败后从同一持久游标恢复、重放不重复、低命中来源仍在 7 天内继续、完成原因只来自游标耗尽。仓库验证 `go test ./... -count=1`、`npm run build`、`make web2api` 和 `GOCACHE=/tmp/dataark-go-cache make api` 通过。检查点提交：`3064f15`。
- [x] 2026-07-14T00:06:05+08:00 M7：把安全正文抓取与 `ExtractArticle` 串入共享候选处理作业，完成文章页硬规则、错误分类、有限重试、不可变正文版本和推荐前置门禁。聚焦验证 `go test ./discovery ./recommendation ./api ./bootstrap -run 'Test(ProcessCandidate|FeedCandidatesReach|GenerateDailyRecommendationsEnrichesPendingCandidates|V3Goose)' -count=1` 通过，模块验证 `go test ./discovery ./recommendation ./api ./bootstrap ./jobqueue -count=1` 和 race 验证 `go test -race ./discovery ./recommendation ./api -count=1` 通过；本地 B Feed 的 13 个候选全部到达 `ready` 或明确 `ineligible`，相同 HTML 不增版本、正文变化增版本，标签／登录／短正文被排除，瞬时失败有限重试，摘要-only 候选不被富化或推荐。仓库验证 `go test ./... -count=1`、`npm run build`、`make web2api` 和 `GOCACHE=/tmp/dataark-go-cache make api` 通过。检查点提交：`ef6cb17`。
- [x] 2026-07-14T00:20:59+08:00 M8：完成跟踪参数 URL、重定向最终 URL、canonical、正文 exact hash 和确定性近似正文聚类，选出唯一代表并保留成员、版本、全部溯源和历史快照。聚焦验证 `go test ./discovery ./recommendation ./bootstrap -run 'Test(ResolveCandidateDuplicates|UpsertTrackingAliases|RecommendationSelectionOnlyUsesDuplicateRepresentative|DuplicateFeedbackCreatesReview|V3Goose)' -count=1` 通过，模块验证 `go test ./discovery ./recommendation ./api ./bootstrap ./jobqueue -count=1` 和 race 验证 `go test -race ./discovery ./recommendation ./api -count=1` 通过；本地三入口验收只产生 1 个推荐代表和 3 条代表溯源，跟踪参数双入口为 1 候选／2 溯源，近似正文同簇、独立正文异簇，代表变化不改历史快照，“太重复”只生成复核信号且来源权重为零变化。仓库验证 `go test ./... -count=1`、`npm run build`、`make web2api` 和 `GOCACHE=/tmp/dataark-go-cache make api` 通过。检查点提交：`b629780`。
- [x] 2026-07-14T00:34:14+08:00 M9：完成版本化、可解释的文章级内容价值评估，建立不含任何来源聚合字段的静态输入边界、始终可用的规则版和可选 OpenAI-compatible 增强版。聚焦验证 `go test ./discovery ./recommendation ./api ./bootstrap -run 'Test(ArticleAssessmentInput|LowHitSource|OptionalAssessor|OpenAICompatibleArticleAssessor|ProcessCandidate|EnrichDiscoveryCandidateUpdatesStructuredFields|V3Goose)' -count=1` 通过，模块验证 `go test ./discovery ./recommendation ./api ./bootstrap ./jobqueue -count=1` 和 race 验证 `go test -race ./discovery ./recommendation ./api -count=1` 通过；B 的 100 篇样本中 97 篇普通、3 篇精品，恰好 3 篇 eligible，同正文放在 A/B 得分完全一致且 B 精品高于 A 普通文；规则／增强版本并存，增强失败回退规则，重评不改历史 assessment 或日报引用，旧富化不再覆盖质量分。仓库验证 `go test ./... -count=1`、`npm run build`、`make web2api` 和 `GOCACHE=/tmp/dataark-go-cache make api` 通过。检查点提交：`45f7661`。
- [x] 2026-07-14T00:51:25+08:00 M10：完成共享候选与逐用户曝光／打开／已读／深读／不感兴趣／归档状态分离，并为来源、逻辑站点和破坏性日报操作实施 owner 权限。聚焦验证 `go test ./discovery ./recommendation ./api ./bootstrap -run 'Test(UserCandidate|RecordUserCandidate|RecommendationFeedbackAndSourceBlock|DiscoverySourceManagement|DestructiveRecommendation|CandidateInteraction|SiteStatus|OwnerPause|GlobalSafety|V3SQLiteMigration|V3Goose)' -count=1` 通过，race 验证同一测试集通过；双用户验收证明 A 的状态与来源屏蔽不影响 B，owner 暂停保留候选并停止抓取／图谱／回溯，member 管理请求明确返回 403。仓库验证 `go test ./... -count=1`、`npm run build`、`make web2api` 和 `GOCACHE=/tmp/dataark-go-cache make api` 通过。旧全局状态按候选进入可对账复核表，不猜测用户归属且旧字段继续可读；检查点提交：`4d4a488`。
- [ ] M11：完成公平抓取预算、低命中来源非零检查下限、候选库存和新鲜／常青／探索池。
- [ ] M12：完成推荐 v3 候选选择、多样性、探索配额、冷却再推荐和“目标 N、实际 M”语义。
- [ ] M13：完成幂等、可替换、可撤销的文章级反馈，偏好衰减、冷启动设置和显式来源屏蔽。
- [ ] M14：完成按用户时区生成的不可变日报、启动补偿、非破坏性补充和模型故障降级。
- [ ] M15：完成来源图谱、抓取健康、历史覆盖、文章评估、反馈状态和日报解释的前端体验。
- [ ] M16：完成运营指标、结构化日志、管理统计和长尾精品贡献指标。
- [ ] M17：完成端到端验收、PostgreSQL/SQLite 回归、灰度迁移、旧路径清理、部署文档和最终复盘。

## Surprises & Discoveries

初始审阅时已经确认以下事实。后续执行者发现新的事实时，必须在本节追加日期、文件位置和影响，不要删除旧记录。

- 2026-07-13：`api/discovery/store.go` 同时承担来源 CRUD、Feed 解析、站点爬取、候选入库和全局定时器，职责过于集中。当前站点爬取限制为同域名，不能遍历外部友情链接。
- 2026-07-13：`api/discovery/model.go` 中候选只有一个 `SourceID`/`SourceName`；同一文章由多个入口发现时，冲突更新可能覆盖来源，无法保留完整溯源。
- 2026-07-13：来源模型已经有 `ETag`、`LastModified`、`FailureCount`、`NextFetchAt` 等字段，但主抓取调度尚未完整使用它们；当前发现调度主要是统一周期、串行遍历所有启用来源。
- 2026-07-13：`api/discovery/extractor.go` 已有 DOM Distiller 正文抽取辅助能力，但常规 Feed／链接候选入池并未稳定地把正文抓取和抽取作为可推荐前置步骤。
- 2026-07-13：候选的 `new/read/ignored/archived` 是共享全局状态，和多用户场景中的个人曝光、打开、归档及不感兴趣语义冲突。
- 2026-07-13：推荐反馈当前以可叠加事件记录，并会更新来源偏好；这会把单篇文章反馈外溢为来源级评价，不符合文章粒度原则。
- 2026-07-13：推荐历史当前通过用户与候选的唯一约束实现永久不重复，无法表达“只曝光未打开后经过冷却可再次出现”或者“正文实质更新后重新出现”。
- 2026-07-13：今日推荐查询使用服务器日期，而推荐调度使用用户时区；午夜附近可能读取错误日期。
- 2026-07-13：手动生成今日推荐会走破坏性重建路径并删除原项目及关联反馈，这与不可变日报目标冲突。
- 2026-07-13：PostgreSQL 已有 River 持久任务能力，但目前由推荐模块拥有；发现、回溯和文章处理需要共享任务运行时，而不是各自再建定时循环。
- 2026-07-13：认证用户模型没有显式角色，来源增删、全局忽略和手动重建等高影响操作目前没有清晰的所有者边界。
- 2026-07-13：用户指定的 `docs/exec-plans/blogroll-article-recommendation-v3.md` 最初以未跟踪文件 `docs/exec-plans/dataark-blogroll-article-recommendation-v3-execplan.md` 存在。为使恢复命令和用户给出的路径一致，M0 将其移动到计划规定的正式路径。
- 2026-07-13：基线测试无既有失败；`go test ./...` 与 `npm run build` 均通过。当前受限执行环境默认禁止 `httptest.Server` 绑定本机临时端口，因此包含确定性站点的 Go 测试需要显式本机端口权限，但不访问真实互联网。
- 2026-07-13：工作区中的未提交 `makefile` 会在 `make all` 中永久修改用户级 `~/.npmrc` 并执行 `npm i`，该全局副作用被安全策略拒绝。等价的仓库级验证拆为已通过的 `npm run build`、`make web2api` 和 `GOCACHE=/tmp/dataark-go-cache make api`；生成的二进制和嵌入资源在验证后清理／恢复，未混入提交。
- 2026-07-13：M1 检查 PostgreSQL 基础设施时，`docker compose ps` 返回 “docker: command not found”；这不是迁移代码失败。Goose 文件已通过解析测试，SQLite 已执行真实 DDL／回填／约束测试，但 PostgreSQL DDL 和回退演练必须在 M17 或更早获得 Docker／PostgreSQL 后补跑。
- 2026-07-13：`api/assets/embed.go` 固定读取被忽略的 `api/assets/web/assets/` 子树，仓库仅跟踪占位 `index.html`、图标和图片；删除忽略的构建资源后 `TestLoadFileReturnsEmbeddedAssetSubtree` 会失败。证据是首次 M1 全量测试只有 `DataArk/assets` 失败并报告 `open .: file does not exist`，执行前端构建和 `make web2api` 后全量测试通过。因此验证后必须保留该忽略目录，不能把它当作普通临时文件清理。
- 2026-07-13：启动顺序先让 GORM 扩展已有 `discovery_sources`／`discovery_candidates`，再运行 Goose 创建 v3 表；因此 `site_id` 和 `representative_id` 可能已存在，单纯的 `ADD COLUMN IF NOT EXISTS ... REFERENCES` 无法保证 PostgreSQL 外键。`000003_blog_discovery_v3.sql` 使用具名、条件式约束块独立建立这两个外键。
- 2026-07-13：SQLite／开发运行时若每次启动都新建内存幂等集合，两个调度器启动恢复会各执行一次相同副作用。M2 将内存存储按同一个 `*gorm.DB` 共享，并保留到运行时重建后；`TestStartSQLiteDuplicateRecoveryRunsOnce` 证明两次启动只执行一次。
- 2026-07-13：现有发现 ticker 的问题不是周期唤醒本身，而是在 ticker goroutine 中串行抓取所有来源。M2 保留轻量周期唤醒以兼容现有配置，但其回调现在只把到期工作交给共享队列；实际抓取不再由全局 ticker 串行执行。M3 将进一步按端点 `next_due_at`、退避和域名限制细化调度。
- 2026-07-13：GORM 默认把字段名 `ETag` 映射为 `e_tag`，但 M1 PostgreSQL 迁移和既有列名是 `etag`；M3 首次条件请求持久化测试暴露了 SQLite 的同类错配。`DiscoverySource` 和 fetch-run 模型现在显式声明 `column:etag`，自动迁移和 PostgreSQL DDL 因而使用同一列名。
- 2026-07-13：原站点抓取依赖 Colly 的独立 HTTP 栈，无法共享新抓取器的验证器、robots 缓存、响应类型和域名节流。M3 用有页数上限的同域 BFS 和现有 HTML 解析器替换该路径；`go mod tidy` 随之移除 Colly 及其仅由此路径引入的间接依赖。
- 2026-07-13：M0 的 C 首页故意包含文本“is not a blogroll”及普通新闻链接；M4 初版若只做关键词包含，会把否定语境和同一 `<main>` 下的异构外链误判为友链。解析器现在识别中英文否定短语，并只在最近的稳定区块内统计外链列表；该夹具继续保留普通 A 文章引用，同时新增独立 Friends 页形成真正的 C→A 图谱边。
- 2026-07-13：仅用 `created_at` 无法可靠执行“每日新激活 100 个观察站点”，因为前一天 pending 的节点在今天恢复时不会计入当天创建数。`DiscoverySite.ActivatedAt` 和第五份增量迁移现在记录实际激活日，跨扫描和重启都能执行同一每日上限。
- 2026-07-13：旧 `fetchSiteCandidates` 会在一次站点抓取中递归页面、直接抓 Feed／Sitemap，并把站点导航 URL 当作永久标题；它无法表达端点独立验证器、调度和 provenance。M5 生产分支改为主页单次条件抓取→保存独立端点→由共享队列立即抓取，每个端点因此拥有自己的 ETag、退避和 fetch run；旧私有辅助函数暂留给 M6 回溯重构，但不再位于来源主路径。
- 2026-07-13：B 首页的 `/blogroll.html` 和 `/archive/page-2.html` 会被旧“路径含 blog 或两层斜杠”启发式误当文章。M5 在最新文章链接入口增加导航／归档／标签／分页／Feed／Sitemap 排除规则；Blogroll 由 M4 图谱处理，归档和分页由 M6 有界回溯处理，避免用 URL 充当标题提前进入推荐候选。
- 2026-07-13：River 的 worker 注册若直接导入推荐／发现包会与调用方形成导入环。共享包因此只持有稳定 Args、River client 和函数式 `Handlers`，由 API 组合根注入业务 handler；推荐包不再拥有 River client 生命周期。
- 2026-07-13：主页中的历史入口不能只按 URL 是否含 `archive` 判断；B 的精品文章名 `high-archive.html` 会因此被误当分页入口而永远不入池。M6 将历史导航识别限制为明确的 archive／archives／page 路径段、分页锚文本和年份层级，文章文件名中的普通单词不再触发导航分类。
- 2026-07-13：Sitemap index 的子端点既是可独立条件抓取的新文来源，也是历史覆盖的待处理页。只保存端点会让历史作业等待另一轮来源调度；M6 在发现和抓取嵌套 Sitemap 时同步幂等合并回填游标，仍由统一安全抓取器实际访问。
- 2026-07-14：共享任务运行时从 M2 起已定义 `ProcessCandidate`，但 API 组合根一直没有注入 handler；因此 M5/M6 虽正确入队，生产 worker 会报告 handler unavailable。M7 接入 `RunProcessCandidateJob` 后，Feed、Sitemap 和归档候选才真正进入常规正文流水线。
- 2026-07-14：旧推荐富化查询只检查 `enrichment_status`，向量补充查询甚至只按 ID 读取；若只新增候选处理状态，摘要-only 或明确不合格页面仍可能从旧路径旁路进入推荐。M7 在富化、普通候选和向量补充三处统一增加 `processing_state=ready AND eligibility_state=eligible` 门禁。
- 2026-07-14：DOM Distiller 能稳定抽取正文，但不保证每个列表页都返回错误；把“抽取成功”当作“文章页”会接受标签和登录页。M7 将明确 `<article>`／文章结构化类型与导航 URL 排除结合，并把无法识别语言的边界样本送 review，而不是猜测 eligible。
- 2026-07-14：候选表的唯一 `url` 只能合并完全相同的发现 URL；跟踪参数可由 `normalized_url` 提前合并，但重定向和 canonical 必须在正文抓取后才知道，因此不能安全地在入池时物理删除记录。M8 保留成员记录，用代表关系表达逻辑文章，并在聚类后把 provenance 汇到代表。
- 2026-07-14：旧 `feedbackDeltas` 对“太重复”返回来源和风格各 `-0.5`，会把文章身份问题外溢为来源惩罚。M8 将四类偏好增量全部改为零，并新增独立 duplicate-review signal；现有测试此前没有覆盖该分支。
- 2026-07-14：三词 shingle 的 Jaccard 相似度在一篇约 40 词文章只替换两个词时约为 `0.74`–`0.78`；初始 `0.82` 会漏掉明显改写副本。M8 采用 `0.74`，同时要求至少 20 词、同语言、字数在 0.5–2 倍内且每次最多比较 500 篇，降低短模板误并和无界扫描风险。
- 2026-07-14：旧 `EnrichDiscoveryCandidate` 在主题、实体、摘要富化时同时覆盖 `quality_score` 和 `depth_score`；即使 M9 新 assessor 正确，后续 v2 富化仍会静默改写生效质量。M9 移除这两项更新并用回归测试固定原 assessment 分数不变，旧富化只保留内容标签和模型审计。
- 2026-07-14：推荐包已依赖 discovery 包，discovery 不能反向导入现有 OpenAI-compatible provider，否则形成导入环。M9 把最小 `ArticleAssessor` 接口放在 discovery，由 recommendation 提供 adapter，并由 API 组合根注入；无模型配置时返回 nil，规则评估路径完全独立。
- 2026-07-14：规则评估若只按正文长度会把重复冗长的日常更新误判为高质量。M9 的 97/3 夹具促使基线同时计算词汇信息密度、完整性、证据标记、可读性、因果／权衡深度和常青方法信号；重复 “routine status update” 即使长度有效也低于 0.45，而证据丰富的精品通过。
- 2026-07-14：旧候选 `read/ignored/archived` 行没有操作者或用户 ID，`discovery_candidate_feedbacks` 也只有候选和动作；把这些状态自动分给 `admin`、首个用户或所有现有用户都会制造错误的个人历史。M10 因此只建立逐候选 legacy review 记录并保留原字段，数量可对账但不猜测用户。
- 2026-07-14：来源恢复查询原先只检查 `discovery_sources.enabled`，逻辑站点进入 `paused/blocked` 后仍可能排入来源抓取；单独更新站点状态不足以真正暂停。M10 在恢复、旧批量抓取和实际抓取入口同时加入站点运营门禁，并保持候选数据不变。

## Decision Log

每条决策都应包含日期、决定、理由和被拒绝的替代方案。实施过程中只有在新证据出现时才修改决定；修改时追加新条目，不要抹掉历史。

- 2026-07-13：把人工添加或人工确认的博客称为“种子站点”或“允许抓取站点”，不把“批准”解释为内容质量背书。理由是抓取许可和内容价值是两个独立维度。
- 2026-07-13：来源发现采用从种子站点开始的有界图遍历，主要证据是友情链接、Blogroll、Friends、Links、推荐阅读等页面或区域。拒绝无边界开放网络爬虫，因为它不可控、难解释且不符合当前产品范围。
- 2026-07-13：逻辑博客站点与具体抓取端点分离。一个站点可以拥有主页、RSS/Atom/JSON Feed、RSSHub、Sitemap 和归档入口。拒绝继续把所有 URL 都压入同一个“来源”概念，因为无法正确表达图谱和调度。
- 2026-07-13：来源级历史优质率、平均分或用户对单篇文章的反馈，不得作为文章入池或推荐的硬性否决条件，不得作为文章质量乘数或封顶项。来源级数据仅用于安全、抓取健康、额外资源分配、图谱优先级和日内多样性。
- 2026-07-13：任何仍允许抓取、仍可能更新且未因 robots、安全、明确屏蔽或非博客判定而停止的站点，都保留可配置的非零新文检查下限；历史回溯在完成前也保留非零批次预算。拒绝依据低历史命中率将预算降为零。
- 2026-07-13：共享的是站点图谱、抓取端点、原始文章、正文、去重关系和文章级内容评估；个人化的是曝光、打开、已读、深读、归档意图、不感兴趣、显式偏好、屏蔽规则和日报。
- 2026-07-13：文章先经过“有效性硬规则”，再经过“文章内容价值软评估”，最后结合“用户相关性、时效、新颖性和多样性”排序。三层信号必须在数据和代码接口上分开。
- 2026-07-13：文章评估输入不包含来源平均质量、来源命中率或来源级自动推导偏好。来源可作为展示溯源、显式用户屏蔽／收藏和多样性字段，但不能决定文章本身质量。
- 2026-07-13：Feed 增量发现和历史回溯并行。旧文章达到文章级门槛后进入常青池，推荐时保留并展示原始发布时间。
- 2026-07-13：每日 `N` 是目标，不是必须填充的配额。硬过滤永不放宽；软多样性约束可在候选不足时按固定、可记录的顺序逐步放宽。
- 2026-07-13：探索率必须落实为可观察配额。默认沿用 15%；当 `N >= 5` 且有合格探索文章时至少选 1 篇。探索文章仍要通过文章级有效性、质量、重复和屏蔽门槛。
- 2026-07-13：已发布日报不可删除或原地重排。重试返回同一快照；不足时可以追加有审计记录的补充项目，但不能覆盖原项目和反馈。
- 2026-07-13：反馈维护“当前有效状态”和不可变事件历史。重复点击幂等，改选会替换，撤销可恢复；只有显式“屏蔽此来源”才产生来源级硬过滤。
- 2026-07-13：远程 LLM 和向量能力是可选增强。所有测试、抓取、文章有效性、基础评估和日报生成必须有确定性本地降级路径。
- 2026-07-13：迁移采用增量表、双写／双读、后台回填和特性开关，不进行一次性破坏性重写。旧字段只在 v3 行为稳定且数据校验通过后移除。
- 2026-07-13：共享 River 运行时承载持久化任务；SQLite 和单元测试提供同步或内存降级。拒绝为每个子系统创建独立、无法统一恢复的 ticker。
- 2026-07-13：当前范围不实现协同过滤、来源全局质量排行榜、无边界搜索引擎、强制依赖单一模型厂商或以提示词调优替代数据闭环。
- 2026-07-13：M0 只新增边界和测试，不把现有生产抓取或推荐路径提前切到新接口。理由是 M0 的明确约束是不改变生产行为；M2 和 M3 将分别接管任务和 HTTP 路径。拒绝在夹具尚未固定时重写 `api/discovery/store.go`，因为会把后续里程碑风险混入基线。
- 2026-07-13：抓取和任务接口采用本文档 `Interfaces and Dependencies` 中规定的稳定方法签名；测试替身在内部用唯一键去重，并让多个替身实例共享存储来模拟进程重启。理由是后续 River／同步实现可以替换边界而无需改变业务调用方。
- 2026-07-13：确定性站点使用三个相互引用的 `httptest.Server` 和磁盘夹具模板，而不是 DNS、外网域名或容器。理由是它能真实覆盖跨站重定向和图谱循环，同时完全不依赖互联网；拒绝硬编码随机端口，因为测试重跑不可复现。
- 2026-07-13：M1 使用“增量 DDL + 启动时幂等回填”，而不是在 SQL 中用脆弱的 URL 正则推导站点。Go 代码按规范主机键合并同一主机和常见 `www` 变体，并为每个旧候选建立稳定哈希溯源键；拒绝在迁移中自动跨独立域名合并，因为误合并难以安全回滚。
- 2026-07-13：`000003_blog_discovery_v3.sql` 的 Down 是数据保留 no-op；回滚方式是先关闭后续 v3 开关并继续保留新增图谱、溯源、评估、用户状态和快照。理由是自动 Drop 会不可逆删除实施期间采集的数据；真正清理必须在对账后用单独审核迁移完成。
- 2026-07-13：数据库层移除跨日报的永久 `(user_id, candidate_id)` 和用户级 dedupe 唯一约束，但保留同日报 `(day_id, candidate_id)` 唯一。v2 的 `AddRecommendationItem` 暂时继续应用层永久去重，直到 M12 用冷却策略替换；这样 v3 能表达未来再推荐，同时 v2 开关关闭路径的行为不变。
- 2026-07-13：旧 `admin` 用户幂等回填为 `owner`，新注册用户默认为 `member`；不根据创建顺序自动提升其他现有账号。理由是用户名 `admin` 是仓库现有明确部署所有者，猜测“第一个普通用户”可能造成越权，后续权限 API 以显式角色为准。
- 2026-07-13：迁移已有推荐项时只填充空的快照字段，后续重复启动不从当前候选覆盖历史标题、URL、摘要、作者、来源或发布时间。理由是从 M1 开始就要避免回填破坏历史展示，即使不可变发布逻辑要到 M14 才正式切流。
- 2026-07-13：共享 `jobqueue` 定义五种只含稳定 ID、日期或内容版本的参数，River 使用 `ByArgs` 唯一键，日报额外使用 24 小时窗口；不把网页正文或候选对象放入队列。理由是作业重试必须从数据库读取当前持久状态，并避免队列膨胀或敏感正文泄露。
- 2026-07-13：SQLite／测试采用同步内存队列，记录 pending/running/completed/failed、attempts 和错误；completed/running 重复入队幂等，failed 或进程中断状态可在重建后重试。拒绝用简单 channel 作为降级，因为 channel 无法模拟重启恢复和稳定唯一键。
- 2026-07-13：启动恢复分别由发现和推荐模块查询自己的表，再通过共享 `JobEnqueuer` 入队；共享包不查询领域表。理由是避免共享基础设施反向依赖领域模型，并使恢复逻辑可以用 SQLite 和固定时钟独立测试。
- 2026-07-13：M2 只为已存在的来源抓取和日报生成注入可执行 handler；Blogroll、回溯和文章处理 worker 类型已注册，但 handler 在对应 M4、M6、M7 才接入。缺失 handler 返回明确错误而不是把作业伪装为成功，避免静默丢失待处理工作。
- 2026-07-13：M3 的所有发现网络访问统一通过 `HTTPClientFetcher`，并对 Feed、Sitemap、HTML／正文和 robots 使用独立默认大小上限；robots 获取失败采用保守的 fail-closed，并以 `robots_unavailable` 区别于明确的 `robots_disallowed`。理由是无法确认站点规则时继续抓正文风险更高，且两类状态必须支持不同的运维判断和后续重试。
- 2026-07-13：端点成功调度以类型上限为安全基线：活跃 Feed 24 小时、观察站点 7 天、休眠站点 30 天；观察到实际更新时可在 15 分钟下限和类型上限之间缩短间隔。失败采用以来源 ID 和连续失败次数生成的稳定抖动指数退避，成功清零；低历史命中从不产生 `NULL` 或无限远时间。
- 2026-07-13：新增 `000004_discovery_fetch_observability.sql`，为抓取运行保存最终 URL、Content-Type、ETag、Last-Modified 和 robots 状态；Down 保留审计数据。拒绝把这些信息塞入错误文本或覆盖端点原 URL，因为结构化运行记录更易对账，且重定向不应悄然改变管理员配置的入口。
- 2026-07-13：M4 只把显式 `rel=friend/me/blogroll`、明确中英文标题／区块、常见 Links/Friends/Blogroll 路径或至少三个同区块外站组成的稳定列表作为图谱证据；普通正文外链本身永远不够。边只保存来源页、锚文本、最多 240 字符上下文、检测规则和置信度，不保存正文全文。
- 2026-07-13：达到深度、单站或每日上限时，仍创建规范站点和边，但在节点 `operational_pause` 与边 `pending_reason` 中记录原因、暂不创建可抓端点；后续扫描有预算时清除 pause、写 `activated_at` 并安排主页抓取和 Blogroll 扫描。理由是静默截断不可恢复，而把 pending 节点当作已激活又会绕过预算。
- 2026-07-13：站点分类采取保守策略：只把已知社交平台和明确登录／购物流程判为 `non_blog`，其他由友链证据发现但尚未验证的目标进入 `observing`。历史文章质量、平均分和命中率不进入分类、建边、深度或任务优先级；独立入链数只提高抓取端点优先级。
- 2026-07-13：人工新增来源在同一数据库事务中升级／创建 `seed` 站点并确保主页端点，事务提交后安排来源抓取与图谱扫描；队列临时失败不回滚已保存种子，到期端点和 `next_graph_scan_at` 由启动／周期恢复补入。这样 API 不会因可恢复的队列瞬断留下“返回失败但数据已创建”的歧义。
- 2026-07-13：M5 继续复用 `discovery_sources` 表表达主页、Feed、RSSHub、Sitemap 和 Sitemap-index 端点；显式页面端点直接保存，常见 Feed／Sitemap 路径只有通过统一安全抓取器且能被对应解析器接受后才保存。新端点 `next_due_at` 立即到期并入共享队列，避免把猜测路径永久写成健康端点。
- 2026-07-13：候选保留第一次发现的 `SourceID`／`SourceName`，后续发现只更新 `last_seen_at` 并幂等写 `DiscoveryCandidateProvenance`；Feed、主页链接和 Sitemap 的元数据可信度分别为 80、40、20，空标题／摘要永不清空已有值，低可信数据不能覆盖高可信展示字段。新增 `metadata_confidence` 由第六份增量迁移持久化，不参与文章质量评分。
- 2026-07-13：新候选统一进入 `fetch_pending`，不再用 URL 补标题；只在逻辑候选首次创建时安排 `ProcessCandidate(candidate_id, content_version=0)`。重复 Feed item、另一端点的相同 URL 和 304 仍更新端点运行／provenance 所需状态，但不再次安排正文处理或评估；正文版本变化后的重新处理由 M7/M8 明确递增版本后触发。
- 2026-07-13：M6 为每个站点／策略保存 JSON 游标中的 pending 与 visited URL，并按 `sitemap` 优先、`archive` 次之选择到期状态；默认每作业 1 页且硬上限 20 页。拒绝用单次递归遍历或数据库偏移量作为游标，因为前者无界，后者在新 URL 插入时会跳项或重复。
- 2026-07-13：回填完成只允许写入 `cursor_exhausted`；robots 明确拒绝可暂停，其他安全／类型类不可恢复错误连续 5 次后才暂停，普通网络失败始终有限退避重试。历史产出和精品命中不参与停止条件；高产批次可在 1 小时后继续，其他未完成状态最迟使用可配置且默认 7 天的下限。
- 2026-07-13：没有可信发布日期的历史链接以发现时间填充 `published_at`，同时写 `published_confidence=discovered_at`；Sitemap `lastmod` 和 Feed 日期分别保留独立置信标记。拒绝把无日期旧文无标记地伪装成刚发布内容。
- 2026-07-14：M7 的 `ready` 表示安全抓取、文章页判断、标题、正文长度和语言识别已经完成；在 M8 去重和 M9 文章评估完成前，资格保持 `unknown`，并显式记录 `dedupe_pending,assessment_pending`。拒绝为了保持旧推荐数量而提前标记 `eligible`，因为这会绕过尚未完成的重复代表和文章级门槛。
- 2026-07-14：每次实质正文变化创建 `(candidate_id, content_version)` 唯一的不可变 `discovery_article_content_versions` 行；相同正文只刷新当前抓取／抽取时间。提交正文版本时用预期版本守卫和行锁，拒绝覆盖式保存单一正文，因为并发旧作业可能使版本号与内容错配，也无法审计更新。
- 2026-07-14：正文网络失败采用候选级 5 分钟起、24 小时封顶的有限退避，默认最多 5 次；robots、安全 URL、响应大小和内容类型属于立即终止的硬拒绝，耗尽的瞬时故障进入 `failed/review`。错误摘要压缩到 500 字符，不保存响应正文；所有阈值均有本地默认和命令行配置。
- 2026-07-14：M8 不物理合并候选、正文版本或历史推荐项；每个成员保留原 ID，`representative_id` 指向当前代表，代表聚合全部 provenance，推荐只读取 `dedupe_state=ready` 且 self／空代表关系的项。拒绝改写既有推荐 `candidate_id` 或快照，因为代表优化不能改变已发布历史。
- 2026-07-14：代表选择只使用当前可访问性、canonical 是否指向候选原始主机、最终 URL 与 canonical 一致性、正文完整度和可信发布时间；同分取较小 ID，并持久化明细理由。来源平均质量、来源命中率、图谱深度和用户偏好不进入聚类或代表分数。
- 2026-07-14：近似重复使用本地三词 shingle Jaccard 作为始终可用的确定性基线，不依赖向量或 LLM；exact body、canonical 和 URL 身份优先级更高。每个候选另存 raw／normalized／final／canonical／content-hash identity，cluster ID 在合并时优先沿用最早的现有 ID，保证重跑和新成员加入不漂移。
- 2026-07-14：“太重复”反馈写入 `discovery_duplicate_review_signals` 供后续人工或自动复核，不修改主题、来源、风格和深度偏好。该表只对候选设级联外键，用户和推荐项 ID 作为审计引用保留，避免跨领域删除历史信号。
- 2026-07-14：`ArticleAssessmentInput` 只包含当前版本标题、正文、作者、发布时间、语言、字数和必要的重复代表信息；字段反射测试禁止 source／site／yield／graph／feedback／reputation／average／hit 等名称。拒绝让 assessor 接收完整 `DiscoveryCandidate`，因为它会使来源表现字段未来悄然进入质量计算。
- 2026-07-14：规则 assessor 版本固定为 `deterministic_rules/1.0.0/article-quality-v1`，输出九个 0–1 维度和 JSON 理由；默认总体门槛 0.45，可由 `-discover-article-quality-threshold` 调整。低于门槛是文章级 ineligible，低置信度是 review，任何来源产出统计不参与门槛或分数。
- 2026-07-14：每次评估先幂等保存规则版本，再尝试可选增强；增强输出完整、范围合法且有理由时成为 current assessment，失败或格式错误只写候选 `assessment_error` 并继续使用规则版本。拒绝只在 LLM 失败时临时计算但不落库的降级，因为那样无法解释当前资格或重放。
- 2026-07-14：推荐项创建时复制候选的 `current_assessment_id`；正文版本变化只新增新的 assessment 行并切换候选当前引用，不更新旧推荐项。旧主题／实体 enrichment 不再写质量和深度分，确保只有 ArticleAssessor 能决定文章内容价值。
- 2026-07-14：候选 `status` 暂时保留为只读兼容字段，生产 API 的 new/read/ignored/archived 读写改为当前用户的 `user_candidate_states` overlay；个人操作永不改写共享 `status`。拒绝立即删除或复用旧字段，因为旧 UI／二进制仍需要可回滚读路径，且无操作者的历史状态不能安全迁移。
- 2026-07-14：旧全局 `read/ignored/archived` 每个候选只写一条 `discovery_legacy_candidate_state_reviews`，默认 `pending` 并说明无可靠用户身份；不自动建立任何用户状态。迁移 Down 是数据保留 no-op，回滚旧读路径仍可使用原 `status`，复核数量可独立对账。
- 2026-07-14：`paused` 和 `blocked` 是 owner 管理的可逆逻辑站点状态：两者都停止抓取、图谱和未完成回溯，分别记录 `owner_paused` 与 `global_safety_block`；恢复为 seed/observing/active 时只恢复运营游标，不删除或重算候选。member 只拥有个人候选状态、设置、反馈、屏蔽和归档权限。

## Outcomes & Retrospective

2026-07-13，M0 已完成。仓库现在拥有可推进的时钟边界、带条件验证器和响应大小限制的 HTTP 获取边界、仅接受稳定标识的幂等任务边界，以及覆盖 A→B→C 循环、误识别外链、Feed/Sitemap/归档多重溯源、历史精品、robots、500、超时、同域／跨域重定向、非文章页和双用户隔离数据的本地夹具世界。现有发现重复抓取和推荐日期／日报去重行为被回归测试固定，生产路径没有切流。

与计划的唯一验证差异是未直接运行用户工作区版本的 `make all`：它会持久修改全局 npm 配置，安全审查拒绝该副作用。前端生产构建、嵌入资源步骤和后端最终二进制构建已分别通过，因此 M0 的构建证明完整；后续应在清理或改为仓库级 npm 配置后恢复单命令验证。M1 的主要风险是新增表和约束必须同时适配 PostgreSQL Goose 与 SQLite GORM，并且不能减少旧记录数量。

2026-07-13，M1 已完成。`000003_blog_discovery_v3.sql` 和对应 GORM 模型新增逻辑站点、端点关联、图谱边、候选溯源、抓取运行、历史回溯状态、版本化文章评估、用户候选状态、候选处理／资格字段、角色以及日报／推荐项快照字段。启动回填把旧来源按主机映射为种子站点，把旧候选映射为多溯源结构，冻结既有日报展示数据，并可安全重复运行。SQLite 迁移测试证明旧用户、来源、候选、日报、项目和反馈数量不减少；旧 UI／v2 服务测试继续通过。

M1 与计划的差异是当前机器无法执行 PostgreSQL 容器集成；迁移文件的 Goose 解析、约束设计和 SQLite 行为已有自动测试，真实 PostgreSQL 升降级仍是明确遗留。M2 的主要风险是 River 当前由推荐包持有全局客户端，抽取共享运行时必须避免导入环并保持 SQLite 同步实现与 v2 调度行为。

2026-07-13，M2 已完成。`api/jobqueue/` 现在统一拥有 River 客户端、worker 注册、关闭和默认队列生命周期，并定义来源抓取、Blogroll 扫描、历史回溯、候选处理和用户本地日期日报五类幂等作业。SQLite／测试使用可跨运行时实例共享状态的同步内存实现；失败作业可重试，运行中断作业在重建时转为可重试，单个来源失败不阻塞其他恢复项。启动会补入到期来源、未完成回溯、处理中候选和已过生成时间但缺日报的用户日期。

推荐 v2 已通过共享 `GenerateDaily` handler 继续工作，推荐包不再持有 River client；发现 ticker 只负责入队到期工作，不再串行执行网络抓取。PostgreSQL River 的真实重启恢复仍受当前 Docker 缺失限制，但 River 参数／worker、SQLite 重启语义、并发幂等和 race 检测均有自动证据。M3 的主要风险是把已有 `fetchDiscoveryURL` 切到统一抓取器时必须同时处理 SSRF、重定向、robots、验证器、响应类型、域名节流和现有测试替身，且不能访问外网。

2026-07-13，M3 已完成。Feed、Sitemap、站点 HTML 和后续文章请求现在共享一个无 Cookie／认证信息的安全抓取器：初始 URL 与每次重定向都执行 SSRF 检查，按内容类型使用大小上限，并在读取前遵守缓存的 robots 规则和逐域并发／最小间隔。Feed 端点持久化 ETag／Last-Modified；`304` 作为未变化成功写入 fetch run，不再解析 Feed 或增加候选。每次尝试都记录结构化 HTTP、最终 URL、验证器、robots 和错误类别，来源成功会清零失败，失败会设置有限退避，新来源立即到期，启动恢复继续只入队已到期端点。

M3 的验证完全使用内存 SQLite、固定时钟、测试抓取替身和本机 `httptest`，没有真实互联网、用户数据或 LLM 密钥。PostgreSQL 迁移的真实容器执行仍因本机缺少 Docker 未补验；新增迁移是纯增量、Goose 可解析且 Down 保留审计数据。M4 的主要风险是友情链接识别必须复用同一安全抓取层，同时区分站内普通链接、广告／导航和真正跨站 Blogroll 证据，并严格控制循环、深度与扇出。

2026-07-13，M4 已完成。`BlogrollDiscoverer` 从首页和有界的常见 Friends/Links/Blogroll 页面提取显式关系、上下文标题和稳定外链列表，`SiteClassifier` 只做保守结构分类，`SiteGraphService` 规范化 `www`／根主机、幂等写边、维护最小深度和独立入链优先级，并为新观察站点创建立即到期的主页端点。扫描 worker 已接入共享 River／内存运行时和启动恢复；`GET /api/discovery/sites/:id/graph` 返回直接入／出边、证据、从最近种子的最短路径、深度、独立入链数及停止原因。

循环和预算行为由固定三站夹具证明：重复扫描 A、B、C 不增加站点或边，C 首页的普通新闻、广告、社交和文章引用均不被误判，只有独立 Friends 页产生 C→A。超出深度、每次激活和每日激活的目标不会丢失，后两类在下一次有预算的扫描恢复。测试仍不访问互联网、用户数据或 LLM；PostgreSQL 容器执行继续受本机缺少 Docker 限制，第五份 Goose 迁移仅增加证据和激活时间且 Down 保留数据。M5 的主要风险是把主页、Feed、Sitemap 端点发现和多溯源文章入池接到现有双路径时，不能覆盖首次来源或让缺正文链接提前变成 ready。

2026-07-13，M5 已完成。主页端点现在从 `<link rel=alternate>` 识别 RSS、Atom 和 JSON Feed，从页面／robots 识别 Sitemap，在缺少声明时只保存通过安全抓取和解析验证的常见路径，并支持 `crawl_config` 中的 `rsshub_url` 或基于全局 RSSHub 地址的 `rsshub_route`。Sitemap URL set 形成最新候选，Sitemap index 形成有 50 个上限的独立子端点；所有新端点归属同一逻辑站点、立即到期并保留旧来源列表 API 的双读兼容。

文章入池已经从“覆盖式 upsert”改为逻辑候选加幂等发现溯源。首次来源、首次时间和非空高可信元数据稳定保留；每个 Feed、Sitemap、主页链接的原 URL、来源页和最近发现时间独立存在。同一文章的重复 200、另一端点发现和 304 都不会增加正文处理／评估次数，新标题-only 项明确等待正文抓取。测试完全使用 SQLite、固定时钟、内存队列记录器和本机夹具，没有外网、真实用户或 LLM；PostgreSQL 仍因 Docker 缺失只完成 Goose 解析和增量 DDL 审查。M6 的主要风险是把当前未使用的递归站点辅助路径改造成有持久游标、批次和完成原因的回溯作业，不能一次抓完整站或在重试时重复入池。

2026-07-13，M6 已完成。主页发现的归档／分页入口和 Sitemap index／子 Sitemap 现在会建立或扩展站点级回填状态；共享 `BackfillSite` worker 每次只消费有界 URL 批次，并原子保存 pending／visited 游标、已查看 URL、文章、重复、失败、最早／最晚覆盖时间、最近批次、估计完成度和完成原因。覆盖 API `GET /api/discovery/sites/:id/backfill` 可直接读取每种策略的恢复位置和停止证据。

固定 B 站夹具证明当前 Feed 之外的旧 Sitemap 精品和只存在于分页归档的精品都能进入同一候选／溯源路径；注入失败后重启会继续原游标，重放已访问批次不会增加候选或处理作业。低产出只影响额外机会，不会将下次批次设为空或无限远；未知日期有显式置信标记。测试没有互联网、真实数据或 LLM，PostgreSQL 实跑仍受 Docker 缺失限制，第七份 Goose 迁移已通过解析并采用数据保留 Down。M7 的主要风险是将 `ExtractArticle` 接入现有候选作业时，需要把页面类型、正文版本、可重试错误和资格状态拆开，同时确保相同 HTML 不重复创建版本。

2026-07-14，M7 已完成。共享候选 worker 现在通过 M3 安全抓取器获取最终文章页，DOM Distiller 与 HTML 元数据回退抽取标题、作者、正文、摘要、canonical、发布时间和语言，再按文章结构、导航／登录／标签类型、标题、可配置正文长度、语言和安全结果进入 `ready`、`review`、`failed` 或 `ineligible`。每次失败保存类别、有限摘要、尝试次数和下一处理时间；重启恢复只重新安排已到期项，旧 `discovered` 候选幂等转为 `fetch_pending`。

正文 hash 未变时只刷新时间，实质变化时递增版本并保存不可变抽取快照；陈旧版本作业成为 no-op，并发提交用行锁保护。抽取成功后去重与评估显式保持 pending，资格不会提前变成 eligible；旧推荐富化和向量旁路也只能读取真正 ready+eligible 的候选。固定 Feed、故障、页面类型和摘要-only 测试全程关闭 LLM 并不访问外网。PostgreSQL 仍因 Docker 缺失只验证第八份 Goose 迁移可解析和数据保留 Down。M8 的主要风险是把 URL、重定向、canonical、exact hash 和近似正文聚类合并为稳定代表，同时迁移当前候选唯一 URL 约束且不丢失已有 provenance 或历史快照。

2026-07-14，M8 已完成。正文处理成功后现在立即建立 raw、normalized、final、canonical 和 content-hash 身份，并按 URL／canonical／exact body／近似正文形成稳定 cluster。成员与不可变正文版本不删除；cluster 保存唯一代表、匹配方法、成员数和代表选择解释，所有发现 provenance 幂等迁移到代表。服务重启会恢复 `ready + dedupe_pending` 项，重放保持相同 cluster 和代表。

代表只依据文章自身的可访问性、canonical 原站证据、最终 URL、一致性、正文完整度和发布时间元数据选择。推荐富化、普通查询和向量补充都增加 dedupe-ready／代表门禁，即使成员被错误标成 eligible 也不会同日报竞争。确定性三词 shingle 处理无向量环境下的近似副本；比较有语言、长度、候选数边界。重复反馈改为独立 review signal，不再处罚来源。测试不访问外网、用户数据、向量服务或 LLM；PostgreSQL 容器缺口未变，第九份 Goose 迁移已解析且 Down 保留身份审计。M9 的主要风险是把当前 v2 富化分数替换为版本化文章级 assessor，并用静态输入边界证明任何来源聚合字段都无法进入质量计算。

2026-07-14，M9 已完成。`ArticleAssessor` 现在是文章版本专用边界，规则实现基于正文自身的信息密度、原创增量近似、完整性、证据、可读性、深度和常青价值生成版本化分数、置信度与理由。只有去重代表会评估并依据文章门槛成为 eligible；来源名称虽仍在候选展示模型中，但无法进入 assessor 输入。恢复调度会继续 `dedupe_ready + assessment_pending` 项。

规则结果始终先落库；API 组合根可注入 recommendation 包的 OpenAI-compatible adapter，成功时规则与增强两版并存，失败时规则版照常激活且错误可见。旧 enrichment 只更新摘要／主题／实体／内容标签，不再覆盖质量；推荐项保存 active assessment ID。100 篇低命中来源防回归、静态字段边界和相同正文跨来源测试证明没有来源平均表现的硬过滤、乘数或封顶。测试没有真实模型、密钥、互联网或用户数据；第十份 Goose 迁移可解析且数据保留，PostgreSQL 实跑仍待 Docker。M10 的主要风险是把旧全局 read／ignored／archived 写路径切成用户状态并实施 owner/member 权限，同时保持旧 UI 的过渡读路径和可对账回填。

2026-07-14，M10 已完成。候选内容、处理、去重和文章资格继续共享，生产候选 API 则按认证用户读取和写入 `user_candidate_states`；日报发布记录独立曝光次数，推荐反馈同步个人当前状态，用户 A 的打开、不感兴趣和显式来源屏蔽均不改变用户 B 的候选或推荐选择。旧全局状态没有可靠操作者，因此只进入逐候选管理员复核表，不自动污染任何当前或未来用户；原 `status` 和全局归档任务引用仍保留供兼容与回滚。

来源 CRUD／手动抓取、逻辑站点暂停／恢复／全局安全屏蔽、手动历史回溯和破坏性日报重建均由后端强制 owner 权限。暂停和屏蔽会阻止恢复调度、旧批量抓取及已排队来源的实际网络入口，并暂停未完成回溯；恢复会重新激活图谱和对应运营游标，但候选文章始终不删除。验证全程使用 SQLite、本地确定性数据和规则路径，没有真实互联网、用户数据或 LLM 密钥；PostgreSQL 容器实跑缺口仍未改变。M11 的主要风险是把运营健康、产出效率、基础抓取下限和额外预算拆开建模，不能让低命中统计通过调度公式把任何允许抓取站点的检查机会降为零。

每完成一个里程碑，在本节追加实际结果、与计划差异、遗留问题和下一里程碑风险。最终必须回答：

- 是否真的能够从友情链接图谱发现新站点和历史精品，而不是只显示图谱数据；
- 是否存在任何来源级统计暗中淘汰或封顶单篇文章；
- 低命中来源是否在长时间运行后仍获得非零检查机会；
- 日报、用户状态和反馈是否真正按用户隔离；
- 已发布日报是否在重试、刷新、模型切换和服务重启后保持不变；
- 长尾来源贡献的精品文章是否有可测量证据；
- 哪些兼容字段、开关或旧路由仍需后续清理。

## Context and Orientation

DataArk 是 Go 后端和 Vue 前端组成的单仓库应用。与本计划直接相关的现有位置如下：

- `api/discovery/model.go` 定义发现来源、候选和候选反馈等模型。
- `api/discovery/store.go` 目前包含来源管理、抓取、Feed 解析、同站点爬取、Sitemap 发现、候选入库和发现调度。
- `api/discovery/extractor.go` 提供 DOM Distiller 驱动的正文抽取。
- `api/recommendation/model.go` 定义用户推荐设置、日报、推荐项、反馈、屏蔽和画像。
- `api/recommendation/service.go` 包含候选选择、评分、富化、日报生成、反馈和画像更新。
- `api/recommendation/scheduler.go` 判断用户本地生成时间。
- `api/recommendation/jobs.go` 包含当前 River 日报任务。
- `api/api/controller.go` 注册发现、推荐、反馈和管理 API，并启动调度器。
- `api/bootstrap/database.go` 负责 GORM 自动迁移和 Goose SQL 迁移。
- `api/migrations/` 保存 PostgreSQL 迁移。
- `web/src/views/RecommendationsView.vue` 目前集中承载来源、候选、设置、日报和反馈界面。
- `docs/exec-plans/content-discovery-and-recommendations.md` 和 `docs/exec-plans/recommendation-v2-llm-daily.md` 记录前两阶段设计和已实现范围。

本文档使用下列术语：

“逻辑站点”是一个博客身份，例如 `https://example.com/`，不等于某一个 Feed URL。“抓取端点”是站点的主页、Feed、Sitemap、RSSHub 或归档入口。“种子站点”是用户或管理员主动添加、允许用作图谱起点的站点。“观察站点”是由图谱自动发现、允许以低预算验证和抓取的站点。“图谱边”记录一个站点通过哪个页面、哪个锚文本和何种证据链接到另一个站点。

“文章溯源”记录一篇逻辑文章通过哪些站点、端点、Feed 项、Sitemap 或页面链接被发现。“文章处理状态”描述抓正文、抽取、去重、评估和可推荐资格，不表示用户是否看过。“用户文章状态”描述某个用户是否曝光、打开、深读、归档、不感兴趣或屏蔽，不改变其他用户。

“文章有效性”是硬规则，例如页面是不是文章、正文是否存在、是否安全、是否重复代表项。“文章内容价值”是文章级软评估，例如信息密度、原创性、证据、结构、深度和常青价值。“用户相关性”是某篇有效文章对某个用户的主题、语言、深度和新颖性匹配。三者不得混为一个来源级总分。

“抓取下限”表示在站点仍允许抓取时，无论其历史精品命中率多低，都必须在有限时间内再次检查新文章；“额外预算”可以依据更新频率、图谱可信度、用户兴趣和历史产出效率增加，但不能把下限降为零。

“新鲜池”包含近期发布且合格的文章；“常青池”包含较旧但仍有长期阅读价值的文章；“探索池”包含新站点、长尾站点、新主题或用户尚未形成偏好的合格文章。池是选择标签，不是互斥的物理表；一篇文章可以同时具备多个标签，但在一次日报中只能出现一次。

“不可变日报”表示发布时复制标题、URL、摘要、来源、发布时间、推荐理由、文章评估版本、用户画像版本、策略版本和模型版本。候选表后续变化不得重写历史展示。

## Non-Negotiable Product Invariants

实现中的任何优化都不得违反以下约束。Codex 每个里程碑都要检查相关不变量并用测试固定。

1. 来源平均质量、精品命中率、历史负反馈或图谱层级不得直接使一篇自身合格的文章失去候选资格。
2. 来源统计不得作为文章质量分的乘数、减数、封顶值或隐藏阈值。
3. 每篇文章必须独立完成有效性和内容价值判断；同站点文章不能共享一个最终质量结论。
4. 一个低命中率但仍在更新的允许抓取站点必须获得非零新文检查下限；未完成的历史回溯必须获得非零批次下限。
5. 只有明确屏蔽、robots 禁止、安全风险、持续不可访问达到运维暂停策略、或者可靠地判定为非博客，才允许停止抓取或图谱扩展。因质量普通只能减少额外预算。
6. 每个站点和文章的发现路径必须可追踪；同一文章的后续发现不能覆盖已有溯源。
7. 用户行为不得写入共享候选状态；用户 A 的打开、不感兴趣、归档或深读不得影响用户 B。
8. 单篇反馈不自动产生来源级奖惩；来源级硬过滤必须来自用户明确选择或管理员的安全／运维动作。
9. 重复、屏蔽、非文章、安全和最低正文要求是硬规则；为凑足 `N` 不得放宽。
10. `N` 是目标数量。合格候选少于 `N` 时发布 `M < N` 并记录可解释的短缺原因。
11. 已发布日报不可原地删除、重排或重写；反馈和候选变化只影响未来日报或显式追加的补充项。
12. 远程 LLM、向量数据库或某个外部提供商不可用时，系统仍能发现、处理并生成规则版日报。
13. 图谱遍历必须有域名、深度、数量、速率、robots、SSRF、响应大小和重试边界，不能退化为无边界爬虫。
14. 任何调度、重试和迁移必须幂等；服务重启不能制造重复站点、重复文章、重复日报或重复反馈权重。

## Scope

本计划包含：

- 从种子站点和已确认观察站点中识别友情链接；
- 有界、可恢复、可解释的博客图谱遍历；
- 逻辑站点、抓取端点和图谱边的数据建模；
- Feed、Sitemap、归档页和站内文章链接驱动的新文发现及历史回溯；
- 安全抓取、条件请求、逐端点调度、失败退避和域名限流；
- 正文抓取、文章页识别、规范化、内容版本和处理状态；
- exact、canonical 和近似重复；
- 版本化文章级质量评估与本地降级；
- 共享文章池与用户交互状态分离；
- 公平抓取预算和长尾来源非零探索；
- 新鲜、常青、探索候选池；
- 每日目标 `N`、多样性、冷却再推荐、解释和不可变快照；
- 幂等反馈、撤销、偏好衰减、冷启动和显式屏蔽；
- 来源图谱、抓取健康、候选处理、日报和反馈的前端；
- 指标、日志、端到端测试、迁移和灰度清理。

本计划不包含：

- 从搜索引擎或整个互联网进行无边界来源发现；
- 依据博客平均质量整体淘汰来源；
- 把一个用户的个人评价变成全局文章质量真相；
- 需要大规模多用户数据的协同过滤；
- 依赖某一个 LLM 提供商才能工作的主路径；
- 内容全文搜索、移动客户端或复杂通知渠道的全面重做；
- 以追求算法复杂度为目标的 Bandit 或强化学习。先实现显式探索配额和可测量闭环，再决定是否升级算法。

## Codex Continuous Execution Protocol

Codex 必须按以下协议持续执行：

1. 每次工作会话开始时运行 `git status --short` 和 `git rev-parse --show-toplevel`，确认处于预期仓库；阅读 `PLANS.md`、`AGENTS.md`、`CONVENTIONS.md`、本文档以及上一个未完成里程碑涉及的代码。
2. 把当前代码与本文档重新对齐。若仓库在计划编写后发生变化，在 `Surprises & Discoveries` 中记录，并对后续步骤做最小必要修订。
3. 在修改前运行相关基线测试；若基线已经失败，记录原始失败，不把既有失败误归因于本里程碑。
4. 一次只推进一个最小但完整的里程碑。先完成数据和后端行为，再暴露 API，最后做 UI；不要同时留下多个半完成主路径。
5. 每个外部依赖必须有接口和确定性测试替身。自动测试不能要求真实互联网、真实 OpenAI 密钥或真实用户数据。
6. 每完成一个可观察行为，先运行聚焦测试，再运行模块测试，最后运行仓库级构建。测试失败时立即修复，不把已知失败带到下一里程碑。
7. 更新本文档中的 `Progress`、`Surprises & Discoveries`、`Decision Log` 和 `Outcomes & Retrospective`，写明实际命令、结果、关键指标和提交哈希。
8. 形成可恢复的提交后，立即继续下一个未完成里程碑，不询问用户“是否继续”。仓库要求 PR 单提交时，在提交 PR 前把该 PR 的工作压缩为一个提交并 rebase；不得创建 merge commit。
9. 遇到可逆的实现选择，优先选择更安全、可观测、兼容旧数据的方案并记录。只有缺失凭据、基础设施不可用、会不可逆删除数据或需求发生直接冲突时才暂停。
10. 不以“代码编译”作为完成标准。每个里程碑必须通过本节指定的行为测试，并能由另一个执行者从本文档复现。
11. 不在日志、测试快照或错误信息中泄露正文全文、访问令牌、Cookie、用户隐私或模型密钥。
12. 特性开关启用前必须具备回滚路径；旧路径只能在新路径经过数据对账和端到端验收后清理。

建议为此工作使用一个从最新 `dev` rebase 出来的功能分支。里程碑结束时至少形成一个本地检查点提交；如果多个检查点最终进入同一个 PR，则在提交 PR 前按仓库约定压缩为一个提交。

## Implementation Strategy

实施采用“增量结构、双路径迁移、逐步切流”的方式。不要直接重命名或删除已有表，也不要先改 UI 再补数据语义。

第一阶段建立测试夹具、站点图谱和持久任务基础；第二阶段把 Feed、新文、历史回溯、正文和去重串成可恢复的文章流水线；第三阶段完成文章级评估和用户状态隔离；第四阶段替换推荐、反馈和不可变日报；最后补齐 UI、指标、灰度和旧路径清理。

在 v3 完整切流前，现有页面和推荐 v2 应继续可用。必要时增加下列特性开关，名称可根据仓库配置习惯调整，但语义必须保持：

- `DISCOVERY_V3_ENABLED`：启用逻辑站点、图谱和新文章流水线的读路径。
- `DISCOVERY_BLOGROLL_ENABLED`：允许自动扫描和扩展友情链接。
- `DISCOVERY_BACKFILL_ENABLED`：允许执行历史回溯作业。
- `RECOMMENDATION_V3_ENABLED`：使用新候选池、冷却和不可变快照。
- `DISCOVERY_V3_DUAL_WRITE`：迁移期间同时维护旧候选字段和新结构。

开关默认值应在迁移和测试阶段保持保守；部署文档必须解释启用顺序和回滚影响。

## Milestone M0 — Baseline and Deterministic Test World

目标是让所有后续网络、时区、任务和模型行为都能在本地确定性复现。这个里程碑不改变生产行为。

先记录执行开始时的 `dev` 提交哈希、Go 版本、Node 版本、数据库模式和基线测试结果。创建 `api/discovery/testdata/sites/` 或等价目录，保存一组小型 HTML、RSS/Atom、Sitemap 和 robots 夹具。测试世界至少包含：

- 种子站点 A 的友情链接页指向 B、C；
- B 的 Blogroll 又指向 A 和 C，形成循环；
- C 包含普通外链、广告链接和非博客链接，用于验证误识别；
- B 有大量普通文章以及至少三篇独立高质量文章，其中一篇只存在于旧 Sitemap，一篇只存在于分页归档；
- 同一文章同时出现在 Feed、Sitemap 和另一个站点的链接中；
- 一个端点支持 `ETag`、`Last-Modified` 和 `304 Not Modified`；
- 一个路径被 robots 禁止；
- 一个端点超时或返回 500；
- 一个 URL 发生同域和跨域重定向；
- 一个页面是标签页或登录页而不是文章；
- 两个用户具有不同的时区、反馈和屏蔽规则。

在 `api/discovery` 中引入最小的测试边界：

- `Clock` 提供 `Now()`，生产实现使用真实时间，测试使用可推进时钟；
- `HTTPFetcher` 接收受限请求并返回状态码、最终 URL、响应头和受限正文；
- `JobEnqueuer` 提供幂等入队能力，测试实现可以同步执行或记录调用；
- 模型或富化接口保留本地确定性替身。

这些接口只用于隔离不确定性，不要在本里程碑重写业务。为现有来源抓取和推荐生成添加基线回归测试，确保后续迁移能区分旧行为和新行为。

完成标准：

- 所有夹具由 `httptest.Server` 或等价本地服务器提供，不访问互联网；
- 测试可以推进时间、制造重启和重复执行；
- `cd api && go test ./...` 通过，或者既有失败被明确记录；
- `cd web && npm run build` 通过；
- 本文档记录基线提交和测试输出摘要。

## Milestone M1 — Additive Data Model and Compatibility Migration

目标是建立正确的数据语义，同时不破坏已有来源、候选、推荐历史和用户。

新增 Goose 迁移，优先使用 `api/migrations/000003_blog_discovery_v3.sql`；如果单个迁移过大，可以按明确依赖拆成连续编号，但每个迁移都必须有升级、回退说明和自动测试。SQLite 测试环境使用对应 GORM 模型或测试迁移，确保两种数据库语义一致。

新增逻辑站点表 `discovery_sites`。最低字段包括：

- 稳定 ID；
- 规范根 URL、规范主机键和展示名称；
- 状态：`seed`、`observing`、`active`、`paused`、`blocked`、`non_blog`；
- 发现方式和图谱最短深度；
- 抓取许可／robots 状态；
- 首次发现、最近仍被引用、最近发现文章和最近验证时间；
- 下一次图谱扫描时间；
- 运维暂停原因；
- 创建和更新时间。

不要添加可被误用为硬过滤的单一“来源质量分”。来源统计在后续里程碑使用独立、命名明确的健康和产出字段或聚合表。

保留现有 `discovery_sources`，把其语义收窄为抓取端点，并增加 `site_id`、端点类型、优先级、上次尝试、上次成功、下次到期、退避状态和内容验证器。现有来源迁移为一个逻辑站点及至少一个端点；迁移应通过规范主机和根 URL 合并明显相同的记录，但不在自动迁移中做激进跨域合并。

新增：

- `discovery_site_edges`：`from_site_id`、`to_site_id`、来源页面、锚文本、关系类型、证据、置信度、首次／最近发现、是否仍活跃和图谱深度；
- `discovery_candidate_provenances`：候选、站点、端点、发现方法、原始 URL、来源页面、首次／最近发现；
- `discovery_fetch_runs`：抓取作业的开始、结束、状态、HTTP 结果、是否未变化、新增／重复／失败计数和错误分类；
- `discovery_backfill_states`：站点、策略、游标、批次、状态、覆盖区间、上次成功和完成原因；
- `discovery_article_assessments`：候选、内容版本、评估器和策略版本、维度分、总体分、置信度、理由和创建时间；
- `user_candidate_states`：用户、候选、首次曝光、最近曝光、打开、深读、归档、当前显式反馈、撤销和更新时间。

扩展候选，至少能表达：

- `processing_state`：`discovered`、`fetch_pending`、`fetched`、`extract_pending`、`extracted`、`dedupe_pending`、`assessment_pending`、`ready`、`failed`；
- `eligibility_state`：`unknown`、`eligible`、`ineligible`、`review`；
- 结构化不合格原因；
- 最终 URL、canonical URL、规范 URL；
- 正文内容版本、正文最后变化时间；
- 代表候选或重复簇；
- 首次和最近发现时间。

保留旧 `status` 字段用于迁移兼容，但新增代码不得再把个人阅读行为写入该字段。

扩展日报和推荐项，使其能够保存：

- 目标数量、实际数量、用户时区、策略版本、状态、发布时间和结构化短缺原因；
- 标题、URL、摘要、作者、来源展示、发布时间、推荐理由、池类型、探索原因、文章评估版本、画像版本和模型版本的快照；
- 项目是否属于原始发布或后续补充；
- 审计版本，但不允许覆盖旧版本。

为后续冷却再推荐移除或替换“用户与候选永久唯一”的约束。迁移时先增加新的历史／冷却表达和兼容查询，再删除旧唯一约束，避免同时允许重复。保留“同一日报内同一候选唯一”。

为全局管理动作增加明确所有者能力。优先在用户模型增加 `role` 或 `is_owner`，把现有用户名 `admin` 和首个部署所有者安全迁移为 owner；不要依赖前端隐藏按钮作为权限。

完成标准：

- 在现有数据库快照上执行升级后，来源、候选、推荐历史和反馈数量不减少；
- 重复执行应用启动和迁移不会产生重复站点、边、溯源或日报；
- PostgreSQL 与 SQLite 集成测试都验证唯一约束、外键和默认值；
- 可以从旧来源记录查询到对应逻辑站点和端点；
- 旧 API 在 v3 开关关闭时仍能工作；
- 回退说明明确哪些新增数据会保留、哪些功能必须先关闭。

## Milestone M2 — Shared Durable Job Runtime

目标是用同一个持久、幂等、可恢复的作业系统承载所有周期工作，替代发现模块中的全局串行 ticker 和推荐模块独占的任务运行时。

把 River 客户端生命周期、数据库连接、作业注册和关闭逻辑从 `api/recommendation/jobs.go` 抽到 `api/jobqueue/` 或等价共享包。推荐模块只注册日报作业，不再拥有运行时。

至少定义以下作业类型：

- `discovery_fetch_source`：抓取一个端点；
- `discovery_scan_blogroll`：扫描一个逻辑站点的友情链接；
- `discovery_backfill_site`：执行一个有界历史回溯批次；
- `discovery_process_candidate`：抓正文、抽取、去重和触发评估；
- `recommendation_generate_daily`：生成指定用户、指定本地日期的日报；
- 可选的 `recommendation_supplement_daily`：只在明确不足且有新合格候选时追加缺少项目。

每种作业的参数都必须只包含稳定 ID、目标日期、内容版本或策略版本，不能把大正文塞入队列。唯一键必须使同一目标的重复入队安全。作业开始和完成应写结构化状态，崩溃后可以重试。

PostgreSQL 使用 River 持久化。SQLite、开发和单元测试提供同步或内存实现，但业务代码只依赖 `JobEnqueuer`。启动时查询已到期端点、未完成回溯、待处理候选和漏生成日报并补入队；不要等待下一个完整周期。

完成标准：

- 重复启动两个调度器不会为同一端点、候选或用户日期执行两次副作用；
- 在作业中途模拟进程停止，重启后能够继续且不重复文章、边或日报；
- 单个来源失败不会阻塞其他来源；
- 推荐 v2 在开关关闭时仍能通过共享运行时生成；
- `go test -race` 对任务注册、同步降级和幂等测试通过。

## Milestone M3 — Safe Fetching, Validators, Robots and Per-Endpoint Scheduling

目标是把 HTTP 访问变成统一、安全、可度量的基础能力，并真正使用已有的端点调度字段。

从 `api/discovery/store.go` 拆出抓取器、端点存储和调度策略，建议文件为：

- `api/discovery/fetcher.go`
- `api/discovery/robots.go`
- `api/discovery/source_store.go`
- `api/discovery/scheduler.go`
- `api/discovery/fetch_service.go`

实际文件名可以调整，但职责必须分离。

`HTTPFetcher` 的生产实现必须：

- 使用明确的 DataArk User-Agent 和可配置超时；
- 仅允许 HTTP/HTTPS；
- 在初始 URL 和每次重定向后重新执行 SSRF／私网地址检查；
- 限制重定向次数、响应大小和允许的 Content-Type；
- 按域名限制并发和最小请求间隔；
- 缓存并遵守 robots 规则，记录允许、禁止、无法获取和过期状态；
- 对 Feed、Sitemap、HTML 分别设置合理正文大小上限；
- 不携带用户 Cookie、认证头或浏览器会话；
- 读取端点的 `ETag` 和 `Last-Modified`，发送条件请求；
- 将 `304` 记录为成功且未变化，不重复解析、正文抓取或评估；
- 记录最终 URL、状态码、响应验证器和错误类别。

逐端点调度使用 `next_fetch_at`，不再每隔固定周期串行抓取所有来源。成功后根据端点类型、实际更新间隔和配置计算下次时间；失败使用带上限和抖动的指数退避；成功后清零连续失败。新端点立即安排首次抓取。服务启动时补做已经到期的端点。

初始可配置默认值应体现“低命中不等于停抓”：

- 种子或活跃 Feed 的最大检查间隔默认不超过 24 小时；
- 观察或低历史命中站点的新文检查最大间隔默认不超过 7 天；
- 长期无更新但仍可访问的站点可降到默认 30 天；
- 内容产出效率只能增加额外预算或在上述范围内改变间隔，不能设置为永不抓取；
- robots 禁止、明确屏蔽、可靠非博客或人工暂停可以停止。

这些数值必须可配置，并在后续真实运行数据出现后调整。

完成标准：

- 夹具端点第一次返回 `200 + ETag`，第二次收到条件头并返回 `304`；候选和评估次数不增加；
- robots 禁止路径没有发送正文请求；
- 重定向到私网地址被拒绝；
- 超大响应和错误 Content-Type 被安全截断或拒绝；
- 连续失败产生退避，成功后恢复；
- 一个低命中观察站点的 `next_fetch_at` 始终有限，不会被设置为 `NULL` 或无限远；
- 域名限流和并发在确定性测试中可验证。

## Milestone M4 — Blogroll Detection and Bounded Site Graph

目标是从种子站点发现新的博客来源，并保留完整、可解释的图谱证据。

新增职责清晰的组件，建议为：

- `BlogrollDiscoverer`：从 HTML 中提取可能的友情链接和证据；
- `SiteClassifier`：判断目标更像博客、未知站点还是明显非博客；
- `SiteGraphService`：规范化站点、写边、计算深度和安排观察任务。

友情链接识别不能只依赖一个固定 URL。至少识别：

- `<link rel="...">` 或锚点中的 `friend`、`me`、`blogroll` 等关系；
- 页面标题、导航和区块标题中的“友情链接”“友链”“邻居”“朋友”“推荐博客”“Blogroll”“Friends”“Links”“People I read”等中英文模式；
- `/links`、`/friends`、`/blogroll`、`/roll` 等常见独立页面；
- 由多个外部博客链接组成的稳定列表或区块；
- 首页、About、Links 页面中带明确上下文的外部链接。

每条候选边保存来源页面、锚文本、HTML 上下文摘要、检测规则、置信度、首次和最近发现时间。正文全文不写入日志或边表。

图遍历采用有界广度优先策略。初始默认值：

- 最大自动扩展深度 3；
- 每站点每次最多接受 50 个候选外部站点；
- 每日最多激活 100 个新观察站点；
- 同一规范站点只创建一个节点；
- 循环只更新边，不重复递归；
- 同一根域名和常见 `www` 变体合并，跨独立域名不自动合并；
- 聚合站、社交平台、登录页、商城和纯导航站标记为未知或非博客，不因“内容普通”标记非博客。

人工种子保存后立即安排站点验证、端点发现和 Blogroll 扫描。由友情链接发现的站点自动进入 `observing`，获得低预算抓取，不要求人工逐个批准；管理者可以升级为种子、暂停、屏蔽或禁止继续向下一层扩展。

多个独立已知站点指向同一目标时，提高验证和图谱扫描优先级，但这只表示“值得探索”，不得转化为文章质量分。

提供只读 API 查询：

- 一个站点的直接入边、出边和证据；
- 从最近种子到该站点的最短发现路径；
- 图谱深度、独立入链站点数和最近仍被引用时间；
- 当前状态以及停止／暂停原因。

完成标准：

- A → B → C → A 的夹具只生成三个站点和预期边，不无限递归；
- 普通正文外链不会仅因存在就被当作友情链接；
- 新 B 自动进入观察状态并安排端点发现；
- B 的历史文章质量数据完全不参与 B 是否被识别为图谱节点；
- 用户可以通过 API 得到 B 的“从 A 的友情链接页发现”证据和最短路径；
- 达到深度和每日上限时，剩余目标处于待处理状态而不是静默丢失。

## Milestone M5 — Endpoint Discovery and Recent Article Ingestion

目标是把逻辑站点与可抓取端点连接起来，并可靠发现最新文章。

站点验证后依次尝试：

- HTML `<link rel="alternate">` 中的 RSS、Atom 和 JSON Feed；
- 常见 Feed 路径；
- 已配置 RSSHub 路由；
- `robots.txt` 或页面中的 Sitemap；
- Sitemap index 和 URL set；
- 首页或文章列表中的站内文章链接。

每一个发现的端点归属于逻辑站点，使用 URL 规范化去重并立即安排首次抓取。不要把 Feed URL 当作独立博客站点。

重构 Feed 入库，使每个 Feed item 先形成“发现事件”，再解析到逻辑文章。已有候选冲突时：

- 更新 `last_seen_at` 和必要的元数据；
- 新增或更新 `discovery_candidate_provenances`；
- 不覆盖首次发现来源；
- 不因另一个来源的标题或摘要为空而清空已有值；
- 只有更可信或更新版本的数据才替换展示字段；
- 正文版本未变化时不重复安排评估。

Feed 中缺少正文并不直接成为不合格；它应进入正文抓取队列。站点链接候选不能再以 URL 永久充当标题并直接参与推荐。

完成标准：

- 一个站点可同时拥有主页、Feed 和 Sitemap 端点；
- 同一文章从 Feed 和 Sitemap 发现只保留一个逻辑候选和两条溯源；
- 新端点保存后立即抓取；
- 304 或重复 Feed item 不重复处理；
- Feed 只有标题时，候选状态为待正文抓取而非 ready；
- 现有来源数据在双写阶段仍可由旧 UI 查看。

## Milestone M6 — Persistent Historical Backfill

目标是逐步发现 RSS 当前窗口之外的少数高价值历史文章。

为每个站点建立可恢复的回溯策略和游标。按可靠性优先使用：

1. Sitemap index 及其子 Sitemap；
2. 站点归档页、年份／月份页面；
3. 明确的文章列表分页；
4. 从已知文章和导航中发现的站内文章链接。

每个回溯作业只处理有界批次，保存游标、最近覆盖日期、已查看 URL 数、发现文章数、重复数、失败数和完成原因。大型博客不能在一个事务或一个进程生命周期内一次抓完。作业重试从持久游标继续，重复批次幂等。

调度策略必须保证：

- 新观察站点先获得小批量样本，以验证结构和抓取安全；
- 已确认博客持续推进历史覆盖；
- 高产出站点可以获得额外批次；
- 低历史精品命中率只能减少额外批次，不能让未完成回溯永久停滞；
- robots 禁止、管理员暂停、可靠非博客、连续不可恢复错误或明确完成才允许停止；
- 旧文章保留原始发布时间；未知发布时间使用发现时间但标记置信度，不伪装成新文。

提供覆盖状态 API：策略、游标、最早覆盖日期、估计完成度、最近批次和停止原因。

完成标准：

- B 的 RSS 只有最近普通文章，但旧 Sitemap 中的精品文章仍被发现；
- 另一篇只在分页归档中的旧文章也被发现；
- 中途停止并重启后从同一游标继续；
- 重跑一个批次不制造重复；
- 低命中来源在推进测试时仍按最大等待间隔获得下一批；
- 完成状态有明确证据，不能仅因若干批次没有精品文章就判定完成。

## Milestone M7 — Article Fetch, Extraction and Eligibility Pipeline

目标是把现有 `ExtractArticle` 真正接入每一篇候选的常规处理路径。

将候选处理拆成可重试步骤，每一步更新 `processing_state` 并记录错误类别：

1. 获取最终文章页面；
2. 验证响应类型和安全规则；
3. 使用 DOM Distiller 和元数据回退抽取标题、作者、正文、摘要、发布时间、canonical 和语言；
4. 判断是否为文章页；
5. 计算正文版本和基本统计；
6. 安排去重；
7. 安排文章级评估；
8. 在所有硬条件满足后标记 ready／eligible。

文章有效性硬规则至少包括：

- 页面可访问且允许抓取；
- 是文章而非首页、分类、标签、搜索、登录、评论、纯导航或错误页；
- 有可用标题；
- 正文达到可配置的最低有效长度，或者被明确标记为允许的短文类型；
- 语言可识别；
- 没有被共享安全规则拒绝；
- 不是重复簇的非代表项；
- 处理版本完整。

正文抽取失败的候选进入可重试失败或人工复核状态，不直接参与正常推荐。只有摘要但没有正文的候选可以保存在库中，但默认不 eligible；若产品以后允许“摘要候选”，必须是显式策略而非隐式降级。

正文哈希变化时增加内容版本并重新抽取、去重和评估；页面未变化时只更新最近看到时间。

完成标准：

- Feed 发现的每个候选最终都进入明确的 ready、review 或 failed/ineligible 状态；
- 标签页和登录页被文章有效性规则排除；
- 正文抽取结果写入候选并成为评估输入；
- 相同 HTML 重试不创建新内容版本；
- 正文实质更新会创建新版本并触发后续步骤；
- LLM 完全关闭时流水线仍能完成。

## Milestone M8 — Article Identity, Dedupe and Provenance

目标是把“同一文章”从 URL 字符串提升为稳定内容身份。

按下列层级建立身份：

- 原始发现 URL；
- 去除跟踪参数、Fragment、默认端口和已知无意义参数后的规范 URL；
- 重定向后的最终 URL；
- 页面声明的 canonical URL；
- 规范正文 exact hash；
- 基于标题、正文指纹或向量的近似重复簇。

exact URL、canonical 和正文完全相同的记录必须合并为一个逻辑文章或一个代表候选，所有发现路径保留。近似重复可以保留成员，但只允许代表项进入同一次推荐；代表项选择应考虑正文完整度、canonical 可信度、原始作者站点、可访问性和发布时间，而不是来源平均质量。

转载或镜像关系无法确定时，不要删除数据；建立重复簇并记录代表选择理由。用户“太重复”反馈应创建重复审查信号，必要时修正簇，而不是惩罚来源或主题。

完成标准：

- 同一文章通过三个入口发现只产生一个可推荐代表和三条溯源；
- URL 参数变化、HTTP 重定向和 canonical 别名被正确聚合；
- 两篇正文近似但并非完全相同的文章进入同一簇且只推荐一篇；
- 代表项变化不会丢失旧推荐快照；
- “太重复”反馈不修改来源权重；
- eligible 池中的 exact 和 canonical 重复数为零。

## Milestone M9 — Versioned Article-Level Assessment

目标是以文章本身为评价粒度，建立可解释、可版本化且不依赖来源平均质量的内容评估。

定义 `ArticleAssessor`，输入只能包含当前文章版本的标题、正文、作者、发布时间、语言、结构化元数据和必要的重复信息。不得输入：

- 来源历史平均分；
- 来源精品命中率；
- 来源级自动推导的正负权重；
- 图谱层级作为质量指标；
- 其他文章的用户负反馈汇总。

评估输出至少包含：

- 信息密度；
- 原创性或增量信息；
- 论证／说明完整性；
- 证据、案例或可验证依据；
- 结构与可读性；
- 内容深度；
- 常青价值；
- 总体文章质量；
- 置信度；
- 简短、可展示的理由；
- 评估器名称、版本、规则版本和时间。

保留确定性规则评估器作为始终可用的基线。现有 OpenAI-compatible 富化可以作为增强评估器，但超时、配额或解析失败时必须回退。不同版本评估并存；候选记录只引用当前生效版本，历史日报保存当时版本。

文章是否 eligible 使用文章级有效性和文章级最低门槛。低置信度进入 review 或探索前的待处理池，不以来源历史表现替代判断。

必须添加一个防回归测试：站点 B 有 100 篇文章，其中 97 篇普通、3 篇高质量；三篇精品的文章评估与同等正文从种子站点 A 抓取时结果一致，并且能超过 A 中质量普通的文章。任何来源聚合字段加入 `ArticleAssessor` 输入都应使测试或静态边界失败。

完成标准：

- 相同正文在不同来源下得到相同文章级评估；
- 低命中来源中的精品可以成为 eligible；
- 来源统计不出现在评估器输入或质量计算路径；
- 规则版和可选 LLM 版都保存版本及理由；
- LLM 故障时仍产生规则评估且不阻塞日报；
- 重新评估只创建新版本，不改写历史日报。

## Milestone M10 — Shared Content, Per-User Interaction and Permissions

目标是消除全局 `read/ignored/archived` 对多用户的污染。

所有曝光、打开、已读、深读、不感兴趣、个人归档意图都写入 `user_candidate_states` 或对应用户事件，不再修改候选共享 `status`。共享候选只保留处理状态、全局安全／质量资格和管理员归档结果。

迁移旧状态时采用保守策略：

- 旧 `read` 可以迁移为已有用户的打开／已读状态，但必须记录迁移来源；
- 旧 `ignored` 不应自动成为所有未来用户的屏蔽；若无法确定操作者，只保留为旧兼容信息并进入管理员复核；
- 旧 `archived` 的全局任务关系保留，同时为已知操作者建立个人归档状态；
- 完成数据对账前不删除旧字段。

建立权限：

- owner 可以添加、暂停、屏蔽逻辑站点，管理全局安全拒绝，启动手动抓取／回溯和查看运维错误；
- member 可以查看共享候选、管理自己的设置、反馈、屏蔽和个人归档；
- 普通用户不能删除共享来源、把候选全局忽略或破坏性重建日报；
- 后端必须强制验证，不能只靠前端隐藏。

完成标准：

- 用户 A 打开或不感兴趣后，用户 B 的候选状态和选择不变；
- 用户 A 显式屏蔽来源只影响 A；
- owner 暂停抓取不会删除已发现文章；是否继续向用户推荐已有文章由暂停原因和屏蔽范围决定；
- 未授权来源管理请求返回明确错误；
- 旧数据迁移数量可对账并可回滚读路径。

## Milestone M11 — Fair Crawl Budgets and Candidate Inventory

目标是提高资源效率，但严格保留长尾来源的发现机会。

为站点维护名称明确的运营统计，不创建单一来源质量分：

- 图谱发现可信度：独立入链站点数、最短种子距离、人工种子状态；
- 抓取健康：成功率、连续失败、响应未变化率、解析成功率、最近更新；
- 内容产出效率：每百篇 eligible 数、重复率、正文抽取率、用户正反馈文章数；
- 历史覆盖：已处理 URL、最早覆盖日期、回溯状态。

调度器以基础下限加额外预算工作：

- 每个允许抓取且可能更新的站点有明确的最大空闲时间；
- 每个未完成回溯站点有明确的最大批次等待时间；
- 更新频率快、多个种子入链、用户明确关注或近期产出精品可增加预算；
- 低 eligible 比例、重复多或更新慢可以减少额外预算；
- 任何产出统计都不能把基础下限降为零；
- 暂停和停止必须记录非质量原因及恢复条件。

建立候选库存视图：

- eligible 新鲜候选数；
- eligible 常青候选数；
- eligible 探索候选数；
- 每个用户硬过滤后估计可用数；
- `inventory_days = 独立合格候选数 / daily_limit`；
- 少于 7 天预警，少于 3 天严重预警，初始阈值可配置。

探索标签基于“新站点、较少曝光站点、长尾站点、新主题或用户尚未形成偏好的主题”，不是“质量未知就直接推荐”。所有探索文章先通过文章级门槛。

完成标准：

- 模拟数月调度后，低命中 B 仍在其最大空闲时间内被检查并继续回溯；
- 高产出来源获得更多额外预算，但不会占满所有作业；
- 管理 API 能解释某站点下次抓取时间由基础下限还是额外预算决定；
- 库存指标区分新鲜、常青和探索；
- 来源历史平均表现没有出现在文章 eligible 查询中。

## Milestone M12 — Recommendation v3 Selection

目标是从合格文章中生成可解释、可重复、文章级竞争的每日推荐。

推荐选择分为候选生成、文章／用户评分和多样性重排。

候选生成只接受：

- `processing_state = ready`；
- `eligibility_state = eligible`；
- 当前代表项；
- 未命中用户硬屏蔽；
- 不属于同日报已选重复簇；
- 满足语言和安全规则；
- 满足再推荐冷却。

评分只使用：

- 文章级内容价值；
- 用户对主题、风格、深度、语言的偏好；
- 发布时间、新鲜度和常青价值；
- 与用户近期已读／已推荐内容的新颖性；
- 明确的用户来源收藏可以作为个人相关性，但自动文章反馈不能转换为来源整体加分；
- 探索和多样性需要的标签。

来源平均质量、来源精品率和全局来源声誉不得进入评分或封顶。来源仅用于用户显式屏蔽／收藏、探索标签和多样性约束。

每日目标数量语义：

- 合格候选不少于 `N` 时发布恰好 `N`；
- 少于 `N` 时发布全部合格的 `M`，记录按原因分类的排除数量；
- 不得因不足而放宽重复、屏蔽、有效性、最低正文或文章级质量硬门槛；
- 页面展示“目标 N，实际 M”。

探索配额：

- 默认沿用 15%，可由用户设置；
- 当 `N >= 5` 且存在合格探索文章时至少 1 篇；
- 优先覆盖新站点、长尾站点、新作者、新主题和相邻主题；
- 每篇探索文章记录具体原因；
- 探索文章参与相同文章级质量门槛。

初始软多样性上限：

- 同一来源不超过日报的 30%；
- 同一主主题不超过 40%；
- 同一作者设置合理上限；
- 同一重复簇最多一篇；
- 兼顾新鲜与常青、短文与深度文章。

只有软约束可在候选不足时按固定顺序放宽，并在日报记录具体放宽项。硬约束永不放宽。

再推荐规则：

- exact、canonical 和近似重复代表关系不重复作为独立文章；
- 已归档、已深读或已明确正负评价的文章默认不再进入发现日报；
- 仅曝光未打开的文章经过可配置 60–90 天冷却后可再次竞争；
- 正文发生实质更新时可作为“文章已更新”重新出现，并显示标记；
- 旧的用户／候选永久唯一约束不能继续代替这些规则。

所有选择使用固定时钟、稳定排序和显式随机种子，保证同一用户、日期、策略版本和候选快照在重试时得到相同结果。

完成标准：

- 低命中 B 的高质量文章可以在同等用户相关性下超过高命中 A 的普通文章；
- `N=10` 且有足够候选时恰好选 10；
- 只有 7 篇合格时只选 7 并给出原因；
- 有合格探索候选时满足探索配额；
- 来源和主题软上限按预期生效并能解释放宽；
- 仅曝光文章冷却后可重现，明确反馈文章默认不重现；
- 硬过滤违规、同簇重复和来源屏蔽违规均为零。

## Milestone M13 — Feedback, Preference Learning and Cold Start

目标是让用户反馈含义清晰、可撤销且只在适当粒度影响未来推荐。

为每个推荐项维护当前有效反馈，同时保留不可变事件历史。支持：

- `valuable`：文章级软正反馈；
- `not_interested`：文章及相似主题／风格的软负反馈；
- `deep_read`：强正参与信号；
- `too_repetitive`：重复簇或相似内容问题；
- `block_source`：用户显式来源硬过滤；
- `reduce_topic`、`reduce_style`：明确范围的软偏好；
- 撤销和改选。

行为规则：

- 同一反馈重复提交幂等；
- 改选时关闭旧的当前状态并创建新事件；
- 撤销只改变当前有效状态，不删除历史；
- “太重复”进入重复聚类修正，不降低来源质量；
- “不感兴趣”不自动屏蔽来源；
- 文章反馈更新主题、风格、深度和内容表示偏好，不自动更新来源整体权重；
- 只有显式 `block_source` 产生来源级硬规则；
- 打开是弱信号，深读和归档是强信号；仅曝光未点击不直接视为负反馈；
- 软偏好随时间衰减，显式屏蔽在撤销前不衰减；
- 反馈从下一次未发布日报开始生效，不重排已发布日报。

首次启用推荐时提供可跳过的轻量设置：主题、语言、短文／长文或深度偏好、探索比例和可选的明确来源收藏。跳过时使用文章质量、新鲜度和多样性默认策略，不借用其他用户个人数据。

提供“重置个人推荐偏好”能力。重置不删除历史日报和原始事件，但新建一个画像版本并停止旧软权重生效。

完成标准：

- 重复点击一次和多次的画像结果一致；
- 改选和撤销有明确当前状态；
- 对 B 的一篇文章点“不感兴趣”后，B 的另一篇不同主题精品仍可被选择；
- 显式屏蔽 B 后，B 对该用户的未来候选为零，但其他用户不受影响；
- “太重复”只影响簇和相似内容；
- 时间推进后软反馈权重按策略衰减；
- 用户可在 UI/API 看到并撤销当前反馈。

## Milestone M14 — Immutable Daily Digests and Timezone-Correct Scheduling

目标是让每日推荐成为按用户本地日期发布、可审计、可恢复的不可变产品。

所有“今日”查询先读取用户设置中的 IANA 时区，再计算本地日期。调度器、生成服务、API 和历史页面必须使用同一日期函数。用户修改时区只影响未来日报；历史日报保留生成时区。

日报生命周期：

- `draft`：候选选择和验证尚未发布，可安全重算；
- `published`：事务内冻结快照，之后不可删除或原地重排；
- 可选 `supplemented`：候选不足后，新候选到达时只追加缺少项目，并记录补充时间和策略，不修改原项目；
- `failed`：记录可重试错误，不伪装成空日报。

同一用户和本地日期只有一个正式日报身份。发布事务复制所有展示字段和版本。刷新历史不能从当前候选重新拼接标题或摘要。

移除普通用户可见的破坏性“重新生成今日”。后台重试同一日报时：

- 已发布则直接返回现有快照；
- draft 可按同一策略安全重算；
- failed 可重试；
- 任何补充都只追加，不能删除原项目或反馈；
- 确需人工修订时创建新审计版本，旧版本仍可访问。

调度器每分钟或通过持久计划查询到期用户。服务启动后，对已经过本地生成时间但当日尚无日报的用户立即补做。日报只消费 ready/eligible 池，不在生成临界路径同步等待大量网页抓取或模型调用。已有文章评估不足时使用规则版结果；外部模型故障不得阻止发布。

完成标准：

- 两个不同时区用户在各自本地日期得到正确日报；
- 服务在生成时间后重启会补生成一次；
- 重复入队、刷新和手动重试返回字节语义一致的已发布项目；
- 修改候选标题或当前评估不会改变历史快照；
- 反馈不会被重建删除；
- 候选不足时显示目标、实际和短缺原因；
- LLM 下线时仍发布规则版日报并记录降级；
- 不再存在普通用户可调用的破坏性重建路由。

## Milestone M15 — API and Frontend Experience

目标是让新的来源图谱、文章粒度和日报语义对用户可见，不把复杂状态隐藏在后端。

把 `web/src/views/RecommendationsView.vue` 按现有前端约定拆成可维护组件或组合式模块，至少覆盖：

- 种子和观察站点列表；
- 站点详情、发现路径和图谱关系；
- 抓取端点健康、下次抓取、最近错误和条件请求状态；
- 历史回溯覆盖与手动推进；
- 候选处理状态、文章有效性、文章级评估和全部溯源；
- 用户级曝光、打开、归档和反馈状态；
- 新鲜／常青／探索标签；
- 今日目标 N、实际 M、短缺原因和软约束放宽；
- 推荐理由、探索原因和原始发布时间；
- 已选反馈状态、改选和撤销；
- 明确的“屏蔽此来源”“少推荐此主题”“少推荐此风格”范围；
- owner-only 来源管理和全局处理动作。

不要显示一个“来源质量分”。可显示抓取健康、更新频率、历史覆盖、候选产出和用户个人明确收藏，但文案必须避免暗示来源中的所有文章同质。

API 路由可以在现有 `/api/discovery` 和 `/api/recommendation` 下扩展，也可以将 owner 路由放入 `/api/admin/discovery`。无论命名如何，必须做到：

- 用户态端点从认证身份推导 user ID，不接受任意 user ID；
- owner 动作后端校验；
- 分页、过滤和结构化错误；
- 所有写操作幂等或带版本冲突处理；
- 前端不再调用全局候选 read/ignored 作为个人行为；
- 前端不再暴露破坏性今日重建。

完成标准：

- 从页面添加种子后能看到图谱扩展、端点状态和回溯进度；
- 能从一篇推荐追溯到站点和发现路径；
- 用户反馈有选中态，可改选、撤销并显示影响范围；
- 两个用户浏览同一候选时界面状态隔离；
- 日报不足和探索原因清楚可见；
- 非 owner 无法通过直接 API 请求执行管理动作；
- `npm run build` 和新增前端测试通过；
- 页面在没有 LLM 配置时仍可完整使用。

## Milestone M16 — Observability and Product Metrics

目标是让维护者能够回答“为什么没发现、为什么没推荐、长尾精品是否被发现”。

使用现有日志框架输出结构化事件，并贯穿 `job_id`、`fetch_run_id`、`site_id`、`source_id`、`candidate_id`、`user_id` 的安全标识。不要记录正文全文、Cookie、访问令牌或模型密钥。

管理统计至少包含：

来源和图谱：

- 种子、观察、活跃、暂停、屏蔽和非博客站点数；
- 新图谱边、新观察站点、按深度分布；
- 每站点独立入链数和最近仍被链接时间；
- 到期抓取按时开始率；
- 成功、304、失败、robots 禁止、退避和最近成功；
- 低命中站点是否超过最大空闲时间，目标为零个违规。

候选和文章：

- 新发现、正文抽取成功、文章页识别、失败和复核数；
- exact、canonical 和近似重复率；
- eligible 比例；
- 新鲜、常青、探索库存和库存天数；
- 文章评估规则版／模型版使用率和降级率；
- 历史回溯发现的 eligible 文章数。

日报和用户价值：

- 按时发布率；
- 目标 N 的填充率和短缺原因；
- 屏蔽违规、硬过滤违规、重复簇违规，目标均为零；
- 探索配额完成率；
- 来源、主题和作者集中度；
- 打开、valuable、deep-read、archive、not-interested、block 和 duplicate 反馈率；
- 探索文章接受率；
- 反馈后相似内容接受率变化。

必须实现并明确展示：

`长尾精品贡献率 = 来自低历史 eligible 命中率或低曝光站点、且获得 valuable/deep-read/archive 的推荐文章数 ÷ 全部获得这些正反馈的推荐文章数`

“低历史命中率”只用于统计分组，不回写文章资格或质量。另需记录：

- 有多少低命中站点至少贡献过一篇正反馈文章；
- 友情链接扩展站点贡献的正反馈文章数；
- 历史回溯文章在正反馈中的比例；
- 从新站点发现到第一篇精品的时间。

完成标准：

- 管理 API 或页面能够从一条空日报解释是库存不足、硬过滤、处理积压还是调度失败；
- 可以证明低命中站点未被调度饿死；
- 所有硬规则违规指标为零；
- 指标计算有固定测试数据和预期值；
- 日志可关联完整作业链路但不包含敏感正文。

## Milestone M17 — End-to-End Validation, Rollout and Cleanup

目标是证明完整闭环工作，并安全替换旧路径。

建立一个覆盖以下场景的端到端测试：

1. owner 添加种子 A；
2. A 的友情链接发现 B，B 又发现 C 和 A，循环不会扩张；
3. B 的近期 Feed 大多是普通文章；
4. B 的旧 Sitemap 和归档页中各有精品文章；
5. 文章流水线抓正文、去重并逐篇评估；
6. B 的精品独立达到 eligible，来源历史命中率没有降低其质量；
7. 同一文章经多个入口只有一个代表但有多条溯源；
8. 用户 U1 和 U2 的状态隔离；
9. U1 对 B 的一篇普通文章不感兴趣，B 的另一篇精品仍可推荐；
10. U1 显式屏蔽 B 后，只有 U1 不再看到 B；
11. `N=10` 时有足够候选发布 10 篇，候选不足时发布 M 并解释；
12. 有合格探索候选时满足探索配额；
13. 304、robots、500、超时和重启按预期处理；
14. 日报按两个时区正确发布；
15. 已发布日报在重试、候选更新和模型故障后保持不变；
16. 关闭 LLM 和向量能力后仍能完成全流程；
17. 长尾精品贡献指标得到预期非零结果。

至少执行：

    git status --short
    cd api
    gofmt -w <本里程碑修改的 Go 文件>
    go test ./...
    go test -race ./discovery ./recommendation ./jobqueue/...
    cd ../web
    npm run build
    cd ..
    make all

如果新增了前端测试脚本，也运行其正式命令并写入本文档。使用 Docker Compose 启动 PostgreSQL 环境，执行完整迁移、River 重启恢复和 pgvector 可选路径测试；同时保留 SQLite 测试。

灰度顺序：

1. 部署只包含增量表和双写，不切读；
2. 回填逻辑站点、端点、溯源和用户状态并对账；
3. 开启安全抓取和共享作业运行时；
4. 对少量种子开启 Blogroll 图谱和历史回溯；
5. 开启文章处理和评估 v3，比较新旧候选；
6. 对测试用户开启推荐 v3；
7. 验证日报、反馈和指标至少跨越多个用户本地日期；
8. 扩大到所有用户；
9. 停止旧写路径；
10. 在至少一个稳定发布窗口后删除旧路由、全局个人状态写入和破坏性重建；
11. 最后再删除不再使用的字段和唯一约束。

每一步都必须有回滚方法。关闭 v3 读路径不得删除已经采集的图谱、正文、评估或快照。迁移和回填命令应可重复运行。

最终更新：

- `README.md` 或相关部署文档；
- `docs/exec-plans/` 中本文档的结果；
- 配置变量、默认值和升级说明；
- API 权限和数据语义；
- 运营手册：来源失败、robots、处理积压、库存不足、模型降级和日报补偿；
- 数据保留和隐私说明。

清理完成标准：

- 生产代码不再把个人阅读行为写入候选全局状态；
- 不再有破坏性重建已发布日报的普通路径；
- 不再用永久 user/candidate 唯一约束代替冷却策略；
- 发现调度不再依赖单一全局串行 ticker；
- 所有旧兼容字段都有删除或保留理由；
- `Progress` 全部勾选，`Outcomes & Retrospective` 写明实际结果和遗留事项。

## Concrete Commands and Working Discipline

每个里程碑开始时，从仓库根目录执行：

    git status --short
    git rev-parse --show-toplevel
    git rev-parse HEAD
    sed -n '1,240p' PLANS.md
    sed -n '1,240p' AGENTS.md
    sed -n '1,240p' CONVENTIONS.md
    sed -n '1,320p' docs/exec-plans/blogroll-article-recommendation-v3.md

Go 基线：

    cd api
    go test ./...
    cd ..

前端基线：

    cd web
    npm ci
    npm run build
    cd ..

仓库级基线：

    make all

不要每次无条件执行 `npm ci`；只有依赖或 lockfile 变化、环境首次初始化时执行。若仓库已有缓存约定，应遵循仓库约定。Go 测试需要隔离缓存时可使用：

    GOCACHE=/tmp/dataark-go-cache go test ./...

每个里程碑结束前，至少执行相关聚焦测试和：

    cd api && go test ./...
    cd ../web && npm run build
    cd .. && make all
    git diff --check
    git status --short

在支持 PostgreSQL 的环境执行迁移和持久作业测试。使用仓库现有 Compose 文件和环境变量，不在本文档中复制真实密码。执行者应把确切命令和输出摘要补入 `Progress` 或 `Outcomes & Retrospective`。

每次提交前：

- 更新本文档；
- 确认没有密钥、临时正文或大型抓取文件；
- `git diff --check`；
- 检查迁移可重入；
- 检查新增 API 权限；
- 检查本里程碑对应的不变量测试；
- 使用描述行为结果的提交信息。

## Interfaces and Dependencies

以下接口表达必须保持，即使实际包名根据重构调整。

抓取边界：

    type FetchRequest struct {
        URL          string
        ETag         string
        LastModified string
        Kind         FetchKind
        MaxBytes     int64
    }

    type FetchResult struct {
        StatusCode   int
        FinalURL     string
        ContentType  string
        ETag         string
        LastModified string
        NotModified  bool
        Body          []byte
        FetchedAt     time.Time
    }

    type HTTPFetcher interface {
        Fetch(ctx context.Context, req FetchRequest) (FetchResult, error)
    }

时间边界：

    type Clock interface {
        Now() time.Time
    }

任务边界：

    type JobEnqueuer interface {
        EnqueueFetchSource(ctx context.Context, sourceID uint) error
        EnqueueScanBlogroll(ctx context.Context, siteID uint) error
        EnqueueBackfillSite(ctx context.Context, siteID uint) error
        EnqueueProcessCandidate(ctx context.Context, candidateID uint, contentVersion string) error
        EnqueueGenerateDaily(ctx context.Context, userID uint, localDate string) error
    }

友情链接边界：

    type BlogrollLink struct {
        URL             string
        AnchorText      string
        SourcePageURL   string
        EvidenceKind    string
        EvidenceSummary string
        Confidence      float64
    }

    type BlogrollDiscoverer interface {
        Discover(baseURL string, html []byte) ([]BlogrollLink, error)
    }

站点分类边界：

    type SiteClassification struct {
        Kind       string
        Confidence float64
        Reasons    []string
    }

    type SiteClassifier interface {
        Classify(ctx context.Context, siteURL string, evidence SiteEvidence) (SiteClassification, error)
    }

正文抽取边界应复用现有 `ExtractArticle` 能力，并让调用端只依赖稳定结果结构。文章评估边界：

    type ArticleAssessmentInput struct {
        CandidateID    uint
        ContentVersion string
        Title          string
        Author         string
        BodyText       string
        Language       string
        PublishedAt    *time.Time
        Metadata       map[string]string
    }

    type ArticleAssessment struct {
        InformationDensity float64
        Originality        float64
        Completeness       float64
        Evidence           float64
        Readability        float64
        Depth              float64
        EvergreenValue     float64
        OverallQuality     float64
        Confidence         float64
        Reasons            []string
        Assessor           string
        Version            string
    }

    type ArticleAssessor interface {
        Assess(ctx context.Context, input ArticleAssessmentInput) (ArticleAssessment, error)
    }

`ArticleAssessmentInput` 不得加入来源平均质量、来源命中率、来源自动权重或图谱层级。若实现确需来源名称用于正文归属识别，应通过单独、不可参与评分的溯源对象传递，并用测试证明结果不随来源统计变化。

优先复用仓库已有依赖：GORM、Goose、River、gofeed、Colly、DOM Distiller、URL 规范化和现有 OpenAI-compatible 客户端。只有现有依赖无法安全实现 robots、HTML 解析、近似指纹或测试需求时才新增库；新增前在 `Decision Log` 记录原因、维护状态和替代方案。

## Validation and Acceptance

最终验收不是“所有测试通过”这一句，而是以下行为可以由测试夹具和本地应用演示。

### Source graph and crawling

- 添加种子后立即产生站点、端点、首次抓取和 Blogroll 扫描任务。
- A → B → C → A 不重复、不死循环，深度和每日上限有效。
- 每个自动发现站点都有可解释发现路径。
- robots、SSRF、响应大小、条件请求和域名限流有效。
- 新端点首次立即抓取，失败独立退避，启动补做已到期任务。
- 低命中站点在允许抓取状态下不会超过配置的最大空闲时间。

### Article discovery and quality

- Feed 最新文章和 Sitemap／归档历史文章都能进入统一流水线。
- 正文抽取成功后才进入正常评估；非文章页不能 ready。
- 同一文章多个入口只有一个代表，全部溯源保留。
- eligible 池 exact 和 canonical 重复为零。
- 低命中来源中的精品文章得到与相同正文在其他来源时一致的文章级评估。
- 来源统计不参与文章有效性、质量门槛、质量分或封顶。
- LLM 关闭时规则评估可以完成全流程。

### User isolation and feedback

- 用户 A 的曝光、打开、反馈、归档和屏蔽不改变用户 B。
- 单篇“不感兴趣”不屏蔽来源。
- “太重复”只修正重复或相似内容。
- 只有显式“屏蔽来源”产生来源级硬过滤。
- 反馈重复提交幂等，可改选、撤销，软偏好衰减。
- 已发布日报不会因反馈而重排。

### Daily recommendation

- 用户本地日期和生成时间在 API、调度器和历史页一致。
- 合格候选足够时恰好 `N` 篇；不足时 `M` 篇并解释。
- 硬过滤、屏蔽和重复违规为零。
- 探索配额在存在合格探索候选时生效。
- 低命中来源精品可以公平超过其他来源普通文章。
- 曝光未打开文章按冷却规则处理，实质更新文章可带更新标记重新出现。
- 发布后刷新、重试、重启、候选修改和模型切换不改变快照。
- 外部模型失败时仍能发布。

### Initial operational targets

这些是初始验收和监控阈值，不是永久产品真理，真实运行后可通过 `Decision Log` 调整：

- eligible 池 exact/canonical 重复：0；
- 日报硬过滤、屏蔽和重复簇违规：0；
- 允许抓取的低命中站点超过最大空闲时间：0；
- 代表性批准站点测试集正文抽取成功率：至少约 90%；
- 正常供给时目标可推荐库存：至少约 7 天，低于 3 天严重预警；
- 到期抓取在调度容差内开始的比例：目标至少 95%；
- 已发布日报重试一致率：100%；
- 反馈幂等测试：100%；
- 端到端夹具中的长尾精品贡献率：必须大于 0。

## Idempotence and Recovery

迁移、任务和写路径必须允许重复执行。

站点以规范根 URL／主机键幂等 upsert；边以 from、to、来源页面和关系类型幂等 upsert；溯源以候选、来源端点、发现方法和原始 URL 幂等 upsert。重复抓取只更新最近看到时间和 fetch run，不制造候选。

正文以内容版本控制。相同正文不重复评估；新正文创建新评估版本。失败步骤保存可重试状态和错误类别，不通过删除候选“重来”。

回溯批次在事务内保存游标和发现结果，重试同一游标安全。若批次部分完成，下一次可以从最后已确认位置继续；不要把“本批没有精品”当成完成条件。

日报发布使用事务和用户／本地日期唯一身份。已发布时生成作业直接返回；补充只追加新项目并有唯一约束；任何失败都不能先删除原日报。反馈使用幂等键和当前状态版本，重试不重复加权。

回滚时优先关闭 v3 读路径和新任务入队，保留新增表和采集数据。不要通过 Down 迁移删除仍被新旧路径引用的数据。执行破坏性清理前必须导出计数、运行对账并在 `Decision Log` 记录。

## Artifacts and Notes

实施过程中把有长期价值的证据记录在本节，而不是粘贴大量日志。建议记录：

- 每个迁移的表和约束摘要；
- 图谱夹具示意及预期节点／边数量；
- 低命中来源精品防回归测试名称；
- 日报不可变快照测试名称；
- PostgreSQL 重启恢复测试命令；
- 数据回填前后计数；
- 灰度开关启用日期；
- 性能基线：每千来源调度耗时、每百文章处理耗时和队列积压；
- 已知无法自动分类、需要人工复核的站点模式。

不要把用户真实正文、真实私有 Feed、Cookie、Token 或生产错误全文写入本文档。

M1 迁移证据：`api/migrations/000003_blog_discovery_v3.sql` 只增加结构并移除跨日报永久唯一约束，Down 保留数据；`api/bootstrap/database_v3_test.go` 在 SQLite 旧式表和数据上执行两次迁移，期望 2 个旧来源合并为 1 个逻辑站点、2 个候选得到 2 条溯源、1 个旧日报／项目／反馈计数不变，并验证同一候选可出现在不同日报但不能在同一日报重复。

M3 抓取证据：`api/discovery/fetch_m3_test.go` 在本地服务器和固定时钟上验证条件请求、robots、重定向复验、类型限制、退避恢复、有限低命中调度和域名限制；`api/migrations/000004_discovery_fetch_observability.sql` 以结构化列保留每次抓取的最终 URL、响应类型、验证器和 robots 状态。

M4 图谱证据：`api/discovery/blogroll_m4_test.go` 验证显式／上下文／稳定列表规则、否定语境、A→B→C→A 幂等循环、质量独立、自动 observing／端点任务、最短路径和三类 pending 恢复；`api/migrations/000005_blogroll_graph_evidence.sql` 增加检测规则、上下文摘要和持久激活时间。

M5 入池证据：`api/discovery/endpoint_m5_test.go` 验证主页／Feed／Sitemap 端点共存和立即调度、Sitemap index 子端点、Feed+Sitemap 单候选双溯源、可信元数据合并、标题-only `fetch_pending` 以及重复 200／304 零重复处理；`api/migrations/000006_recent_ingestion_metadata.sql` 只增加与质量评分分离的元数据可信度。

M6 回填证据：`api/discovery/backfill_m6_test.go` 验证旧 Sitemap 精品、archive-only 精品、策略优先级、失败游标恢复、批次重放幂等、未知日期置信度、7 天有限下限和只基于游标耗尽的完成原因；`api/api/controller_test.go` 固定覆盖 API 的游标与停止原因；`api/migrations/000007_historical_backfill_observability.sql` 只增加发布日期置信度和最近批次时间且 Down 保留采集数据。

M7 正文证据：`api/discovery/candidate_processing_m7_test.go` 验证 Feed 全部候选到终态、正文抽取元数据、相同／变化 HTML 版本语义、陈旧作业、标签／登录／短文硬排除、瞬时失败上限和 robots 停止；`api/recommendation/service_test.go` 证明摘要-only 候选不能被旧富化／推荐旁路选中；`api/migrations/000008_article_processing_pipeline.sql` 增加处理审计和不可变正文版本且 Down 保留数据。

M8 身份证据：`api/discovery/dedupe_m8_test.go` 验证 tracking URL 合并、多 URL／redirect／canonical／exact body 三成员单代表、三条代表 provenance、近似聚类、独立正文隔离、代表变化与历史快照隔离；`api/recommendation/dedupe_m8_test.go` 验证成员无法旁路推荐及 duplicate feedback 零来源惩罚；`api/migrations/000009_article_identity_clusters.sql` 增加身份、cluster、代表解释和复核信号且 Down 保留数据。

M9 评估证据：`api/discovery/assessor_m9_test.go` 验证 assessor 静态输入边界、B 的 97 普通／3 精品、同正文跨来源同分、规则降级、正文版本重评和历史 assessment 不变；`api/recommendation/article_assessor_m9_test.go` 验证 OpenAI-compatible adapter 不传 URL／来源身份、规则与增强并存以及推荐项冻结 active assessment；`api/migrations/000010_article_assessment_activation.sql` 增加当前引用和降级错误且 Down 保留所有评估。

M10 隔离与权限证据：`api/discovery/user_state_m10_test.go` 与 `api/recommendation/user_state_m10_test.go` 验证双用户状态、曝光、反馈和来源屏蔽隔离；`api/discovery/site_admin_m10_test.go` 验证 owner 暂停／安全屏蔽保留候选、停止三类工作并可恢复；`api/api/permissions_m10_test.go` 验证 member 对来源、站点、回溯和破坏性日报操作得到 403。`api/migrations/000011_user_candidate_state_and_permissions.sql` 只增加旧状态复核审计，保留旧字段和逐用户状态以支持回滚读路径。

M2 恢复证据：`TestMemoryQueueConcurrentDuplicateExecutesOnce` 对同一候选版本并发入队 100 次只执行 1 次；`TestMemoryQueueRetriesInterruptedJobsAndIsolatesFailures` 证明失败来源和模拟进程中断可恢复且不阻塞其他来源；`TestStartSQLiteDuplicateRecoveryRunsOnce` 证明两个运行时恢复同一端点只执行一次；`TestRecoverDueJobsContinuesAfterIndependentSourceFailure` 与 `TestRecoverDueJobsEnqueuesOnlyMissingLocalDay` 固定发现和日报启动补偿边界。

## Plan Revision Note

2026-07-13：创建初始版本。相较于早期“来源质量优先”的可能解释，本计划明确采用“友情链接受控扩展、来源级信号只调度资源、文章级独立质量判断、低命中来源保留非零预算”的产品约束，并把它贯穿数据模型、抓取、历史回溯、推荐、反馈、日报、UI、指标和端到端验收。

2026-07-13：完成 M0 并把计划移动到恢复协议规定的正式路径。此次修订记录基线版本和验证结果，增加确定性多站点夹具及可替换时钟、HTTP、任务边界，并说明受限环境中 `make all` 的安全替代验证；这些变化为后续数据库和任务里程碑提供无外网、无 LLM 密钥的稳定测试基础。

2026-07-13：完成 M3 后补充统一抓取边界、结构化抓取审计、逐端点有限调度和本地安全验收结果；新增的第四份 Goose 迁移只扩展 fetch-run 可观测字段，保留 M1 的增量／数据保留回滚策略。

2026-07-13：完成 M4 后补充 Blogroll 证据规则、有界 BFS 图谱、pending 恢复、种子即时调度和只读最短路径 API 的实际结果；第五份 Goose 迁移继续采用增量且数据保留的回滚策略。

2026-07-13：完成 M5 后补充端点独立调度、主页／Feed／Sitemap 增量入池、多溯源与元数据合并的实际行为；第六份 Goose 迁移只增加元数据可信度并保留回滚期间的候选和溯源数据。

2026-07-13：完成 M6 后补充有界持久游标、Sitemap／归档策略优先级、有限非零调度、发布日期置信度、覆盖状态 API 和本地重启／幂等验收结果；第七份 Goose 迁移只扩展回填可观测字段并保留历史采集数据。

2026-07-14：完成 M7 后补充常规候选 worker、文章页硬规则、有限失败状态、不可变正文版本、旧候选恢复和推荐门禁的实际行为；第八份 Goose 迁移只增加处理审计与正文版本结构，继续采用数据保留回滚。

2026-07-14：完成 M8 后补充多层文章身份、确定性近似聚类、可解释代表、溯源汇聚、推荐代表门禁和重复复核信号；第九份 Goose 迁移保留候选成员与历史快照，只增身份审计结构。

2026-07-14：完成 M9 后补充文章专用 assessor 边界、规则维度与门槛、可选增强适配、失败降级、旧富化隔离和推荐 assessment 快照；第十份 Goose 迁移只增加当前评估引用与错误审计并保留全部版本。

2026-07-14：完成 M10 后补充逐用户候选 overlay、曝光与反馈同步、旧全局状态复核、owner/member 后端权限、可逆站点暂停／安全屏蔽和手动回溯；第十一份 Goose 迁移只增加可对账复核表，原状态和用户数据均保留。

2026-07-13：完成 M1。此次修订记录增量 v3 模型、数据保留回填、角色和日报快照语义、唯一约束转换、SQLite 迁移证据及 PostgreSQL 基础设施缺口；选择数据保留 Down 和启动幂等回填，是为了让后续里程碑可逐步切流并在任何检查点安全恢复。

2026-07-13：完成 M2。此次修订记录共享 River／内存运行时、五类稳定作业参数、启动恢复、并发幂等、失败隔离、发现 ticker 切换及 v2 兼容证据；函数式 handler 注入保持基础设施与领域包无环，为后续抓取、图谱、回溯和文章处理逐项接入留下明确边界。
