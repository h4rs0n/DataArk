# Material 内容模型升级

适用范围：Goose `000032`–`000035`。这些迁移把正文、评估状态和来源关系移出 `discovery_candidates`，并将用户状态、评估、推荐和归档关联到 `material`。

## 升级

1. 停止旧 API 与 worker，避免旧代码在新 schema 上写入。
2. 使用现有备份流程备份 PostgreSQL 和归档目录，并记录旧二进制/镜像版本。先在备份副本演练；数据迁移和身份合并期间保持维护窗口。
3. 启动新版本。Goose 顺序执行结构迁移和数据复制；随后启动代码归并强身份并回填本地归档文本。
4. 确认下列检查通过后恢复使用。评估队列继续默认暂停，仍需 owner 手动开启。

后续 Goose `000036` 删除人工标注工作流的五张表及全部数据，正文、正式评估和推荐快照继续保留。升级至该版本前须停止旧 API 并备份数据库；迁移拒绝 Down，恢复需要备份及匹配的旧版本。详见[文章评估手册](article-assessment-runbook.md#human-workflow-removal-in-goose-000036)。

## 检查

```sql
SELECT version_id FROM goose_db_version WHERE is_applied ORDER BY id DESC LIMIT 1;
-- 应为 35 或更新的迁移版本。
SELECT * FROM material_migration_checkpoints ORDER BY name;
-- legacy-storage-copied、strong-identities、archive-content、v3-compatibility。
SELECT COUNT(*) FROM discovery_candidates WHERE material_id IS NULL;
SELECT COUNT(*) FROM archive_documents WHERE material_id IS NULL;
-- 均为 0。
SELECT * FROM material_ingestion_issues ORDER BY archive_document_id;
SELECT COUNT(*) FROM material_provenances WHERE migration_uncertain;
SELECT material_id, COUNT(DISTINCT domain_key) AS independent_sources
FROM material_provenances WHERE domain_key <> '' GROUP BY material_id;
```

迁移保留旧抽取记录 ID、推荐条目 ID 和展示快照。强身份合并保留来源证据；旧 material ID 可通过 `material_redirects` 解析。同日历史重复推荐标记 `legacy_duplicate`，不会被删除。

`migration_uncertain` 表示旧去重逻辑曾转移来源、且原 URL 无法唯一还原目标的历史证据，需按原 URL 复核。独立来源计数按发现站点域名去重，不按 endpoint 数或文章目标域名计算。

## 归档异常与回滚

缺失或损坏文件不会阻止元数据迁移，问题记录在 `material_ingestion_issues`。恢复文件后运行现有归档重建流程重新抽取。重建索引不会自动清理该问题表；要重新核查文件并清除已修复的问题记录，在维护窗口仅删除 `archive-content` 的 checkpoint 后重启，启动回填会检查全部归档文档，再次核查问题表。不得清空其他 checkpoint 来反复执行生产迁移。

Down 会主动报错，不能无损恢复拆表前的写入关系。回滚必须停止新版本，恢复升级前 PostgreSQL/归档备份，再启动匹配的旧版本。

## 开发验证

`cd api && go test ./...` 覆盖 SQLite 回归。生产验证必须将 `DATAARK_POSTGRES_TEST_DSN` 指向空的、名称以 `dataark_v3_verify` 开头的可丢弃数据库，再运行：

```sh
cd api
go test ./bootstrap -run TestPostgresV3MigrationsRiverRestartAndPGVector -count=1
```

测试覆盖历史内容搬迁、强身份合并、版本/评估保留、推荐快照、用户状态汇总、来源删除后的独立计数，以及 River 重启和 pgvector；同时验证 `000036` 删除包含旧记录的工作流表、重复启动和拒绝自动回滚。
