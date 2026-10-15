# Batch 模型输出类型错误调查（2026-10-14）

当前大量 `model output is invalid JSON or was truncated` 的主要原因是模型输出字段类型与本地评估结构不符。抽查的 JSONL 和模型输出 JSON 语法均有效，且没有输出截断。当前错误文案将多类错误合并，丢弃了实际解码错误。

## 生产记录与供应商结果交叉验证

调查时运行 #2 使用 `qwen3.7-flash`，仍处于 `preparing`。已导入 5,900 篇，其中成功 1 篇、失败 5,899 篇；失败中 5,898 篇使用上述解析错误文案，另 1 篇是业务字段校验失败。另有 1,000 篇在途。这是调查时的快照，后台仍可能推进。

只读查询生产记录，并对供应商已完成的四个批次输出进行内存分析。未提交推理、未修改生产数据或运行状态。仅保存格式和类型统计，未落盘完整模型输出、文章正文、密钥或任务标识。

| 批次 | 结果数 | HTTP 200 / 正常 stop / 内层有效 JSON | reasons 返回字符串 | keywords 返回字符串 | 本地解析失败 | 本地成功 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| #39 | 100 | 100 | 100 | 92 | 100 | 0 |
| #40 | 100 | 100 | 100 | 91 | 100 | 0 |
| #94 | 100 | 100 | 99 | 89 | 99 | 1 |
| #97 | 100 | 100 | 100 | 94 | 100 | 0 |

400 条外层 JSONL 均正常解析，400 条内层模型内容均为有效 JSON，全部 `finish_reason=stop`。399 条 `reasons` 为字符串，366 条 `keywords` 为字符串。另有少量额外字段、缺失字段。批次 #94 唯一的 `reasons` 数组结果对应该批次唯一的本地成功记录。

全部本地失败记录的生成 token 为 34–970，低于请求的 `max_tokens=4096`；四批样本的正常结束原因进一步排除了这些样本因输出限额截断而失败。没有下载并逐条验证全部 5,900 条，因此不把抽样结论当作全量输出证明。

## 错误产生位置

1. `api/assessment/batch_import.go` 的 `importBatchStream` 先解析 JSONL 外层，再解码 `response.body.choices[0].message.content`。外层解析失败会返回 `invalid Batch result JSONL`，与本次文案不同。
2. `api/assessment/openai.go` 的 `articleAssessmentOutput` 将 `reasons` 和 `keywords` 都定义为 `*[]string`，而 `summary` 为 `*string`、三个评分为 `*int`。
3. `api/llm/client.go` 的 `DecodeChatJSON` 使用严格 Go JSON 解码，拒绝类型不符、未知字段和尾随 JSON。
4. 导入代码把 `finish_reason=length` 或任意严格解码错误统一写成 `model output is invalid JSON or was truncated`，未保留解码器原始原因。因此有效 JSON 的类型错误也会显示为“invalid JSON”。

合成示例（不是生产正文）：

```json
{"reasons":"论据充分；范围有限","keywords":"队列、调度、评估"}
```

这些字段需要的类型是：

```json
{"reasons":["论据充分","范围有限"],"keywords":["队列","调度","评估"]}
```

## 为什么大量返回了字符串

`api/assessment/batch_service.go` 的 `itemRequest` 固定使用 `response_format={"type":"json_object"}`，未提交输出 JSON Schema。当前提示词描述“两条理由”和“3 到 8 个关键词”，但没有明确声明它们必须为 JSON 字符串数组，也没有给出完整 JSON 输出示例。

[百炼结构化输出文档](https://help.aliyun.com/zh/model-studio/qwen-structured-output)明确区分：JSON Object 模式保证 JSON 格式，但不保证固定结构、键名和字段类型；JSON Schema 模式用于控制结构和类型。当前请求约束与本地严格解码要求存在缺口，供应商成功完成请求也不表示输出能通过本地校验。

类型不匹配已经由真实输出统计证明；提示词未显式说明数组是代码中的缺口，不能单凭此次只读调查证明某一次生成选择字符串的内部原因。

## 本地复现与后续修复方向

通过临时 Go overlay 向现有 assessment 测试包增加调查测试，未改动实现。合成一份完整有效 JSON，仅将 `reasons` 或 `keywords` 改为字符串，现有 `llm.DecodeChatJSON` 返回：

```text
json: cannot unmarshal string into Go struct field articleAssessmentOutput.reasons of type []string
json: cannot unmarshal string into Go struct field articleAssessmentOutput.keywords of type []string
```

恢复数组后，严格解码和评估业务校验均通过。测试命令：

```sh
cd api
go test -overlay=/tmp/dataark-batch-output-investigation/overlay.json ./assessment \
  -run '^TestInvestigateValidJSONWithWrongAssessmentTypes$' -v -count=1
```

建议修复：明确提示词中的数组类型并提供完整 JSON 示例；验证当前模型和 Batch 接口的 JSON Schema 支持后使用结构约束；区分截断、语法错误、类型错误、未知字段及业务校验错误，并保存脱敏原因。历史失败需评估能否从现有结果安全恢复，不能直接改为成功或盲目重新付费推理。

本次仅调查，没有部署代码修复、暂停运行或触发失败重评。自动审批曾拒绝完整生产输出落盘方案，理由是可能导出敏感内容；后续采用获批的仅元数据统计方案完成调查。
