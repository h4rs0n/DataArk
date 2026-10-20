# 现存数据库全域数据审计

调查日期：2026-10-20（Asia/Shanghai，约 21:54–22:08）。仅只读调查，运行库 Goose 000045；没有执行迁移、数据修复、任务恢复或服务重启。

## 调查方法与结论边界

此次从数据库全部现存表、字段、约束及记录出发，寻找逻辑重复、关联缺失、状态冲突、无效计数、异常时间和内容投影不一致。命中异常后才查看当前读写代码，不按迁移文件筛选调查范围，也不限制异常发生年代。

对 **61 张基础表**做全量基础扫描：145 个计数/时间断言、23 个状态取值观察、9 组用户归属检查；另做 78 项跨域业务一致性查询及 7 项字段形状检查。发现记录后进一步查具体 ID、时间、版本及队列关系。此前 Material 审计的 33 项归属检查继续作为补充，结果见[前一份专项记录](2026-10-20-migration-data-audit.md)。

这覆盖了当前全部基础表的基础质量扫描，**不等于证明每个历史发布均无缺陷，也不等于语义审阅了全部文章**。现存数据无法还原已被删除或覆盖、且没有审计日志的历史值；确定有异常与确定是某次迁移导致是两件事。

扫描采用只读事务；多表大查询使用一致性快照，分组查询之间后台继续运行。为限制查询开销，设置语句超时；一条初始指纹检查超时后，改为一次分组汇总重新完成。另一条查询触及容器共享内存限制后，改为事务内串行、低内存执行完成，没有改全局参数。所有结论采用完成的查询结果。

复查脚本：[只读全域扫描 SQL](2026-10-20-existing-data-audit.sql)。临时原始证据位于本次工作环境 `/tmp/dataark-full-data-audit/`；报告保留了主要数量及具体异常 ID。

## 新发现及实际影响

### 1. 90 个旧归档文件没有当前文档元数据

从数据出现矛盾开始核对：

- `archive_stats` 有 11 个来源、合计 **90** 个文件，统计更新于 2026-06-28。
- `archive_documents` 为 **0** 行，`material_archive_links` 也为空。
- 仅为核对这一矛盾，只读统计运行 API 的 `/archive` 挂载目录两层内 HTML 数量，结果同为 **90**；没有读取 HTML 正文。
- `archive_tasks` 只有 2 条较早的失败记录，无法代表这 90 个旧文件；已有 12 条历史点击事件也证明旧归档功能曾被使用。

**结论：物理文件仍在，但当前文档元数据表没有登记这些旧文件。** 依赖 archive_documents 的推荐候选、来源 URL 和 Material 内容关联无法覆盖这批文件；不代表依赖磁盘/搜索服务的所有旧归档入口都失效。

观察到数据缺口后再核对代码：启动的 `BackfillMaterialContent` 以 archive_documents 为输入，因此空表会直接完成 archive-content checkpoint；修复问题表为空也不能证明所有磁盘旧文件都已检查。`BackfillArchiveDocumentMetadataFromDisk` 虽有定义，当前生产代码中未发现调用入口。

建议先生成 90 个文件到文档元数据的可核对映射，验证标题、来源 URL 和 Material 去重后再补登记；不要通过清空 checkpoint 期待空表自行恢复。本次没有补登记或重建索引。

### 2. 22 个候选长期停在 fetching，且没有处理任务

22 个候选的 processing_state=fetching，最早更新时间为 **2026-07-15**，最近为 **2026-10-09**；全部没有存活的 discovery_process_candidate 任务，也没有 next_processing_at。

示例：候选 **2822、2823、2824、2825、3896、3897、3898、3899、4851、4852** 停在 7 月；另有 **120221、122222、122224、122225、122312、137386、137388、137524、138192、139048、139487、139515** 停在 10 月。

相比之下，27 个 fetch_pending 候选全部有存活任务。代码中的恢复查询包含 fetch_pending、extract_pending、dedupe_pending 等，但不包括 fetching，所以这 22 条可能长期无法自动恢复。

当前抓取队列本身处于暂停、auto_start=false；暂停解释正常 available 任务的积压，**不能解释这 22 条根本没有任务的 fetching 状态**。建议只对确认不在执行的记录恢复待抓取并幂等补任务，继续尊重用户的暂停状态。

### 3. 当前评估与投影分数不一致：material 3140

标题《GHSL-2020-050: Arbitrary code execution in Pebble Templates》：

