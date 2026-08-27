# 数据库设计

本文档描述 **当前运行中的 PostgreSQL 库**（Compose 服务 `database`，镜像 `pgvector/pgvector:pg17`）在 2026-08-27 的实际 schema，并对照代码里的权威来源。

- 生产 schema **只**由 Goose 编号迁移定义：`api/migrations/000001`–`000030`。
- 启动路径：`api/bootstrap.InitDB()` → `api/database.InitDB()` 连库 → `RunDatabaseMigrations()`（Goose `Up` + River migrator）。
- GORM `AutoMigrate` **只给 SQLite 测试当方言替身**，不定义生产 schema。
- 模型分散在 `api/auth`、`api/archive`、`api/discovery`、`api/assessment`、`api/assessmenteval`、`api/recommendation`。没有 `api/common/`。
- 改列/建表必须新增编号 Goose 文件；不要假设改 GORM tag 就会改生产库。

本实例快照：`goose_db_version.version_id = 30`；扩展 `vector 0.8.3`、`pgcrypto 1.3`、`plpgsql`；`public` 下 **44** 张表（含 Goose 账本与 River）。

这份运行库是「早期 GORM AutoMigrate + 后续 Goose」叠出来的，因此个别类型、多余列与空库纯跑 Goose 不完全相同。文末「运行库与纯 Goose 空库的差异」单独列出。

## 总体约定

- ORM：GORM；生产方言：PostgreSQL。
- 表名：GORM 默认复数，`User` → `users`。评估行故意表名为 `discovery_article_assessments`。
- 主键：业务表多为 `BIGSERIAL`；`archive_tasks.id` 是 `varchar(36)`（UUID）；`archive_stats.source` 是域名主键；`discovery_duplicate_clusters.cluster_id` 是字符串主键。
- 时间：DSN 带 `TimeZone=Asia/Shanghai`；时间列多为 `timestamptz`。
- 外键：归档/用户/搜索事件之间仍无 FK。发现图、评估、推荐、金标工作流、River 客户端队列有显式 `REFERENCES`。
- JSON：推荐反馈 `metadata`、条目 `reason_metadata`、用户画像向量权重为 `jsonb`。候选 `topics`/`entities`、源 `crawl_config` 在 **本运行库** 仍是 `text`（Goose `000001` 意图是 `jsonb`）。
- 向量：`discovery_candidates.embedding` 类型为无维度 `vector`。本实例 71845 行候选中 **0** 行已写入向量。
- HTML 本体与 Meilisearch 索引不在 Postgres 里；库只存元数据与事件。

## 领域关系

```text
users
  ├── user_candidate_states ──────────── discovery_candidates
  ├── recommendation_settings
  ├── recommendation_days ── recommendation_items ──┬── discovery_candidates
  ├── recommendation_feed_batches ── recommendation_items
  ├── recommendation_feedbacks ─────────────────────┘
  ├── user_block_rules
  └── user_recommendation_profiles

discovery_sites
  ├── discovery_sources ── discovery_fetch_runs
  │                    └── discovery_source_schedule_decisions
  ├── discovery_site_edges (from/to)
  ├── discovery_site_operational_stats
  ├── discovery_backfill_states
  └── discovery_candidate_provenances ── discovery_candidates

discovery_candidates
  ├── discovery_article_content_versions
  ├── discovery_article_assessments ◄── current_assessment_id
  ├── discovery_candidate_identities
  ├── discovery_duplicate_clusters (representative)
  ├── discovery_duplicate_review_signals
  ├── discovery_candidate_feedbacks          (历史全局反馈)
  └── discovery_legacy_candidate_state_reviews

assessment_llm_calls                         (观测，无 FK)
article_assessment_workflow_*                (owner 金标，挂 candidate / content_version)

archive_tasks / archive_documents / archive_stats / search_events / archive_click_events
  （彼此无 FK；候选 archived_task_id 只是字符串引用）

river_job / river_queue / river_client / …   (River 自管)
goose_db_version                             (Goose 账本)
```

当前产品流：发现自动爬取 → 候选 `assessment_state=pending` → 人工开启的评估队列写不可变评估行 → 推荐只读 ready/eligible 库存生成每日 digest 与发现 feed。

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

管道中心。`url` 唯一。`representative_id` 自引用 `ON DELETE SET NULL`；`current_assessment_id` → `discovery_article_assessments` `ON DELETE SET NULL`。

**状态机（不要只看 `status`）：**

| 字段 | 含义 | 典型值 |
| --- | --- | --- |
| `status` | 遗留全局态；个人态已拆到 `user_candidate_states` | `new` / `read` / `ignored` / `archived` |
| `processing_state` | 抓取/抽取管道 | `discovered`（遗留）、`fetch_pending`、`fetching`、`ready`、`review`、`failed`、`ineligible`、`domain_blocked` |
| `dedupe_state` | 去重 | `pending` / `ready` |
| `assessment_state` | 评估；discovery 只写 `pending` | `pending` / `ready` / `review` / `degraded` |
| `eligibility_state` | 推荐资格 | `unknown` / `eligible` / `review` / `ineligible` |

