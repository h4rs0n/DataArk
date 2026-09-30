# 文档与代码事实核对（2026-09-30）

核对范围是 `docs/` 下的 Markdown 与 DOT 文件。依据为当前工作树中的源码、编号 SQL 迁移、测试源码、构建清单和 Compose 配置。本次修订只涉及文档；没有执行数据库迁移、模型调用或部署，也没有重新证明历史计划中的运行结果。

## 现行文档中的不一致

下列问题已修订。

| 文档 | 原说明的问题 | 当前代码事实与依据 |
| --- | --- | --- |
| 推荐运维手册：升级和回滚 | 把升级写成全部可逆，允许仅回退旧二进制，要求继续保留兼容列 | [`000034`](../../api/migrations/000034_material_candidate_projection.sql) 已删除候选正文、评估和单来源列；[`000032`](../../api/migrations/000032_material_storage.sql)–[`000035`](../../api/migrations/000035_material_relations.sql) 的 Down 拒绝执行。跨此边界须恢复升级前数据库/归档备份及旧版本。 |
| 推荐运维手册：启动队列 | 写启动暂停 discovery，但下一段要求自动抓取 | [`jobqueue.Start`](../../api/jobqueue/jobqueue.go) 恢复自动 `discovery_crawl`，暂停 `article_assessment`；中断抓取任务恢复为 available。 |
| 推荐运维手册：停止发现 | 把 `-discover-interval=0` 当作停止全部抓取 | [`StartDiscoveryScheduler`](../../api/discovery/store.go) 仅关闭周期调度；[`startApplicationJobQueue`](../../api/api/jobs.go) 仍执行启动恢复。已有队列与 owner 操作仍可抓取，需暂停/禁用站点或源。 |
| 推荐运维手册、数据库设计：启动回填 | 写兼容回填每次启动重跑，Material 手册只列三个 checkpoint | [`runV3CompatibilityOnce`](../../api/bootstrap/database.go) 使用 `v3-compatibility` checkpoint。Material 检查说明已补上第四项。 |
| 推荐运维手册：robots 与历史抓取 | 写 Disallow 阻止抓取、robots 不可用须等待 | [`RobotsCache.Inspect`](../../api/discovery/robots.go) 与 [`SafeFetcher`](../../api/discovery/boundaries.go) 仅记录 robots 状态；不执行 Allow/Disallow/Crawl-delay，也不消费 Sitemap 声明。[`000028`](../../api/migrations/000028_remove_sitemap_capability.sql) 关闭旧 Sitemap 源和未完成回溯。 |
| 推荐运维手册、评估手册：失败重试 | 统一写失败交给 River 自动重试；未说明手动评估的重入步骤 | [`AssessArticleArgs.InsertOpts`、`GenerateDigestSummaryArgs.InsertOpts`](../../api/jobqueue/jobqueue.go) 均设置 MaxAttempts=1。失败评估需 owner backfill `retryFailures:true` 后再次执行队列；摘要由恢复逻辑重新入队；每日生成使用 River 默认重试。 |
| 评估手册：模型输出 | 写摘要使用原文语言、confidence 来自模型、rerank 也使用 32,768 上限 | [`assessment/openai.go`](../../api/assessment/openai.go) 要求 reasons/summary/keywords 使用简体中文；[`article_assessor.go`](../../api/assessment/article_assessor.go) 设置 confidence=1；[`recommendation/openai_provider.go`](../../api/recommendation/openai_provider.go) 将 rerank 上限设为 2,048。 |
| 评估手册：元数据归属与作用 | 写 summary/keywords 在候选物理行，完全不影响排名 | [`applyAssessmentArticleMetadata`](../../api/assessment/assessor.go) 和 [`updateAssessmentMaterial`](../../api/assessment/material.go) 写 `material.summary/topics`；候选由视图投影。topics 被用户画像、主题屏蔽和 LLM rerank 消费，写回本身不改评分或质量门槛。 |
| 评估手册：回填、质量门槛和回滚 | 写缺任意模型行才入队、20/100 固定、回滚必取更早行，并先换配置再回滚 | [`assessment/admin.go`](../../api/assessment/admin.go) 按 material 版本、assessor、model、policy 匹配；回滚只筛当前配置对应的活跃行，优先其他 policy 后按 ID 降序，不要求 ID 更早。[`activateArticleAssessment`](../../api/assessment/assessor.go) 使用可配置门槛，默认 0.20。 |
| 评估手册：配置和停止 | 没说明环境变量仅经 Compose 转 flags；要求停止队列但没有对应 HTTP 操作 | [`flag.ParseFlag`](../../api/flag/flag.go) 读 CLI flags；[`docker-compose.yml`](../../docker/docker-compose.yml) 映射环境变量。[`routes.go`](../../api/api/routes.go) 只有 assessment queue 查询/run，运行中的评估可停止 API，重启后默认暂停。 |
| 推荐运维手册：权限 | 把 source CRUD 和所有 graph/backfill 操作统称 owner-only | [`discovery.go`](../../api/api/discovery.go) 的 source list、graph、backfill coverage 只要求认证；写操作、site operations 和队列操作另需 owner。 |
| 数据库设计与关系图 | 外键概述遗漏 archive→material，图把无 users FK 的归属画成数据库关系，并仍列候选 eligibility 列 | [`000032`](../../api/migrations/000032_material_storage.sql) 定义 archive→material；[`000001`](../../api/migrations/000001_recommendation_v2.sql)、[`000022`](../../api/migrations/000022_discovery_personalized_feed.sql) 没有设置/日期/画像/屏蔽/feed→users FK；eligibility 在 material_article_states。图已修正并标为主要关系概览。 |
| 数据库设计：黑名单与归档 | 把黑名单限定成注册域；把 PostgreSQL 归档存储描述成只有元数据 | [`domain_blacklist.go`](../../api/discovery/domain_blacklist.go) 按主机和子域匹配，可单独屏蔽子域；归档抽取正文进入 material_representations。 |
| 数据库设计、Material 手册：操作细节 | 摘要 GET 路径漏了 `/api`；没有说明归档重建与迁移问题记录的清理边界 | 摘要路径见 [`routes.go`](../../api/api/routes.go)；[`search/rebuild.go`](../../api/search/rebuild.go) 不清理问题表，[`BackfillMaterialContent`](../../api/archive/material.go) 才会重新检查文件并删除已修复记录。 |
| 前端参考文档路径 | `AGENTS.md` 要求的 `docs/references/frontend-pitfalls.md` 不存在 | 已将原 `docs/erroneous experience/frontend-pitfalls.md` 移至要求的位置；本地 Arco alert 类型声明也确认有 title prop。 |