| 字段 | 当前文章状态 | 指针指向的评估 4174 |
| --- | ---: | ---: |
| 质量 | 0.90 | 1.00 |
| 深度 | 0.95 | 1.00 |

该记录为 ready/eligible，使用 `article-quality-v1+llm-v1`，时间在 **2026-07-19**；关联 1 条历史推荐。两个质量/深度检查命中同一个 material，不能算成两条异常。

这是确定的数据源数值不一致。仅凭当前记录不能证明是历史写入、旧分数定义还是搬迁造成；修复前要确认旧协议的投影规则，不能直接取较大分数覆盖。

### 4. 当前评估与展示摘要不一致：material 86975

标题《Travel Back to China: 2026 Edition》：当前评估保留中文模型摘要，material.summary 却是英文原始描述，非同一摘要。当前关键词投影检查全部一致，摘要仅发现这一条不一致，尚无推荐条目关联。

模型评估创建于 **2026-09-29**；material 更新于 **2026-09-30**，当前状态更新于 **2026-10-15**。现存记录支持“评估后的其他写入可能覆盖了展示摘要”的方向，但没有逐字段写入日志，不能仅凭时间证明具体写入者。

建议明确模型摘要与来源描述的存储职责，再决定恢复模型摘要或保留两者，避免只补值后再次被覆盖。

### 5. 日期质量异常跨越多个历史阶段

- **96** 个 material 的 published_at 早于公元 100 年：94 个在公元 1 年、2 个因时区折算落在公元前。示例 material **42320、72211、86902、49066、32638、795**。
- 另有 7 个 100–1969 年日期，其中含 1145 年等，需逐条结合来源判定，不能一律视为错误。
- **9** 个 material 的日期晚于调查日一天以上，最远到 **3017 年**；其中标题含“置顶公告”“Future Blog Post”等，可能是来源刻意置顶，不等于数据库损坏。
- 上述早期/未来日期记录均没有推荐条目关联。部分已传播到 provenance、旧正文版本和 material_versions，不能把这些副本数量相加当作不同文章。

公元 1 年很可能表示缺失日期被保存成时间零值，但最终根因仍需对照来源。当前日期解析函数只做格式解析，没有合理范围判定。建议把“来源原始日期”和“用于新鲜度/排序的可信日期”区分，保留原值及来源证据，不直接将置顶日期改成抓取日。

### 6. 少量历史时间顺序错误，影响较低

- user_material_states 有 **5** 条 first_exposed_at 晚于 last_exposed_at，ID **20、22、24、27、28**；均相差 **19 微秒**。
- 其中 **3** 条 updated_at 也早于 created_at，同样是 19 微秒，并非额外 3 个对象。
- 旧全局状态待审表有 **4** 条 updated_at 早于 created_at，差约 0.3–0.4 毫秒。

这些是真实的时间倒置，未发现曝光数量负值或错误用户归属。当前曝光冲突更新直接写入传入时间，没有取较晚时间；其行为允许乱序写入导致倒置，但不能单凭这点证明 7 月每条记录的最初成因。建议改为单调时间维护及最早/最晚合并，历史值按同一事件的 min/max 修复即可，优先级低于内容与任务问题。

## 跨历史阶段的兼容遗留

当前指针中仍有 **2,553** 条 `openai_compatible / article-quality-v1+llm-v1` 评估没有摘要和关键词，评估创建于 **2026-07-14 至 2026-08-03**。其中 2,496 个 material 仍为 eligible，947 个有历史推荐关联。

这些不是简单的无人引用历史行，而是仍被使用的旧协议当前评估；是否要求全部升级，属于兼容/重评策略。缺少后来才要求的字段不直接等于迁移丢失，不应伪造新字段或删除旧评估。

前次专项发现仍需一并处理：242 个无当前评估指针的 ready 状态、8,002 个版本零存活评估任务、9 个评论区正文污染候选、37 个 pending/eligible 的旧状态。上述数量来自对应事务快照；本轮没有自动清理。

## 已排除的误报与通过项

