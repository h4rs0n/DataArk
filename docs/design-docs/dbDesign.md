# 数据库设计

本文档描述仓库的 PostgreSQL schema，内容模型更新至 Goose `000035`。文中标注的运行库类型与数量是 2026-08-27 的历史快照，不能据此推断生产库已经升级。

- 生产 schema **只**由 Goose 编号迁移定义：`api/migrations/000001`–`000035`。
- 启动路径：`api/bootstrap.InitDB()` → `api/database.InitDB()` 连库 → `RunDatabaseMigrations()`（Goose `Up` + River migrator）。
- GORM `AutoMigrate` **只给 SQLite 测试当方言替身**，不定义生产 schema。
- 模型分散在 `api/material`、`api/auth`、`api/archive`、`api/discovery`、`api/assessment`、`api/assessmenteval`、`api/recommendation`。没有 `api/common/`。
- 改列/建表必须新增编号 Goose 文件；不要假设改 GORM tag 就会改生产库。

本实例快照：`goose_db_version.version_id = 30`；扩展 `vector 0.8.3`、`pgcrypto 1.3`、`plpgsql`；`public` 下 **44** 张表（含 Goose 账本与 River）。

这份运行库是「早期 GORM AutoMigrate + 后续 Goose」叠出来的，因此个别类型、多余列与空库纯跑 Goose 不完全相同。文末「运行库与纯 Goose 空库的差异」单独列出。

## 总体约定

- ORM：GORM；生产方言：PostgreSQL。
- 表名通常使用复数；核心内容表明确命名为单数 `material`，评估行继续使用 `discovery_article_assessments`。
- 主键：业务表多为 `BIGSERIAL`；`archive_tasks.id` 是 `varchar(36)`（UUID）；`archive_stats.source` 是域名主键；`discovery_duplicate_clusters.cluster_id` 是字符串主键。
- 时间：DSN 带 `TimeZone=Asia/Shanghai`；时间列多为 `timestamptz`。
- 外键：归档/用户/搜索事件之间仍无 FK。发现图、评估、推荐、金标工作流、River 客户端队列有显式 `REFERENCES`。
- JSON：`material.authors/topics/entities` 使用 JSONB；迁移兼容原候选的 JSON 文本列。
- 向量：`material_embeddings` 按 `(representation_id, model)` 保存，与具体内容表示关联。
- 原始物理文件与 Meilisearch 索引在 Postgres 外；抽取正文存入 `material_representations`。

## 领域关系

```text
discovery_sites ── discovery_sources
                         │
                  material_provenances ────── material
                         │                      ├── material_versions ── material_representations ── material_embeddings
                  discovery_candidates ─────────┤
                                                ├── material_identities / material_redirects
                                                ├── material_article_states ── discovery_article_assessments
users ─────────── user_material_states ──────────┤
recommendation_days / feed_batches ── items ─────┤
archive_documents / material_archive_links ─────┘

discovery_article_content_versions / material_candidate_versions
  保留历史抽取 ID 和旧候选版本映射，供金标与旧队列任务使用。
river_* / goose_db_version：队列与迁移账本。
```

当前产品流：自动发现并记录来源 → 候选抓取/抽取 → material 内容版本 → 人工评估队列 → 推荐读取 ready/eligible 内容。

## 核心内容模型（`000032`–`000035`）

`material` 仅保存跨媒介共有的内容属性：标题、摘要、作者列表、语言、发布时间、主题、实体、当前版本指针、创建/更新时间。不保存 URL、文件名、路径、MIME、文件大小、正文、时长或文章专属评分。

