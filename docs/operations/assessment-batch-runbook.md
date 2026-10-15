# 百炼 Batch 文章评估

Batch 只替换文章 LLM 评估的执行方式。推荐、重排、日报摘要继续使用原有实时 LLM 配置。生产数据库通过 Goose `000041` 增加运行、批次、文章项及调用日志关联；升级前备份 PostgreSQL。

## 配置与启用

在 `docker/.env` 中配置以下字段，再重建 API 容器。仓库中的 `.env.example` 提供同样的模板。二进制不会自行读取环境变量，Compose 将它们传入同名的小写连字符 CLI 参数，例如 `LLM_BATCH_PROVIDER` 对应 `-llm-batch-provider`。

```dotenv
LLM_BATCH_PROVIDER=bailian
LLM_BATCH_BASE_URL=https://dashscope.aliyuncs.com/compatible-mode/v1
LLM_BATCH_API_KEY=your-bailian-api-key
LLM_BATCH_MODEL=qwen-plus
LLM_BATCH_SIZE=100
LLM_BATCH_CONCURRENCY=10
LLM_BATCH_POLL_INTERVAL=90s
LLM_BATCH_REQUEST_TIMEOUT=60s
```

`LLM_BATCH_PROVIDER=bailian` 即启用，必须同时提供密钥和模型。供应商为空时使用实时评估；暂不支持其他供应商。地址、密钥及模型独立于 `LLM_BASE_URL`、`LLM_API_KEY` 和 `LLM_CHAT_MODEL`，原有必填配置仍需保留。地址与密钥需属于同一百炼地域；模型需支持该地域的 Batch File 和 JSON Schema。

`SIZE` 是每批最多文章数（1–50,000）；输入另受每行 1 MiB、每文件 50 MiB 限制。`CONCURRENCY` 是同时在途的批次数（1–100），不是实时模型并发。默认每 90 秒检查远端，HTTP 请求默认超时 60 秒；控制请求失败按指数退避，最多 15 分钟。

请求使用 OpenAI 兼容的 `/files` 与 `/batches`，每行目标 `/v1/chat/completions`，完成窗口 24 小时。复用当前 `article-value-v4` 的标题、正文证据裁剪和结果验证。新构建批次在 JSONL 每行的 `body.response_format` 提交 `json_schema`、`strict=true` 和完整输出约束；Qwen 禁用思考。依照百炼结构化输出建议，不设置 `max_tokens`。不会进行格式修复调用或切换到实时接口。

Schema 要求六个字段全部存在且禁止额外字段：三个评分为 0–100 整数；`reasons` 为恰好两条、每条 1–120 字符的字符串数组；`summary` 为 1–200 字符字符串；`keywords` 为 3–8 条、每条 1–20 字符的字符串数组。模型和当前地域需支持 JSON Schema，当前配置的 `qwen3.7-flash` 在百炼支持列表中。Batch 提交不会自动降级为 JSON Object。

迁移 `000044` 增加批次 `response_mode`，旧批次默认记录为 `json_object` 并保留原始请求；新批次记录 `json_schema`。在已授权运行中继续组批也会使用新 Schema；已上传或已提交的旧批次继续回收，兼容解析仍可读取原输出。调用日志使用批次持久化的实际请求模式，历史重导入保留原调用模式。

## 手动启动与结果

爬取完成后，文章进入暂停的评估队列。管理员在推荐中心的「评估」页点击「执行 LLM 评估」。若全库查重尚未生效，先完成现有查重流程，再登记一轮 Batch 授权。本轮固定为登记时已到执行时间的队列前缀，后续新增文章等待下一次点击。

登记后后台边移交任务边提交批次：有效待提交文章达到 `LLM_BATCH_SIZE` 后立即构建、上传并提交，无需等整个队列前缀移交完成；前缀取完时提交最后不足一批的文章。达到 `LLM_BATCH_CONCURRENCY` 上限时暂停移交，剩余任务留在 River，直到批次结束释放空位。Batch 文章项和原 River 任务取消在同一事务提交，文章版本具有唯一在途占用。这里的原任务取消代表移交，面板从实际导入结果统计成功和失败。重启会继续已授权的准备、上传、轮询和导入，不会自动登记下一轮。

面板显示本轮待提交、处理中、成功、失败、复用和跳过数量，以及批次远端 ID、供应商状态、提交时间、截止时间和失败项。供应商成功数表示远端请求完成数，输出仍须通过本地校验才算本轮成功。Batch 不提供逐篇推理耗时或 decode token/s；面板显示 `—`，保留供应商返回的 token 用量。