- **45 组共享 domain_key、涉及 149 个站点**，根 URL/host_key 不同，常见于 github.com 的 docs、cli 等子域。domain_key 是可注册域分组，host_key 才有唯一约束；不能按共享 domain_key 批量合并。没有重复 host_key 或重复来源 URL。
- **110 个推荐 feed 批次**中只有 1 个 active、109 个 replaced，符合历史保留；每个批次 actual_count 都与条目数量一致。66 个日推荐的数量、父子用户归属、排名、摘要计数也一致。
- 所有受检用户归属均有效；当前反馈、撤回、屏蔽规则及用户反馈投影未发现冲突。
- 评估数值范围、token 加总、reasoning/cached 子计数、Batch 条目与调用计数、供应商批次与分配条目数量均一致。
- 所有 94,873 个指纹均有 16 个分桶，签名长度正确，无孤立分桶，去重当前运行引用有效。
- 正文 representation 没有空正文或空 hash；当前强文本身份均能在所属 material 的历史正文中找到。
- 评估理由/关键词、游标、去重日志、推荐主题及摘要列表的受检文本 JSON 均可解析。唯一推荐设置中三个偏好字段是空字符串，当前读取会规范化成空数组，不作为损坏 JSON。
- 部分 eligible 分数在 0.20–0.42，按现行 QualityFloor=0.20 不能标成低于阈值；不能凭旧印象使用 0.45 判脏。
- 抓取和评估队列均被用户暂停，available 任务等待多日不单独算作脏数据。58 个当前已 ready 的候选仍有处理任务，也可能承担去重/移交，不直接判为重复执行故障。

## 建议处理顺序

先补齐旧归档的可核验元数据、恢复没有任务的 fetching 记录，并复核此前确认的正文污染；随后规范化错误 ready 状态和无效评估任务。再明确摘要/分数投影及旧协议重评策略，最后处理低风险时间倒置和可疑发布日期。

所有修复均需按对象生成具体集合，保留原始日期、旧版本、推荐快照及任务审计，不应全表重置状态或自动开启队列。本次仅记录调查结果及只读脚本。

## 基础表扫描覆盖

以下为同一次基础扫描事务中的实际行数，包含业务表、保留旧表和 River/迁移基础设施表；一张表进入此清单仅表示完成基础扫描，不代表所有字段都已完成语义验证。

| 基础表 | 行数 |
| --- | ---: |
| `archive_click_events` | 12 |
| `archive_documents` | 0 |
| `archive_stats` | 11 |
| `archive_tasks` | 2 |
| `article_dedupe_buckets` | 1,517,968 |
| `article_dedupe_changes` | 100,100 |
| `article_dedupe_fingerprints` | 94,873 |
| `article_dedupe_members` | 94,873 |
| `article_dedupe_runs` | 1 |
| `article_dedupe_settings` | 1 |
| `assessment_batch_items` | 169,148 |
| `assessment_batch_runs` | 2 |
| `assessment_batches` | 346 |
| `assessment_llm_calls` | 122,207 |
| `discovery_article_assessments` | 178,210 |
| `discovery_article_content_versions` | 100,186 |
| `discovery_backfill_states` | 1,847 |
| `discovery_candidate_feedbacks` | 10 |
| `discovery_candidate_identities` | 481,587 |
| `discovery_candidate_provenances` | 310,911 |
| `discovery_candidates` | 143,529 |
| `discovery_crawl_settings` | 1 |
| `discovery_domain_blacklist_entries` | 3 |
| `discovery_duplicate_clusters` | 185,227 |
| `discovery_duplicate_review_signals` | 2 |
| `discovery_fetch_runs` | 85,709 |
| `discovery_legacy_candidate_state_reviews` | 7 |
| `discovery_site_edges` | 52,384 |
| `discovery_site_operational_stats` | 4,689 |
| `discovery_sites` | 17,062 |
| `discovery_source_schedule_decisions` | 8,071 |
| `discovery_sources` | 8,158 |
| `goose_db_version` | 46 |
| `material` | 137,834 |
| `material_archive_links` | 0 |
| `material_article_states` | 137,834 |
| `material_candidate_versions` | 100,425 |
| `material_embeddings` | 0 |
| `material_identities` | 256,686 |
| `material_ingestion_issues` | 0 |
| `material_migration_checkpoints` | 4 |
| `material_provenances` | 516,151 |
| `material_redirects` | 5,549 |
| `material_representations` | 100,186 |
| `material_versions` | 100,186 |
| `recommendation_days` | 66 |
| `recommendation_feed_batches` | 110 |
| `recommendation_feedbacks` | 22 |
| `recommendation_items` | 1,694 |
| `recommendation_settings` | 1 |
| `river_client` | 0 |
| `river_client_queue` | 0 |
| `river_job` | 26,389 |
| `river_leader` | 1 |
| `river_migration` | 6 |
| `river_queue` | 5 |
| `search_events` | 20 |
| `user_block_rules` | 8 |
| `user_material_states` | 1,374 |
| `user_recommendation_profiles` | 1 |
| `users` | 2 |