| 表 | 职责与约束 |
| --- | --- |
| `material_versions` | 内容版本，唯一 `(material_id, version)`；当前版本复合 FK 保证属于同一 material |
| `material_representations` | 抽取结果，唯一 `(version_id, kind, role)`；目前写 `text/body`，可扩展转录、字幕等表示 |
| `material_identities` | 强身份，唯一 `(kind, identity_key)`；规范化 URL、带算法标识的正文哈希 |
| `material_redirects` | 合并后旧 material ID 到保留 ID 的重定向 |
| `material_provenances` | 来源与内容的多对多关联；保留发现入口、来源页、元数据与首次/最近发现时间 |
| `material_article_states` | 文章评估状态、资格、评分、模型版本等文章专属投影 |
| `material_embeddings` | 内容表示与 embedding 模型对应的向量 |
| `material_archive_links` | 内容到历史归档任务的关联；物理文件详情仍在 archive 领域 |

独立来源数为 `COUNT(DISTINCT domain_key)`，空域名不计数。同一站点的 feed、首页等入口保留多条证据，但只算一个独立来源。域名取发现来源并保存快照，删除来源端点不会丢失计数。HTTP 兼容投影 `discovery_candidate_details` 暴露 `materialId` 和 `independentSourceCount`；尚未修改排名权重。

强身份合并保留版本、评估、推荐快照和来源证据；相似正文只形成去重簇，不合并 material 或转移来源。用户状态按 material 合并，曝光计数累加，反馈取最新事件。历史同日重复推荐标记 `legacy_duplicate`，新推荐由 material 唯一索引防重。

当前只实现网页/归档 HTML 的文本抽取。PDF、DOCX、音视频可复用核心内容、版本、来源关系，并增加专用表示与处理器；本次不包含这些格式的解析器。

升级需在停止旧 API/worker 写入后备份数据库和归档目录。Goose 搬迁数据并删除候选正文列，启动再执行可重入的强身份归并与归档内容回填。缺失归档文件记入 `material_ingestion_issues`；检查该表后修复文件。迁移的 Down 主动拒绝有损回滚，应恢复升级前备份及匹配的旧二进制。参见 `docs/operations/material-migration-runbook.md`。

---

## 用户

### `users`

登录与角色。启动时 `auth.CreateDefaultAdmin()`：若 `admin` 不存在则随机 12 位十六进制密码并打日志；`admin` 角色为 `owner`，其余默认 `member`。`BackfillOwnerRole` 会把历史 `admin` 回填为 owner。

| 列 | 运行库类型 | 约束 | 说明 |
| --- | --- | --- | --- |
| `id` | `bigint` identity | PK | |
| `username` | `text` | 唯一 `uni_users_username`，非空 | |
| `password` | `text` | 非空 | bcrypt；JSON 不返回 |
| `role` | `varchar(32)` | 非空，默认 `member`；`idx_users_role` | `owner` / `member`（`000003`） |
| `created_at` / `updated_at` | `timestamptz` | 可空 | GORM 维护 |

主要操作仍在 `api/auth`：`CreateUser`、`LoginUser`、`GetUserByID`、`GetUserByUsername`、`UpdateUser`（改密码会再哈希）、`DeleteUser`、`GetAllUsers`。

---

## 归档、搜索与点击

HTML 文件在归档目录；Postgres 只存任务、按域名的文件计数、无法从路径恢复的元数据，以及搜索/点击事件。

### `archive_tasks`

离线归档任务。`pending`/`running` 视为活跃；按 URL 查最新任务实现复用，**没有** `url` 唯一约束。

| 列 | 类型 | 约束 | 说明 |
| --- | --- | --- | --- |
| `id` | `varchar(36)` | PK | UUID |
| `url` | `text` | 非空，`idx_archive_tasks_url` | |
| `domain` | `text` | 非空 | |
| `status` | `text` | 非空，`idx_archive_tasks_status` | |
| `file_name` / `error` / `external_task_id` | `text` | 可空 | `error` 为失败详情 |
| `created_at` / `updated_at` / `started_at` / `finished_at` | `timestamptz` | 可空 | |

操作：`CreateArchiveTask`、`SaveArchiveTask`、`GetArchiveTaskByID`、`GetLatestArchiveTaskByURL`、`FindActiveArchiveTaskByURL`、`ListArchiveTasksByStatuses`。没有 `(url, status, created_at)` 复合索引。

### `archive_stats`

