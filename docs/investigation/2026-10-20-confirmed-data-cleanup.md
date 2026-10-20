# 已确认异常数据清理记录

执行日期：2026-10-20（Asia/Shanghai）。依据[全域数据审计](2026-10-20-existing-data-audit.md)及[Material 专项记录](2026-10-20-migration-data-audit.md)，经用户明确授权清理后执行。运行库保持 Goose 000045，未部署工作区 000046，也未重启服务。

## 结果

运行库修复事务已提交，15 项目标验收均为零。33 项内容/版本归属检查除原有 269 条保守的 migration_uncertain 标记外均为零；该标记已在调查中复核，不属于本次需清理的错挂。

| 项目 | 实际修改 | 处理方式 |
| --- | ---: | --- |
| 旧归档文档 | 90 个 | 从既有 HTML 离线恢复标题、SingleFile 原始 URL 及正文，补登记 archive_documents 与 material 关联。90 个均有来源 URL 与可用正文。 |
| 无任务的 fetching 候选 | 22 条 | 改回 fetch_pending，幂等补入 22 个 discovery_process_candidate 任务。 |
| ready 但没有当前评估指针 | 242 个 material | 恢复 pending；226 个当前代表中复用已有 134 个任务，补齐缺少的 92 个任务；16 个非代表继续去重抑制。 |
| pending 却 eligible | 37 个 material | 改为 unknown/assessment_pending，保留旧评估指针及历史结果。 |
| 无效评估任务 | 8,027 个 | 取消 8,002 个版本零任务及 25 个其他过期任务，保留 args、任务行和本次修复 metadata。没有删除任务。 |
| 公元 1 年/公元前发布日期 | 96 个 material | 当前 published_at 改为 NULL；原始来源和不可变版本中的日期保留供追溯。 |
| 曝光时间倒置 | 5 条 | first/last 使用原有时间的 min/max；updated_at 至少不早于 created_at。没有重置曝光计数。 |
| 旧状态待审时间倒置 | 4 条 | updated_at 校正为 created_at，保留旧状态及待审身份。 |
| 分数投影不一致 | 1 个 material | material 3140 的当前质量、深度对齐其明确引用的评估 4174；没有修改历史评估及推荐快照。 |
| 摘要投影不一致 | 1 个 material | material 86975 的展示摘要恢复为当前评估摘要；原值已保存。 |
| 污染正文 | 9 个 material / 148 个候选 | 隔离到 review，清除当前评估激活指针、归零当前分数、禁用现有去重资格；保留不可变正文与历史模型评估。 |

取消任务时按不可变版本映射解析旧 candidate_id 作业，没有直接把候选版本号与可能经合并重排的 material 版本号相比。任务新增使用 River 插入 API，取消使用 JobCancelTx；没有手工拼装任务唯一键。

## 污染内容隔离的具体范围

material：`12432, 118601, 118602, 118603, 123875, 123876, 123877, 135740, 135742`。

进一步核对发现，material 12432 关联 **140 个候选 URL**；另 8 个 material 各关联 1 个候选，总计 **148**。共同正文是评论区列表，可能已有不同文章通过公共区域被误合并。

因此本次执行隔离，而没有删除历史、自动合并这 9 个 material 或直接批量重抓这些 URL。若在身份尚未拆分时重抓，多个不同 URL 仍会覆盖同一个 material，继续扩大污染。

隔离后的 9 个 material 均不能进入推荐/评估准入，148 个候选均为 review，9 条去重成员 applied=false。material 12432 的不可信模型摘要从当前展示投影移除，修改前摘要和评估仍有备份。

后续恢复需要按 URL 重建独立内容身份并重新抽取、再评估；这部分未在本轮凭现有错误正文推测执行。隔离使错误内容停止参与当前决策，并不表示已恢复其真实正文。

## 队列与历史保留