成功项逐条、幂等地写入评估和调用日志。返回时正文版本、查重代表或黑名单已变化的文章记为跳过，不激活旧结果。输出无效、远端请求错误、过期或缺失结果的文章记为失败；可用的同正文历史模型评估保留，否则文章进入 review。其余成功项不受影响。

失败项不会自动重评。修正原因后，勾选「重试失败项」，先预览，再「执行回填」，最后再次点击「执行 LLM 评估」。复用匹配正文、模型、供应商身份及策略的成功评估，无需再次提交。

## 重启、停用与不确定状态

有未结束运行时，必须保留原 `LLM_BATCH_BASE_URL` 和可访问该账户任务的 `LLM_BATCH_API_KEY`。缺少恢复密钥或改变地址会拒绝启动。清空 `LLM_BATCH_PROVIDER` 可将新评估恢复为实时模式，旧 Batch 仍在后台继续回收，旧任务占用的正文版本不会重复入队。不要在在途任务期间更换百炼账户。

上传或创建批次时若连接中断，可能已被供应商接受。系统先持久化“不确定”状态，再通过应用生成的唯一文件名、输入文件 ID 和 `ds_name` 查找对应资源。未找到时保留待核对状态并退避查询，**不盲目重新上传或创建**。连续 8 次无法确认结果或出现其他非永久请求错误时，暂停本轮并要求人工核对；查询返回永久 4xx（408 除外）连续 3 次也会暂停。401/429 立即暂停，无需等待次数上限。面板没有强制重交或取消按钮；需在百炼控制台核对账户、地域及远端记录后点击「继续本轮评估」，继续查询原记录。

百炼批次查询仅支持创建后 30 天内的任务。超过窗口且返回 404 时，结束本地批次并释放未完成文章的占用；已导入成功结果保持不变，缺失结果记为失败，需要人工回填。窗口内的 404 使用上述次数上限暂停，保留远端 ID 和在途占用，不会重新提交。

已结束批次的应用专属输入、输出和错误文件保留七天，再后台删除；本地评估、记录和用量保留。全部运行导入完成后可移除恢复凭据，清理将暂停；恢复原地址和密钥后继续清理。百炼文件没有自动过期机制，长期停用时可自行在控制台核对并清理这些 `dataark-` 文件。

停止 API 会暂停本地推进，远端批次仍可能继续执行。重新启动会继续恢复。因此现有实时评估的“停止后重新启动保持队列暂停”操作不适用于已授权 Batch。Goose Down 在仍有活动 Batch 运行时拒绝执行；完成导入后再安排回滚。

## 查询与验证

每次移交最多接收 250 个队列任务，凑齐一批后优先推进上传和提交。`preparing` 表示本轮队列前缀尚未取完，并不表示远端推理尚未开始；该阶段也可以已有在途批次和已导入的结果。原有未结束的 preparing 运行也会直接使用已接收的文章组成批次，不会重新扫描已移交的任务。可连续查询下面的运行游标：`cursor` 增加表示继续接收任务，并发满时游标会暂停推进，待批次结束后继续。`high_water` 是本轮队列上界（中间可能有其他类型的任务，不能用 ID 差值当文章数量）。

```sql
SELECT id, status, cursor, high_water, lease_until, created_at
FROM assessment_batch_runs ORDER BY id DESC LIMIT 3;

SELECT run_id, status, count(*)
FROM assessment_batch_items GROUP BY run_id, status ORDER BY run_id DESC, status;
```

旧任务的 `content_version=0` 表示没有可用正文版本，缺少候选版本映射或素材版本的任务也无法提交。这些任务移交后记为 `skipped`，保留原队列 job ID、候选 ID 和原因，不触发推理。可直接查询准备阶段跳过明细：

```sql
SELECT job_id, candidate_id, material_id, content_version, error
FROM assessment_batch_items
WHERE run_id = 1 AND status = 'skipped'
ORDER BY id LIMIT 50;
```

准备阶段跳过不会把文章标记为评估失败。需要重新评估时，应先确认文章当前正文、版本和查重代表状态，再通过现有回填流程登记有效版本；不要把旧任务的版本号直接改成最新版本。升级前已被取消而没有 Batch 明细的任务不会自动补出明细。

以下端点仅 owner 可访问，沿用现有认证及响应封装，不返回密钥、prompt、正文或原始 JSONL。

