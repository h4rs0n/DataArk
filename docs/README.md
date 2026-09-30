# 文档索引

现行文档按仓库代码与 Goose `000001`–`000035` 核对于 2026-09-30。部署状态、历史测试结果和模型效果需要在对应环境另行验证。

| 文档 | 用途 |
| --- | --- |
| [数据库设计](design-docs/dbDesign.md) | 内容、发现、评估、推荐与归档的存储职责及迁移约束 |
| [数据库关系图](design-docs/dataark_er.dot) | 主要领域关系概览，含数据库 FK 与应用归属的区别 |
| [Material 升级手册](operations/material-migration-runbook.md) | `000032`–`000035` 的维护窗口、检查与备份回滚 |
| [推荐运维手册](operations/recommendation-runbook.md) | 自动抓取、手动 LLM 评估、个人推荐、故障恢复与发布验证 |
| [文章评估手册](operations/article-assessment-runbook.md) | 当前 `article-value-v4` 协议、人工金标验收、回填和评估指针回滚 |
| [前端易错点](references/frontend-pitfalls.md) | Arco 组件参数与浏览器验证经验 |
| [本次事实核对记录](references/documentation-audit-2026-09-30.md) | 已发现的不一致、修订及代码依据 |

代码事实入口：HTTP 路由在 `api/api/routes.go`，启动数据库迁移与 checkpoint 在 `api/bootstrap/database.go`，生产 schema 在 `api/migrations/`，运行参数在 `api/flag/flag.go`，队列在 `api/jobqueue/jobqueue.go`，Compose 环境变量映射在 `docker/docker-compose.yml`。