按归档一级域名目录统计 `.html` 数量。总数不单独落行，查询时对 `file_count` 求和。

| 列 | 类型 | 约束 |
| --- | --- | --- |
| `source` | `varchar(255)` | PK |
| `file_count` | `bigint` 非空默认 0 | 本库为 bigint；Goose 原文是 `integer` |
| `created_at` / `updated_at` | `timestamptz` | |

接口：`GET /api/archiveStats`、`POST /api/archiveStats/refresh`。扫描跳过 `Temporary`；根目录散落文件不计入。增量用 `IncrementArchiveStat` / `DecrementArchiveStat`。

### `archive_documents`

单个 HTML 的可搜索元数据。身份是 `(domain, file_name)`。

| 列 | 类型 | 约束 |
| --- | --- | --- |
| `id` | `bigint` | PK |
| `material_id` | `bigint` | 非空 FK → `material`，内容身份 |
| `domain` | `varchar(255)` | 非空；与 `file_name` 组成唯一索引 `idx_archive_documents_identity` |
| `file_name` | `varchar(1024)` | 非空 |
| `source_url` | `text` | 原文链接 |
| `title` | `varchar(1024)` | |
| `summary` | `text` | |
| `created_at` / `updated_at` | `timestamptz` | |

按身份 upsert；删文件时同步删行；重建 Meilisearch 时读 `source_url` 写入 `link`。

### `search_events`

`/api/search` 成功后写一行。`keyword`、`created_at` 有普通索引。

| 列 | 类型 |
| --- | --- |
| `id` | `bigint` PK |
| `keyword` | `varchar(255)` 非空 |
| `result_count` | `bigint` 非空默认 0 |
| `created_at` | `timestamptz` |

### `archive_click_events`

归档页成功加载后写一行。`domain` / `file_name` / `path` / `created_at` 有索引。

| 列 | 类型 |
| --- | --- |
| `id` | `bigint` PK |
| `domain` | `varchar(255)` 非空 |
| `file_name` | `varchar(1024)` 非空 |
| `path` | `varchar(1400)` 非空 | 标准化 `/archive/{domain}/{file}` |
| `keyword` | `varchar(255)` | 可选搜索词 |
| `created_at` | `timestamptz` |

---

## 内容发现

发现包把抽出的文章交出去时只把 `assessment_state` 写成 `pending`；评估入队在 `api/api/jobs.go`。Sitemap 能力已关闭（`000028`）：历史 `endpoint_type/type = sitemap` 的源被禁用，sitemap 回溯标为暂停，**表和列仍保留**。

### `discovery_sites`

站点节点。`host_key` 唯一。`status` 有 CHECK：`seed` / `observing` / `active` / `paused` / `blocked` / `non_blog`。

关键列：`root_url`、`host_key`、`domain_key`（`000018`，PSL 规范化由启动时 Go backfill 补全）、`display_name`、`discovery_method`、`graph_depth`、`crawl_allowed`、`robots_status`、`first_discovered_at`、`last_referenced_at` / `last_article_at` / `last_validated_at` / `activated_at` / `next_graph_scan_at`、`operational_pause`、`operational_details`。

### `discovery_domain_blacklist_entries`

按注册域屏蔽抓取。`domain` 唯一。`000021` 默认插入 `csdn.net`。命中后候选 `processing_state = domain_blocked`。

### `discovery_sources`

RSS/Atom 或同站入口。`url` 唯一。`site_id` → `discovery_sites` `ON DELETE SET NULL`。

| 列组 | 列 |
| --- | --- |
| 身份 | `name`、`url`、`type`（`feed` / `site` / `rsshub`；历史可有 `sitemap`）、`endpoint_type`（默认 `legacy`）、`site_id`、`user_managed`、`priority`、`crawl_host` |
| 开关与调度 | `enabled`、`next_fetch_at`、`next_due_at`、`failure_count`、`backoff_until`、`backoff_reason` |
| 条件请求 | **`etag`**（代码写入列）、`last_modified`、`crawl_config` |
| 观测 | `last_fetched_at`、`last_attempt_at`、`last_success_at`、`last_error` |