```text
GET /api/admin/assessment/queue
GET /api/admin/assessment/metrics
GET /api/admin/assessment/batches?limit=20&beforeId=0
GET /api/admin/assessment/batches/1?limit=50&afterId=0
POST /api/admin/assessment/batches/cleanup/resume
```

列表采用批次 ID 向前分页；详情只包含失败文章项，使用 `nextId` 继续向后分页。队列原有快照增加 `executionMode` 和 `batchRun`；查重延迟响应仍为 `{snapshot,dedupeRun}`。

常规测试使用 HTTP 模拟供应商，不发送百炼请求。PostgreSQL 事务、River 移交和恢复测试需指向可清空的独立验证数据库，数据库名必须以 `dataark_v3_verify` 开头：

```sh
cd api
DATAARK_BATCH_POSTGRES_TEST_DSN='host=localhost user=test password=test dbname=dataark_v3_verify_batch sslmode=disable' \
  go test ./assessment -run '^TestPostgresBatchTransferAndRecovery$' -count=1
```

可选真实运输验证使用百炼官方 `batch-test-model`、`/v1/chat/ds-test`，不验证文章评估质量。显式提供 `DATAARK_BAILIAN_TEST_API_KEY` 后运行 `go test ./assessment -run '^TestBailianBatchLiveTransport$' -count=1`；地域地址通过 `DATAARK_BAILIAN_TEST_BASE_URL` 指定。未配置密钥时该测试跳过。测试超时或提交响应丢失时，应先核对远端任务再重试。

