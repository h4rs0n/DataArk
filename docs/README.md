# 文档索引

现行文档按仓库代码与 Goose `000001`–`000044` 核对于 2026-10-14。部署状态、历史测试结果和模型效果需要在对应环境另行验证。

## 文档清单

| 文档 | 类型 | 用途 |
| --- | --- | --- |
| [数据库设计](design-docs/dbDesign.md) | 设计 | 内容、发现、去重、评估（实时与 Batch）、推荐与归档的存储职责及迁移约束 |
| [数据库关系图](design-docs/dataark_er.dot) | 设计 | 主要领域关系概览，含数据库 FK 与应用归属的区别 |
| [Material 升级手册](operations/material-migration-runbook.md) | 手册 | `000032`–`000035` 的维护窗口、检查与备份回滚 |
| [推荐运维手册](operations/recommendation-runbook.md) | 手册 | 自动抓取、手动 LLM 评估、个人推荐、故障恢复与发布验证 |
| [文章评估手册](operations/article-assessment-runbook.md) | 手册 | 当前 `article-value-v4` 协议、LLM 评估队列、回填、评估指针回滚及 `000036` 下线迁移 |
| [文章去重手册](operations/article-dedupe.md) | 手册 | 全库指纹去重与代表文章评估：LSH 聚类、`auto_apply` / `pending_assessment` 编排、运行回滚与故障排查 |
| [百炼 Batch 评估手册](operations/assessment-batch-runbook.md) | 手册 | 异步评估配置、手动授权、恢复、失败重试、文件清理、容量暂停、回退与重导入工具及变更记录 |
| [前端易错点](references/frontend-pitfalls.md) | 参考 | Arco 组件参数与浏览器验证经验 |
| [ExecPlan 编写规范](plans.md) | 方法 | 可选的长篇执行规格（ExecPlan）的编写与维护要求 |
| [文档核对记录](investigation/2026-09-30-documentation-audit.md) | 调查记录 | 2026-09-30 文档与代码核对发现的不一致、修订及代码依据 |
| [Batch 模型输出类型错误调查](investigation/2026-10-14-batch-output-type.md) | 调查记录 | 2026-10-14；`model output is invalid JSON` 主因是输出字段类型与本地结构不符 |
| [Batch 评估跳过调查](investigation/2026-10-11-batch-skip.md) | 调查记录 | 2026-10-11；`preparing` 阶段版本零历史任务进入 `skipped` 的原因 |
| [discovery-feed 刷新延迟调查](investigation/2026-09-30-discovery-feed-refresh-latency.md) | 调查记录 | 2026-09-30；12 秒延迟由同步 LLM 排序与重复候选查询叠加造成 |
| [慢 SQL 调查](investigation/2026-10-09-slow-sql.md) | 调查记录 | 2026-10-09；黑名单文章使查重恢复不收敛的根因分析与修复验收 |

## 文档类型与维护规则

- **手册**（`operations/`）：持续维护的活文档，随代码演进更新。
- **调查记录**（`investigation/`）：一次性事件调查，写完即冻结，不再回改；需要长期生效的结论应沉淀进对应手册的故障排查章节。
- **设计**（`design-docs/`）：数据库结构与关系。新增编号 Goose 迁移时必须同步更新 `dbDesign.md`，避免再次滞后。
- **参考**（`references/`）：长期有效的开发经验。
- **方法**（`plans.md`）：仅在用户要求长篇执行规格时使用。
- 新增文档必须在本索引登记，未登记的文档视为过期草稿。
- 新增文档统一使用中文，与项目定位一致；存量英文手册在下次实质修订时顺手翻译，不安排专门一轮。
- 调查记录统一使用 `YYYY-MM-DD-topic.md` 命名（日期在前，便于按时间排序），正文首行注明调查日期。

代码事实入口：HTTP 路由在 `api/api/routes.go`，启动数据库迁移与 checkpoint 在 `api/bootstrap/database.go`，生产 schema 在 `api/migrations/`，运行参数在 `api/flag/flag.go`，队列在 `api/jobqueue/jobqueue.go`，Compose 环境变量映射在 `docker/docker-compose.yml`。