正文与模型列（本库类型）：`canonical_url`、`normalized_url`、`final_url`、`title`、`summary`、`author`、`body_text`、`language`、`word_count`（bigint）、`content_hash`、`content_version`、`body_changed_at`、`topics`/`entities`（text，存 JSON 文本）、`content_type`/`content_style`、`metadata_confidence`、`quality_score`/`depth_score`/`score`（numeric）、`enrichment_status`/`enrichment_error`/`embedding_model`/`llm_model`/`prompt_version`/`enriched_at`、`embedding`（`vector`）、`duplicate_cluster_id`/`dedupe_key`、`processing_attempts`/`processing_error`/`processing_error_type`/`next_processing_at`/`fetched_at`/`extracted_at`、`assessment_error`、`eligibility_reasons`、`published_at`/`published_confidence`、`first_seen_at`/`last_seen_at`、`crawl_host`、`archived_task_id`（见文末差异）、`source_id`/`source_name`。

主题/摘要的权威写回在评估成功之后，不要再加独立 enrichment hop。

### `discovery_candidate_provenances`

候选如何被发现。`provenance_key` 唯一。FK：`candidate_id` CASCADE、`site_id` CASCADE、`source_id` SET NULL。

### `discovery_fetch_runs`

单次抓取审计。`source_id` CASCADE；`site_id` SET NULL。含 HTTP 状态、条件请求头、`not_modified`、新/重复/失败计数、`error_category`。

### `discovery_backfill_states`

按 `(site_id, strategy)` 唯一。`000028` 把 `strategy = sitemap` 且未完成的游标标为 `paused`。`owner_requested_at` 来自 `000020`。

### `discovery_site_edges`

Blogroll 图。`edge_key` 唯一。`from_site_id`/`to_site_id` CASCADE。CHECK：禁止无关系类型的自环。

### `discovery_article_content_versions`

不可变抽取版本。`(candidate_id, content_version)` 唯一；`candidate_id` CASCADE。

### `discovery_duplicate_clusters` / `discovery_candidate_identities` / `discovery_duplicate_review_signals`

去重簇（代表 `ON DELETE RESTRICT`）、身份别名 `(candidate_id, kind, identity_key)` 唯一、用户报重复信号（无指向 `recommendation_items` 的 FK）。

### `user_candidate_states`

每用户每候选一行，`(user_id, candidate_id)` 唯一。FK 到 `users`、`discovery_candidates` 均为 CASCADE。记录曝光、打开、阅读、归档与当前反馈。列表过滤 `read`/`ignored`/`archived` 走这里，而不是改全局 `discovery_candidates.status`。

### `discovery_candidate_feedbacks`

早期全局反馈（`candidate_id` + `action`）。无 FK。新路径以 `user_candidate_states` 与 `recommendation_feedbacks` 为准。

### `discovery_legacy_candidate_state_reviews`

`000011` 把历史上全局 `read`/`ignored`/`archived` 收进 owner 复核。`candidate_id` 唯一 CASCADE。

### `discovery_site_operational_stats` / `discovery_source_schedule_decisions`

站点计数（无「源质量总分」）；源调度决策 `source_id` 唯一。有意不把运营分乘进资格。

---

## 文章评估

### `discovery_article_assessments`

不可变评估行。唯一键 `(candidate_id, content_version, assessor, assessor_version, policy_version)`。`candidate_id` CASCADE。`000027` 增加 `summary`、`keywords`（article-value-v4）。轴分：`information_density`、`originality`、`completeness`、`evidence`、`readability`、`depth`、`evergreen_value`、`overall_quality`、`confidence`、`reasons`。

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
- `candidate_id` CASCADE；`assessment_id` SET NULL。

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

`goose_db_version`：`id`、`version_id`、`is_applied`、`tstamp`。本实例最新已应用版本 **30**（`000030_assessment_llm_decode_timings`）。

多数 Down 是 **保留数据的 no-op**（`SELECT 1`），回滚 Goose 版本号不会删 v3 表。不要把 Down 当成可逆删表。

---

## 运行库与纯 Goose 空库的差异

本 Compose 库在 Goose 之前用 GORM AutoMigrate 建过基表，因此与「空库只跑 `000001`–`000030`」会有这些差别：

| 现象 | 本运行库 | Goose 空库预期 |
| --- | --- | --- |
| `discovery_candidates.archived_task_id` | 有 `varchar(36)`，代码会写归档任务 ID | `000001` 的 `CREATE TABLE` **没有**此列，后续迁移也未 `ADD COLUMN` |
| `discovery_sources.e_tag` | 有死列 | 不会出现；只有 `etag` |
| `topics` / `entities` / `crawl_config` | `text` | `000001` 转为 `jsonb` |
| `score` / `quality_score` / `depth_score` | `numeric` | `double precision` |
| `archive_stats.file_count`、若干计数/word_count | `bigint` | 部分迁移写的是 `integer` |
| 部分 GORM `index` 标签对应的索引（如 `content_type`、`enrichment_status`、`dedupe_key`） | 存在 | 对应 Goose 文件未必建了同名索引 |

读本库数据、写运维 SQL 时以 **本表实际类型** 为准。给新环境写迁移时仍以 `api/migrations/*.sql` 为准；若要让空库也有 `archived_task_id`，需要单独加编号迁移，而不是依赖 AutoMigrate。

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
