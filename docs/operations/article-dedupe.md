# 全库指纹去重与代表文章评估

## 行为

抓取仍全速运行。迁移 000037 增加 PostgreSQL 指纹、LSH 桶、版本化成员、运行检查点和回滚记录；迁移 000038 增加运行级 `auto_apply` 与设置级 `pending_assessment` 两个持久化编排意图；River 自身迁移先于应用迁移执行。首次部署默认 `shadow`，不自动重新聚类生产库存。

近重复使用 NFC 和空白归一化后的 5 字符 shingles、128 维固定 MinHash 和 16×8 LSH。候选覆盖全库当前已抽取正文，正文哈希精确匹配保留。只有实际 Jaccard ≥0.90、长度比 ≥0.90、数字序列一致且至少 200 个非空白字符的非精确重复可以合并。短文只支持精确匹配；同主题、翻译、较大改写不属于本功能。

每个成员必须直接匹配代表，没有传递继承。完整性达标（簇内最长有效正文的 95%）的成员中优先复用当前模型、策略及正文版本的评估，否则保持合格代表稳定。近重复不合并 material、不迁移来源、不复制评分或摘要。独立评估和已发布推荐历史保留。

覆盖状态由接口即时计算：`dedupe_pending`、`independent`、`waiting_representative`、`covered`、`representative_failed`。成员的 `assessmentState` 不伪装成独立评估成功，`currentAssessmentId` 不指向其他文章。

## 预览及启用

启用去重有两条路径：随评估自动编排，或手动预览流程。

**自动编排（无需单独请求去重 API）**：`POST /api/admin/assessment/queue/run` 启动评估前自动检查去重状态。`shadow` 模式（去重未生效）下自动创建带 `auto_apply` 的全库运行并记录 `pending_assessment` 持久化意图，响应 `Data` 为 `{snapshot, dedupeRun}`。延迟期间 `GET /api/admin/assessment/queue` 同样附带构建中的运行，评估面板把整个编排呈现为一次进行中的评估：状态徽章变蓝色"评估进行中"，"执行 LLM 评估"按钮转 loading，持久横幅显示全库查重的阶段进度（运行号、阶段、百分比、已处理数），每 5 秒刷新，直到评估被释放；不是只在点击瞬间弹一次提示。运行自动走完 `indexing` → `previewing` → `applying` → `active`，跳过 `ready` 人工检查点；激活后恢复流程先取消被取代的非代表作业、补齐代表评估，再自动恢复评估队列并清除意图。`active` 模式（去重已生效）下评估立即启动；`exact` 模式（运维显式回滚过）同样立即启动，绝不会在运维背后自动重新应用去重。自动编排跳过了下文第 4 步的人工抽查，适合已验证过算法质量的库存；首次启用仍建议先走手动预览流程。

**手动预览流程**：下面路径均以 `/api` 为前缀，使用现有 owner 登录认证。

1. `POST /api/admin/discovery/dedupe/runs` 创建预览，返回 `Data.id`。相同待处理/已就绪预览会复用运行 ID。
2. `GET /api/admin/discovery/dedupe/runs/{id}?cursor=0&limit=50` 查看报告及成员分页。使用 `nextCursor` 翻页，单页上限 250。
3. 状态由 `indexing` → `previewing` → `ready`。每批 250 个正文，检查点持久化。`highWaterId` 固定预览边界，避免持续抓取让预览永远无法完成；边界后的新增文章在应用后补齐。
4. 应用前抽查至少 300 对拟合并文章，确认没有独立阅读价值被误合并；按语言、长度、代码、来源和数字内容分层抽样。使用至少 1,000 篇样本内的全对验证检查候选召回率 ≥95%。算法只是词面近似，不能保证语义等价；不合格时不要应用。
5. `POST /api/admin/discovery/dedupe/runs/{id}/apply` 显式应用，状态 `applying` → `active`。每组在事务内重新检查当前正文，过期代表会重新选择或拆分，不照搬预览。
6. `GET /api/admin/discovery/dedupe/clusters/idx-{runId}-{representativeRepresentationId}` 查看成员、相似度及覆盖状态，同样支持 cursor/limit。

手动流程的应用不自动启动付费 LLM 消费；评估仍由 `POST /api/admin/assessment/queue/run` 启动（此时去重已 `active`，评估直接开始）。新增正文改为进入独立自动去重队列 `article_dedupe`（并发 2），去重完成后只登记代表评估。

## 报告与验证

- `totalCurrent` / `indexedCurrent`：当前可处理逻辑文章数与指纹覆盖数量，不按发现候选重复计数。
- `representatives`、`exactCovered`、`nearCovered`：当前版本的分层数量。
- `pendingTargetsSaved`：原先可独立进入评估且仍 pending 的非代表逻辑文章数，不把已被旧去重跳过的别名再次计作节省。
- `existingAssessmentReused`：代表中已有当前有效评估的数量。
- `run.cancelled`：清理的非代表待执行作业数；带 `dedupe_superseded` 标记的取消不计为评估失败。
- `latencyP50Ms` / `latencyP95Ms`：聚类验证阶段记录的毫秒耗时（不含队列等待）；每个成员还提供 `candidateCount`，报告提供 `maxCandidates`。