本运行库另有历史列 **`e_tag`**（早期 GORM 把 `ETag` 映射成 `e_tag`）。当前模型是 `gorm:"column:etag"`，**只读写 `etag`**。`e_tag` 是死列，不要在新代码里用。

### `discovery_candidates`

抓取入口与处理状态。`url` 唯一，`material_id` 非空；多个入口可对应同一 material。`representative_id` 自引用 `ON DELETE SET NULL`。正文和评估属性通过只读视图 `discovery_candidate_details` 联查；Go 写入使用 `discovery.UpdateCandidate(s)`。

**状态机（不要只看 `status`）：**

| 字段 | 含义 | 典型值 |
| --- | --- | --- |
| `status` | 遗留全局态；个人态已拆到 `user_material_states` | `new` / `read` / `ignored` / `archived` |
| `processing_state` | 抓取/抽取管道 | `discovered`（遗留）、`fetch_pending`、`fetching`、`ready`、`review`、`failed`、`ineligible`、`domain_blocked` |
| `dedupe_state` | 去重 | `pending` / `ready` |
| `assessment_state` | 位于 `material_article_states`；discovery 写 `pending` | `pending` / `ready` / `review` / `degraded` |
| `eligibility_state` | 位于 `material_article_states`；推荐资格 | `unknown` / `eligible` / `review` / `ineligible` |

候选实体保留 URL/规范化 URL/最终 URL、crawl host、去重簇/代表、抓取尝试/错误/调度、抓取与发现时间。`000034` 删除标题、摘要、正文、作者、字数、向量、模型状态、评分及单一来源字段。

主题/摘要的权威写回在评估成功之后，不要再加独立 enrichment hop。

### `discovery_candidate_provenances`

迁移前来源证据表，保留为历史审计。新读写全部使用 `material_provenances`，其中 candidate/site/source FK 均为 `ON DELETE SET NULL`，material FK 为 RESTRICT。

### `discovery_fetch_runs`

单次抓取审计。`source_id` CASCADE；`site_id` SET NULL。含 HTTP 状态、条件请求头、`not_modified`、新/重复/失败计数、`error_category`。

### `discovery_backfill_states`

按 `(site_id, strategy)` 唯一。`000028` 把 `strategy = sitemap` 且未完成的游标标为 `paused`。`owner_requested_at` 来自 `000020`。

### `discovery_site_edges`

Blogroll 图。`edge_key` 唯一。`from_site_id`/`to_site_id` CASCADE。CHECK：禁止无关系类型的自环。

### `discovery_article_content_versions`

保留旧抽取版本及其 ID，新增 `material_id`/`material_version_id` 对应核心版本。`(candidate_id, content_version)` 唯一；金标引用继续有效。

### `discovery_duplicate_clusters` / `discovery_candidate_identities` / `discovery_duplicate_review_signals`

去重簇（代表 `ON DELETE RESTRICT`）、身份别名 `(candidate_id, kind, identity_key)` 唯一、用户报重复信号（无指向 `recommendation_items` 的 FK）。

### `user_material_states`

由 `user_candidate_states` 重命名，每用户每内容一行，`(user_id, material_id)` 唯一。candidate 只保留历史入口，可空、删除时 SET NULL。记录曝光、打开、阅读、归档与当前反馈；同一内容的不同 URL 共享状态。

### `discovery_candidate_feedbacks`

早期全局反馈（`candidate_id` + `action`）。无 FK。新路径以 `user_material_states` 与 `recommendation_feedbacks` 为准。

### `discovery_legacy_candidate_state_reviews`

`000011` 把历史上全局 `read`/`ignored`/`archived` 收进 owner 复核。`candidate_id` 唯一 CASCADE。

### `discovery_site_operational_stats` / `discovery_source_schedule_decisions`

站点计数（无「源质量总分」）；源调度决策 `source_id` 唯一。有意不把运营分乘进资格。

---

## 文章评估

### `discovery_article_assessments`