## 历史计划中的过时事实

历史计划保留当时的背景和验证记录，已补充归档说明及现行入口。下列内容不能用作当前实现说明：

- `manual-crawl-queue.md`、`crawl-failure-recovery.md`：手动抓取已变为自动抓取，手动开启的是 LLM 评估。
- `discovery-robots-advisory-policy.md`、早期 v2/v3 计划：Sitemap 提示与抓取已关闭。
- `llm-token-control-observability.md`：原 1,024 token 补全上限已改变。
- `article-assessment-schema-retry.md`：原回放模型输出的步骤已改为只追加校验错误。
- `article-assessment-summary-keywords.md`、`article-assessment-v3.md`：observe、旧 JSON 和旧字段归属已被 active/v4/material 取代。
- `recommendation-modules-split.md`：评估持久化已从 discovery 移至 assessment，无队列时发现也停在 pending。
- `recommendation-one-article-per-source.md`：当前组池优先不同来源，最终顺序由 LLM rerank 决定。旧的硬性限制、diversify 函数和 `source_limit` 放宽分支均已移除，不能承诺最终每来源只出现一篇；当前 selection policy 为 `v3-selection-llm-1`。
- 早期发现、v2 推荐、归档覆盖率计划及若干后续计划：`api/common`、`api/api/controller.go`、`api/recommendation/service.go` 等路径已删除或拆分。旧计划中的这些路径是历史定位信息。

## 验证边界

现行手册的路由、flags、表和字段已与源码/迁移静态核对；本次新增 Markdown 相对链接与 DOT 语法另行校验。数据库设计中的 2026-08-27 实例快照、历史计划中的覆盖率、截图、Compose 测试、数据量和模型效果只保留为历史记录，本次未连接该部署验证。文档修订没有改变代码行为，因此未重跑后端/前端测试或生产集成测试。

校验通过：28 份 Markdown 中的 59 个相对链接均有目标，现行手册明确列出的 9 个不同 HTTP 接口均匹配路由注册，20 份 done 计划均有归档说明；前端参考文档迁移前后内容一致。`git diff --check` 通过，Graphviz `dot -Tsvg` 成功解析关系图。