接口依据：[百炼 Batch File](https://help.aliyun.com/zh/model-studio/batch-interfaces-compatible-with-openai)、[文件管理](https://help.aliyun.com/zh/model-studio/openai-file-interface)、[结构化输出](https://help.aliyun.com/zh/model-studio/qwen-structured-output)。供应商支持模型和地域以官方当前列表为准。

## 容量和错误暂停

默认 `LLM_BATCH_SIZE=100`、`LLM_BATCH_CONCURRENCY=10`。未结束批次从本地准备到结果导入合计最多 10 个；组批缓冲按缺额取出，剩余文章保留在 River，最后不足 100 篇的尾批照常提交。

上传、文件查询、批次提交/查询、结果下载返回 HTTP 401 或 429 时，整轮进入持久 `paused` 状态。批次验证的 `errors.data`、JSONL 顶层 `error` 和 `response.body.error` 中的 `InvalidApiKey`、`invalid_api_key`、`Throttling.*` 等认证/限流错误码也会触发同样的暂停。停止出队及供应商请求，不自动重试；重启与查重恢复均不会解除暂停。管理页面持续显示状态码、脱敏原因及处理提示，处理密钥/权限或额度/限流后点击「继续本轮评估」，沿用原运行和批次。输出 JSONL 中的单篇 401/429 记录为失败并暂停后续导入；恢复后复用已导入行，该失败项需要后续手动回填。已提交的供应商任务可能继续执行，本地暂停不会取消它们。

供应商批次验证错误的 `code`、`message` 和 `line` 会保留在批次及缺失结果的失败文章记录中，完成导入后仍可查询；不保存或展示原始响应正文。密钥和错误消息中的 URL 会脱敏。

删除七天前的文件返回 401/429 时，文件清理暂停标记也会持久化，页面显示独立的文件清理提醒，禁止启动新评估；存在已授权运行时，也暂停其出队与供应商请求。重启不会自动重试删除。修复凭据/额度后，由 owner 点击「重试文件清理」，或调用上述 `cleanup/resume` 接口。此操作不创建新评估运行；若已有授权运行，清理暂停解除后可继续推进。已结束或已退回 River 的运行保持原状态，批次原有推理失败原因不会被清理失败覆盖。

## 把未远端提交的整轮退回 River

这与文章评估结果的回滚接口不同。先停止 API 和 Worker，并备份 River、Batch 和文章状态表。构建专用工具：

```sh
cd api
CGO_ENABLED=0 go build -o /tmp/assessment-batch-reset ./cmd/assessment-batch-reset
# DATAARK_RESET_DSN 通过受保护的环境配置提供，不写入命令历史或日志。
/tmp/assessment-batch-reset -run-id=1 -migrate
/tmp/assessment-batch-reset -run-id=1 -apply
```

默认仅预检；`-migrate` 明确执行编号迁移。工具不启动 API、队列 Worker 或供应商客户端。远端已提交、上传/提交结果不明或有正在执行的评估 Worker 时拒绝回退。

回退在同一事务内恢复保留的原 River 作业，或从 Batch 审计重建被 River 清理的作业；相同正文版本已有等待作业时复用。包括本轮失败和跳过项，保留零版本的真实含义。保存原任务到恢复任务的映射及退回时间，释放正文占用，关闭旧运行。历史失败/跳过记录和 `returned` 汇总保留在数据库；已回退运行从面板移除。重启仍等待手动启动。重复回退已结束运行不会新增任务。

今后移交会在 River metadata 写入 `batch_run_id`，使没有文章项的重复移交也可准确追溯；旧版本没有审计且原任务已清理的作业无法凭空恢复，不应按整个 cancelled 队列盲目重建。

## Batch 面板日志

已退回 River 的 `rolled_back` 运行及其旧批次不再出现在 Batch 面板、队列运行摘要和失败详情接口中；数据库保留原任务到恢复任务的映射及失败/跳过审计。正常结束和仍在进行的批次继续可查，供应商 `expired` 状态需要回收，不能按状态直接清除。

点击「查看失败项」打开失败详情弹窗，逐页加载文章版本和错误原因。详情加载失败时在弹窗内重试；关闭弹窗、切换批次或离开评估页会丢弃过时请求的响应，详细错误不在主面板展开。

## Batch 输出兼容与历史结果重导入

旧 Batch 的 `json_object` 模式不保证字段类型。新批次提交严格 JSON Schema，导入仍兼容字符串形式的 `reasons` 和 `keywords`，以及字符串中编码的 JSON 数组；额外元数据字段会忽略，六个评估字段仍必须存在。关键词字符串按中英文逗号、顿号、分号、换行分隔，保留关键词内部空格。

理由字符串仅在换行或分号明确形成两条且符合每条 120 字符限制时拆分，否则保留完整段落作为一个数组项，最多 240 字符，不补写第二条理由。模型直接返回的理由数组仍要求两条、每条最多 120 字符。评分仍要求 0–100 整数，摘要和关键词数量/长度限制保持原有校验。实时评估的严格解码不变。

`finish_reason=length` 单独显示截断错误；真正无效 JSON、字段类型不符、缺少必填字段和业务约束错误分别显示具体原因。外层 JSONL 无效仍是结果文件错误，不能用兼容解析绕过。

对已结束批次，可用独立工具重新读取原供应商输出文件并恢复原先因模型输出解析/校验而失败的文章，不再提交推理：

```sh
cd api
CGO_ENABLED=0 go build -o /tmp/assessment-batch-reimport ./cmd/assessment-batch-reimport
# 在受保护环境中提供 DATAARK_REIMPORT_DSN、DATAARK_REIMPORT_BASE_URL、DATAARK_REIMPORT_API_KEY。
# 必须使用原运行的地址与账户；默认只预览，不写入数据。
/tmp/assessment-batch-reimport -run-id=2
/tmp/assessment-batch-reimport -run-id=2 -apply
```

工具也支持 `-config-stdin`，从 stdin 接收私有 JSON 配置 `dsn`、`baseUrl`、`apiKey` 和可选的 `qualityThreshold`，不把配置写入命令参数或日志。未指定质量阈值时沿用应用默认值。工具不启动 API/Worker，不执行迁移，不上传文件、不创建 Batch、不回填 River。输出仅含统计和脱敏失败原因。

恢复会在同一事务内锁定 Batch 项和文章，重新检查当前正文版本、查重代表及黑名单，跳过已有有效评估、新 Batch 占用或正在执行的 River 评估。尚未执行的同版本 River 任务不阻止恢复，之后正常组批时会复用已恢复的评估。已退回 River、暂停或有文件清理暂停的运行不能启动重导入；下载返回 401/429 立即停止，应用模式下还会暂停仍在活动的本轮。

恢复成功后更新文章当前评估、摘要、关键词和 Batch 成功统计；原 LLM 调用保留 ID、时间和 token 用量，只修正成功/失败状态，重复执行不会增加调用记录或再次付费推理。无法通过校验的文章保持失败，应用时更新具体失败原因。

## 变更记录

一次性部署与恢复事件的历史记录，写完即冻结；现行操作方式以上文章节为准。

### 2026-10-13 容量调整与运行 1 回退

实例配置及代码默认值改为每批最多 100 篇、最多 10 个未结束批次；HTTP 401/429 暂停整轮评估（见「容量和错误暂停」）。

2026-10-13 18:37（北京时间）用 `assessment-batch-reset` 完成运行 1 的事务回退。API/Worker 全程停止，没有调用真实供应商：

- 回退前：River 有 123,665 个 available 评估任务；运行 1 有 55,037 个审计项，其中失败 18,936、待组批 29,178、跳过 6,923。38 个历史批次均因 HTTP 401 失败，远端 ID 和提交时间均为空，没有远端已提交或结果不明批次。
- 按 55,037 项审计重建 7,085 个 River 任务；另外 47,952 个同版本任务已在 River 等待，直接复用。回退后 River 共 **130,750** 个评估任务全部 available，评估队列仍暂停；全部审计项有恢复任务映射，正文占用、未结束批次和待执行 Batch tick 均为 0。旧运行标记 `rolled_back`，保留失败/跳过审计，`pending_assessment` 为 false。
- 已生成并验证新镜像，仅重建生产 API 容器（created，未启动），容器参数为批大小 100、并发 10。
- 验证：后端完整 `go test ./...` 通过；真实 Postgres/River 集成测试覆盖迁移、限额移交、尾批、重启、原任务恢复/清理后重建、同版本复用、事务失败回滚和重复回退；独立 Compose 使用模拟供应商，Chrome DevTools 验证 401/429 持续提示、手动继续、重启不新增供应商请求。
- 受影响表备份、回退输出、旧镜像 ID、配置备份及验证截图保存在 `/tmp/dataark-batch-reset/`，权限 0600，不纳入版本控制。

### 2026-10-14 严格 JSON Schema 上线

迁移 `000044` 上线，部署镜像 `dataark-batch-schema:verified`（ID `sha256:dd5a322779c1bf940ac846f1106f384e3923c8b70ec69bdb2333df3bc71fcaa2`），迁移版本 44，公开页面 HTTP 200。

- 验证：`go test ./...` 通过；HTTP 测试覆盖真实 JSONL 构建、multipart 上传和 `/batches` 创建及严格 Schema 位置；独立 Postgres 测试覆盖 43→44 迁移、旧输入逐字节不变、默认旧模式、活动运行回滚保护；完整 Postgres 启动迁移、River 重启与 pgvector 集成测试通过。
- 部署后核对：运行 #2 仍使用 `qwen3.7-flash`，69 个结束批次和 10 个已提交旧批次均标记 `json_object`，调用模式与批次模式不一致记录为 0。部署时未构建新的 Schema 批次，测试结果不代表真实供应商已接受 Schema；现有授权运行后续组批自动使用新请求格式，旧远端任务、已存评估和历史失败文章未重新提交。

### 2026-10-14 输出兼容解码与运行 2 恢复

部署 Batch 兼容解码器，镜像 `dataark-batch-compat:verified`（ID `sha256:67aefd879b66d2b05ae812bb805fb6151ef3c8486762798d5d7601375b24779d`），并用 `assessment-batch-reimport` 读取运行 #2 现有输出文件恢复历史失败项，未上传文件、创建 Batch 或重新推理：

- 初次最终预览：59 个已结束批次，5,899 个失败项，5,762 个可恢复，137 个仍未通过校验。应用时后台又完成 3 个批次：共处理 62 个批次、6,199 个失败项，恢复 **6,056** 项；**143** 项保留失败并显示具体原因——112 个关键词超过 20 字符、25 个缺少必填字段、2 个关键词数量不符、1 个评分字段类型不符、3 个真正无效的模型 JSON。
- 升级后的正常回收另导入 300 个结果，成功 294、失败 6。最后只读一致快照：本轮成功 6,351 项、失败 149 项、处理中 1,000 项，本轮仍在执行。
- 一致性核对：成功项评估指针缺失或版本不符 **0**，Batch 状态与调用结果不符 **0**，重复调用关联 **0**；文章项与调用记录的 token 总量均为 **10,028,186**。
- 验证：后端 `go test ./...` 通过；真实独立 Postgres/River 集成测试覆盖恢复事务、等待任务可复用、正在执行的任务不能被覆盖、调用记录幂等性；新增解码测试覆盖原生数组、字符串段落、分号/换行列表、字符串编码数组、英文关键词内部空格、额外字段及无效/截断/缺失字段/超限输出。
- 原始结果仅在内存读取，未导出到磁盘，也未记录密钥或正文。