评估行归属 material；唯一键 `(material_id, content_version, assessor, assessor_version, policy_version)`，并指向 `material_version_id`。candidate 仅作历史入口，删除时 SET NULL。原有分数、摘要和关键词保留。评估任务使用 material ID；旧 candidate 参数通过版本映射兼容。

Go 类型 `assessment.ArticleAssessment` 的 `TableName()` 固定为此表。

### `assessment_llm_calls`

安全观测：禁止写 prompt 或模型正文。无业务 FK。`000029` 建表，`000030` 加 `predicted_tokens`、`predicted_ms`。索引：`(stage, created_at)`、`candidate_id`。

---

## Owner 金标工作流（`api/assessmenteval`）

私有标注，启动时 `RecoverInterruptedEvaluations`。同时最多一个活跃 run：部分唯一索引 `idx_article_assessment_workflow_active`（`status IN pass_one, waiting_pass_two, pass_two, adjudication, human_complete, evaluating, evaluation_failed`）。

### `article_assessment_workflow_runs`

种子、manifest 摘要、策略版本、双 pass 时间戳、评估 generation、模型/prompt、`report_json`。

### `article_assessment_workflow_items`

样本。`(run_id, sample_id)`、`(run_id, position)` 唯一；pass two / adjudication 位置有部分唯一索引。`candidate_id` **RESTRICT**；`content_version_id` **RESTRICT**（避免标金标时删正文）。`000024` 增加 pass one skip 审计。

### `article_assessment_workflow_labels`

人工分。`(run_id, sample_id, pass)` 唯一。CHECK：`pass` 1–3；`quality`/`depth`/`evergreen` 0–100。

### `article_assessment_workflow_scores` / `article_assessment_workflow_calls`

模型打分（`model_run` 1–2）与 token 观测。均 `run_id` CASCADE。

---

## 推荐

### `recommendation_settings`

每用户一行（`user_id` 唯一）。日限额、时区、生成时刻、窗口天数、探索率、偏好主题/语言/长度/深度、收藏源、`enabled`。`000014` 增加偏好列。

### `recommendation_days`

每用户每个本地日期一行。`UNIQUE (user_id, recommendation_date)`，日期列类型为 `date`。

生命周期：`draft` → `published`；补推后 `supplemented`；失败 `failed`。历史值 `pending`/`generated` 在 `000015` 与启动 backfill 中分别映射为 `draft`/`published`。接口层还有非落库状态 `missing`。

发布后预生成 digest 摘要（`000026`：`summary_text` / `summary_highlights` / `summary_topics` / `summary_model` / `summary_prompt_version` / `summary_actual_count` / `summary_generated_at`）。`GET /recommendations/today/summary` 只读缓存。另有 `policy_version`、`shortage_reasons`、`degraded`、`supplement_policy`、`audit_version`。

### `recommendation_feed_batches`

个性化发现 feed 批次。部分唯一索引：每用户最多一个 `status = active`。

### `recommendation_items`

一条推荐。CHECK `recommendation_items_exactly_one_parent`：**要么** `day_id` **要么** `feed_batch_id`，不能同时有、也不能都空。

- `day_id` → `recommendation_days` CASCADE；`(day_id, candidate_id)` 唯一。
- `feed_batch_id` → `recommendation_feed_batches` CASCADE；部分唯一 `(feed_batch_id, candidate_id) WHERE feed_batch_id IS NOT NULL`。
- `material_id` / `material_version_id` 指向内容及版本；`candidate_id` 为可空历史入口，删除时 SET NULL；`assessment_id` SET NULL。
- `(day_id, material_id)` / `(feed_batch_id, material_id)` 在 `NOT legacy_duplicate` 范围唯一；迁移合并造成的已发布重复条目标记例外并保留快照。

发布时冻结 snapshot_* 列（`000003`/`000016`/`000017`）。`000013` 增加 `content_version`、`content_updated`、`cooldown_repeat`。跨日去重靠 cooldown 与版本，**没有**永久的 `(user_id, candidate_id)` 唯一约束。