170K 生产规模需要在实际硬件验证：增量去重吞吐 ≥实测抽取峰值的 1.5 倍，同负载抓取吞吐下降 ≤10%。不要把小规模集成测试视作生产规模证据。重复比例及成本降幅以报告和实际 LLM 调用记录为准。

## 故障、恢复与回滚

自动去重任务按正文版本/算法/运行阶段去重；完成作业不永久阻止后续修复。启动、运行状态切换以及每分钟定时器都会补齐丢失的交接、失效成员及黑名单代表。定时器每分钟只执行两个走部分索引的漂移计数（未应用成员、已抽取但未去重的候选）；只有在探测到漂移、每 30 次 tick 的安全网，或启动/状态切换时才扫描全库，因此已收敛的库存不会因为恢复任务产生全库开销。运行错误写入 `run.error`，River 自动重试，并从持久化检查点继续。索引计算不在写事务内执行，批次使用缓存指纹，避免在抽取 worker 扫描全库。

进程在批次中途被杀（如重建容器）时，运行行停在最后提交的检查点，零丢失零重复。孤儿 `running` 批次作业由下次启动的 `reconcileDedupeJobs` 改回 `available` 立即续跑——必须靠启动回收，因为 River 的 rescuer 默认要 1 小时才回收孤儿作业，而按参数去重的唯一约束（唯一状态含 `running`，防重复批次）在此期间会挡住恢复的重新入队。进程消失且不再重启的极端场景由 rescuer 的 1 小时回收兜底。

代表补登入队是单遍扫描加分块投递：过滤后的计划从已应用成员的聚合驱动并对整个匹配集排序，按批游标翻页会让每一页 250 行都重付一次全量工作（137k 候选上每页 ~1.4 秒、~350 次启动即 ~8 分钟的持续负载），单遍只付一次。

评估激活不等待全局设置行锁：`updateAssessmentMaterial` 先锁该 material 的 `material_article_states` 行，再在该锁内计算代表资格；聚类应用与回滚在写投影前获取同一把 material 行锁。单一锁类型保证无死锁，且互不相关的文章不会被同一批去重阻塞。写投影的阶段（`applying`/`rolling_back`）每批 25 组而非 250，使行锁持有时间为毫秒级。索引与预览阶段仍每批 250，因为它们不写 material 状态。

评估闸门恒为推荐闸门的子集：`discovery.DedupeRepresentativeClause` 是唯一的投影级代表判定，评估、推荐、向量召回和站点统计都原样使用它；`FilterDedupeAssessmentCandidates` 只在其之上按模式进一步收窄。任何模式下都不会为推荐永远无法呈现的文章支付 LLM 费用。

`existingAssessmentReused` 由单条聚合查询计算，报表请求耗时不随库存规模增长。

`pending_assessment` 与 `auto_apply` 均为持久化意图，崩溃不会丢失评估请求：激活与释放之间崩溃时，每分钟恢复检测到 active 模式下的待处理意图，会在剪枝与代表补登之后重新释放评估并清除意图；预览完成与应用请求之间崩溃时，恢复会把带 `auto_apply` 的 `ready` 运行直接推入 `applying`；若更新的运行世代已取代该预览，推进返回 409 并被忽略，不会与运维手动操作竞争。

代表正文更新后，旧覆盖立即失效；黑名单变化会重新选择可用代表。调用前及结果激活时再次检查代表关系，已开始的 LLM 调用可以完成，结果只保留独立历史，不激活已失去代表资格的文章。运行中的调用不由队列清理取消。

`POST /api/admin/discovery/dedupe/runs/{id}/rollback` 撤销当前活跃运行的投影变更，状态 `rolling_back` → `rolled_back`。版本和投影仍匹配时恢复快照；发生后续正文/资格变化时解除覆盖，避免恢复过期状态。保留指纹、原文、独立评估和推荐历史，切换为 `exact` 模式（新近重复不再自动应用）。仅当前活跃运行可回滚；其他构建/应用运行进行中会返回 409。

## 开发验证

```sh
cd api
go test ./...
# 必须使用名称以 dataark_v3_verify 开头的一次性空数据库。
DATAARK_POSTGRES_TEST_DSN='host=127.0.0.1 port=5432 user=postgres dbname=dataark_v3_verify_dedupe sslmode=disable' \
  go test ./bootstrap -run '^TestPostgresV3MigrationsRiverRestartAndPGVector$' -count=1
```

该集成测试验证生产迁移及回滚（含 000038 自动编排列的降级）、有效评估复用、待执行作业取消、并发聚类、在途评估失去资格后不激活和运行回滚。SQLite 用例另覆盖 500 条窗口之外的检索、预览不改变生产、过期快照、新增补齐、黑名单代表、链式聚类，以及自动编排：shadow 模式延迟评估并自动构建应用到 active、释放恰好发生在抑制生效之后、exact 模式不重新应用、崩溃后恢复释放待处理评估并推进停滞的 auto-apply 运行。