- 抓取队列 paused_at 仍为 **2026-10-09 05:51:09.726868 UTC**。
- 评估队列 paused_at 仍为 **2026-10-09 08:19:24.696369 UTC**。
- 新补的 **114** 个任务（22 抓取、92 评估）均进入上述暂停队列，没有启动 worker、访问原站或调用 LLM。
- 活动 Batch 运行仍为 2：status=preparing，admission_enabled=false，high_water=1727153，cursor=1490442。本轮成员范围没有扩展；新补的评估任务遵守后续手动运行的范围规则。
- 正式评估记录仍为 **178,210** 条，推荐条目仍为 **1,694** 条；未删改不可变评估及推荐快照。
- 未来置顶日期、无法确认的早期日期、2,553 条旧协议缺新字段评估、旧来源表、旧集群及保守来源标记均未自动删除。
- 现行 schema 要求候选关联 material，故没有直接删除旧流程的无正文占位 material 或空标题 material，以免破坏抓取历史与外键。它们的后续生命周期整理需在 000046 兼容切换后另行做精确集合审计。

## 执行及备份证据

先生成完整 pg_dump 自定义格式备份，验证其目录可读，并完整恢复到一次性独立 Postgres 容器。完整备份副本上的修复及验收通过后，才使用同一工具在运行库执行；运行库所有业务修改处于一个事务中，写入前再次校验对象集合、队列暂停、无 running 任务及没有供应商在途批次。

备份与敏感逐行证据已复制并校验至本机私有目录 **`/home/harson/sideProj/DataArk/.dataark-backups.local/20261020/`**，权限目录 0700、敏感文件 0600。该目录由现有 `*.local` 规则忽略，已经 git check-ignore 验证，没有提交仓库；无需依赖执行时的临时目录保留恢复材料。

- `before.dump`：完整清理前备份，976,906,118 字节。
- `production-plan.json`：精确对象集合。
- `production-preimage.json`：相关 material、状态、候选、任务、用户时间、去重成员、队列及运行的修改前记录。
- `production-created-jobs.json`、`production-archive-materials.json`：新增对象 ID 清单。
- `archives.json`：90 个既有文件提取出的来源、标题和正文清单；仅本地保存。
- `production-apply.log`、`production-counts.json`、`production-verify.txt`、`production-integrity.txt`：提交与验收结果。
- `repair.go`、`repair`：实际执行工具及源码；通过容器内 Unix socket 连接数据库，不导出密码或环境变量。

完整备份 SHA-256：`ca1d48ff92af0b93bbb15bd3ccf5567407ed0b4f09ff53552ca3aa0332c9b6d8`。

修复工具 SHA-256：`3ba56eb9e801b32d5ffcb63d29fb252d580abb4bb9f6f9b37baa48173d6b2b98`。

自动审批曾拒绝导出容器环境变量；该操作没有执行。后续全部通过容器内本地连接完成，无凭据导出，也没有因此阻塞修复。

一次性演练容器已停止并删除；运行容器保留。发生回退需求时，优先按逐行修改前记录和新增对象清单有针对性恢复，避免覆盖清理后其他正常写入；全库备份作为灾备恢复点。

## 验收

[只读验收 SQL](2026-10-20-confirmed-data-cleanup-verify.sql) 可重复运行。提交后的结果为：

```text
ready_without_pointer                0
pending_and_eligible                 0
invalid_current_zero_dates           0
exposure_time_inverted               0
review_time_inverted                 0
stuck_fetching_without_job           0
current_score_mismatch               0
current_summary_mismatch             0
live_zero_version_jobs               0
contaminated_material_still_eligible  0
contaminated_candidate_still_ready    0
archive_without_material_body        0
current_version_owner_mismatch       0
current_assessment_owner_mismatch    0
candidate_mapping_owner_mismatch     0
```

运行 API 首页复核 HTTP 200。本次只修复现存数据，没有部署防止再次产生这些异常的代码变更；业务队列仍需用户手动开始。