### `recommendation_feedbacks`

表名是复数（`000002` 从 `recommendation_feedback` 迁来）。`is_current` + 可空 `current_key` 唯一索引保证同一 item 当前反馈至多一条。`supersedes_id`、`closed_reason`、`reverted_at`。`metadata` 为 jsonb。

### `user_block_rules` / `user_recommendation_profiles`

屏蔽规则（`user_id, active, rule_type, rule_value` 查找索引；`feedback_id` 无 FK）。画像：正负 embedding、主题/源/风格权重为 jsonb；`feedback_reset_at` 为偏好重置边界。

---

## 任务队列（River）

`database.RunDatabaseMigrations` 在 Goose 之后跑 River migrator。本库表：`river_job`、`river_queue`、`river_client`、`river_client_queue`、`river_leader`、`river_migration`。这些表由 River 拥有，不要用手写 Goose 改它们。

运行中的队列名：

| 队列 | 用途 |
| --- | --- |
| `default` | 每日 digest、digest 摘要 |
| `discovery_crawl` | 抓源 / blogroll / 回溯 / 处理候选（自动消费） |
| `article_assessment` | `assessment_assess_article`；启动时 **暂停**，由 owner 手动开队列 |

本实例已出现的 `river_job.kind`：

- `discovery_fetch_source`
- `discovery_scan_blogroll`
- `discovery_backfill_site`
- `discovery_process_candidate`
- `assessment_assess_article`
- `recommendation_generate_daily`
- `recommendation_generate_digest_summary`

`JobEnqueuer` 在 `api/jobqueue`；discovery 包不 import 该包。

---

## 迁移账本

`goose_db_version`：`id`、`version_id`、`is_applied`、`tstamp`。仓库当前最新迁移为 **35**（`000035_material_relations`），部署版本须查询实际数据库确认。

多数 Down 是 **保留数据的 no-op**（`SELECT 1`），回滚 Goose 版本号不会删 v3 表。不要把 Down 当成可逆删表。

---

## 运行库与纯 Goose 空库的差异

以下是拆表前历史 Compose 库与「空库只跑 `000001`–`000031`」的差别；`000032`–`000035` 会搬迁其中的内容字段：

| 现象 | 本运行库 | Goose 空库预期 |
| --- | --- | --- |
| `discovery_candidates.archived_task_id` | 有 `varchar(36)`，代码会写归档任务 ID | `000001` 的 `CREATE TABLE` **没有**此列，后续迁移也未 `ADD COLUMN` |
| `discovery_sources.e_tag` | 有死列 | 不会出现；只有 `etag` |
| `topics` / `entities` / `crawl_config` | `text` | `000001` 转为 `jsonb` |
| `score` / `quality_score` / `depth_score` | `numeric` | `double precision` |
| `archive_stats.file_count`、若干计数/word_count | `bigint` | 部分迁移写的是 `integer` |
| 部分 GORM `index` 标签对应的索引（如 `content_type`、`enrichment_status`、`dedupe_key`） | 存在 | 对应 Goose 文件未必建了同名索引 |

读取历史库时以实际类型为准。`000033` 检测可选的旧 `archived_task_id` 并搬迁到 `material_archive_links`；新代码不再向候选表写该字段。

---

## 结构关系（归档核心，与早期文档对齐并补全）

```text
users
  id PK
  username UNIQUE
  password
  role
  created_at / updated_at

archive_tasks
  id PK
  url INDEX
  domain
  status INDEX
  file_name / error / external_task_id
  created_at / updated_at / started_at / finished_at

archive_documents
  id PK
  (domain, file_name) UNIQUE
  source_url / title / summary
  created_at / updated_at

archive_stats
  source PK
  file_count
  created_at / updated_at
```

发现与推荐的外键见上文「领域关系」。完整列清单以本运行库 `information_schema.columns` / `pg_constraint` 为准；代码侧模型分别为 `auth.User`、`archive.*`、`discovery.*`、`assessment.*`、`assessmenteval.*`、`recommendation.*`。
