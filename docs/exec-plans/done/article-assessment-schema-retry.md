# Retry invalid article assessment JSON with concrete errors

> 归档说明（2026-09-30 核对）：本文保留实施当时的背景、决策、路径与验证记录，部分内容已被后续变更取代。现行行为与操作入口见 [文档索引](../../README.md)。历史测试输出不代表当前部署状态。
>
> 当前重试只在原始消息后追加校验错误，不回放旧模型输出；最多 6 次 schema 调用再尝试 object，见 `api/assessment/openai.go`。

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds.

This document must be maintained in accordance with `PLANS.md` at the repository root.

## Purpose / Big Picture

When the article-assessment model returns JSON that fails local schema checks, the next request currently either gives up or immediately switches to a looser `json_object` mode without telling the model what was wrong. After this change, the provider keeps `json_schema`, appends the previous assistant output plus the concrete parser or validator error, and retries up to five times. Only then does it fall back to `json_object`. Operators can see each attempt in `llm_call` logs. All chat stages also send the configured sampling parameters and a 32,768 completion cap.

## Progress

- [x] (2026-08-17 22:53+08:00) Authored this ExecPlan and locked retry, cache, logging, and sampling decisions.
- [x] (2026-08-17 23:00+08:00) Applied unified chat sampling and max_tokens in `chatJSON`.
- [x] (2026-08-17 23:05+08:00) Implemented schema retries with unwrapped validator errors, then json_object fallback.
- [x] (2026-08-17 23:08+08:00) Logged every attempt with `error_message` and incrementing `llm_attempt`.
- [x] (2026-08-17 23:12+08:00) Updated tests and the operations runbook; `cd api && go test ./recommendation ./observability -count=1` passed.
- [x] (2026-08-17 23:20+08:00) Stopped replaying assistant completions; after a retryable schema failure, later non-retryable errors still take the json_object fallback.

## Surprises & Discoveries

- Observation: the previous concurrent downgrade test depended on holding the capability mutex for the entire first HTTP probe. After the mutex was limited to cache read/write, concurrent first calls could each probe schema. The replacement test probes HTTP 400 once sequentially, caches `json_object`, then runs concurrent calls that must all use `json_object`.
  Evidence: `TestArticleAssessmentUnsupportedSchemaCachesJSONObjectForLaterCalls` in `api/recommendation/openai_provider_test.go`.
- Observation: `encoding/json` unknown-field errors already include the key name, so unwrapping `invalid chat JSON: %w` is enough to tell the model `json: unknown field "unused"` without parsing the completion again.
  Evidence: `TestArticleAssessmentRetriesSchemaWithConcreteValidatorError`.
- Observation: after one retryable schema failure, a later 429 or context-length 400 returned immediately and skipped `json_object`. Replaying each assistant completion also grew the next prompt until a context-limit failure became likely.
  Evidence: `TestArticleAssessmentFallsBackToJSONObjectAfterRetryableThenNonRetryableError` and `TestArticleAssessmentFallsBackToJSONObjectAfterRetryableThenContextLimit`.

## Decision Log

- Decision: Treat one initial schema call plus five error-feedback retries as the maximum strict path, then one `json_object` call.
  Rationale: The operator asked for at most five retries before compatible output. Counting the first request separately avoids silently reducing the retry budget.
  Date/Author: 2026-08-17 / Codex
- Decision: Feed the unwrapped parser or validator root cause to the model, never the internal wrapper `invalid chat json` alone. Missing fields must be named.
  Rationale: `invalid chat json` is a log classification prefix. The model needs the field name, unknown key, or length limit to correct the next JSON object.
  Date/Author: 2026-08-17 / Codex
- Decision: Cache `json_object` only when the HTTP endpoint rejects JSON Schema. Do not cache a per-article invalid-output downgrade. Do not hold the capability mutex across HTTP.
  Rationale: A truncated completion is not proof that the endpoint cannot enforce schema. Holding the mutex across five long completions would stall the assessment queue.
  Date/Author: 2026-08-17 / Codex
- Decision: Apply `temperature=0.7`, `top_p=0.80`, `top_k=20`, `min_p=0.0`, `presence_penalty=1.5`, `repetition_penalty=1.0`, and `max_tokens=32768` to every `chatJSON` call. Leave `LLM_TIMEOUT` unchanged.
  Rationale: The operator asked for these values on all chat stages. Timeout remains an operator flag; Compose already uses 300s.
  Date/Author: 2026-08-17 / Codex
- Decision: Do not replay assistant completions. Retry and json_object fallback messages are the original assessment pair plus one compact validator-error user message. After any retryable schema failure, a later non-retryable error still attempts json_object.
  Rationale: The previous loop returned on the first later 429, timeout, or context-length error, so fallback never ran. Dumping completions into the next prompt made context-limit failures more likely before that fallback.
  Date/Author: 2026-08-17 / Codex

## Outcomes & Retrospective

`AssessArticle` now keeps `json_schema` through one initial call and five error-feedback retries. The retry user message contains the unwrapped parser or validator cause and names missing fields. It does not replay model completions. A sixth schema failure, or a later non-retryable error after a retryable one, is followed by one `json_object` call on the original messages plus that latest validator error and is not cached. HTTP rejection of structured output still caches `json_object` for that endpoint and model. Failed `llm_call` events include compact `error_message` and incrementing `llm_attempt`. Every chat stage sends the requested sampling parameters and `max_tokens` 32768. Focused tests passed. Remaining operator work is to restart the API so the new retry path and sampling fields take effect; `LLM_TIMEOUT` is unchanged.

## Context and Orientation

`api/recommendation/openai_provider.go` is the OpenAI-compatible HTTP adapter. `AssessArticle` currently probes `response_format.type=json_schema` once, then immediately retries `json_object` when the error looks like invalid JSON, an empty completion, or an HTTP rejection of structured output. That probe result is cached per base URL and chat model in `api/recommendation/assessment_output_capability.go` for the process lifetime. `chatJSON` currently sends `temperature` 0.1 for assessment and 0.2 otherwise, with `max_tokens` 2048 (4096 for digest summaries). Failed `llm_call` events record `error_type` but not `error_message`. Local validation lives in `articleAssessmentOutput.result` and `validateArticleAssessmentResult`. The operations runbook is `docs/operations/article-assessment-v3-runbook.md`.

JSON Schema here means the OpenAI-compatible `response_format` object that names required keys and forbids extras. JSON Object means `{ "type": "json_object" }`, which only requires some JSON object; DataArk still validates the same fields locally.

## Plan of Work

First, raise `llmChatMaxTokens` to 32768, delete the digest-only token constant, and make `chatJSON` always send the sampling fields. Remove per-call temperature and max-token overrides.

Second, change `chatJSON` to return the assistant content even when decoding fails, so retries can replay it. Split capability checks into output-retryable versus HTTP-unsupported-schema. Rewrite `AssessArticle` so schema mode retries with a user message that contains `errors.Unwrap` of the validator error plus a fixed contract reminder. Name missing fields. After six schema failures, call `json_object` with that same feedback. After an HTTP schema rejection, call `json_object` with the original messages and cache that mode.

Third, write `error_message` on failed `llm_call` events through the existing compact sanitizer. Schema attempts number 1 through 6; the later object fallback is attempt 7.

Fourth, update `api/recommendation/openai_provider_test.go` and the runbook, then run `go test ./recommendation ./observability -count=1` from `api/`.

## Concrete Steps

From the repository root, create this file if missing, then edit the Go sources named above. After the code compiles, run:

    cd api && go test ./recommendation ./observability -count=1

Expect all tests to pass, including new cases that prove a second schema request contains `unknown field "unused"` or a named missing field, and that six invalid schema completions are followed by one `json_object` request.

## Validation and Acceptance

A first invalid assessment completion produces a second HTTP chat request whose last user message contains the concrete validator text and the required key list, not only `invalid chat json`. Six invalid schema completions produce six failed `llm_call` events with `llm_attempt` 1 through 6, then a seventh `json_object` attempt. A 429 response still produces one request. After an HTTP 400 that mentions `json_schema`, later calls for that endpoint and model use `json_object` without re-probing. Chat payloads for assessment, rerank, digest, and enrichment include the new sampling fields and `max_tokens` 32768. Logs still omit prompt and completion sentinels.

## Idempotence and Recovery

The change is code and tests only. Restarting the process clears the in-memory capability cache and restores schema probing. No migration is required. A failed assessment still falls through to the existing deterministic rule row in discovery.

## Artifacts and Notes

Retry user message shape:

    Your previous JSON did not satisfy the required schema.
    Parser/validator error: json: unknown field "unused"
    Return only one JSON object with exactly these keys: qualityScore, depthScore, evergreenScore, reasons, summary, keywords.
    qualityScore/depthScore/evergreenScore are integers 0-100. reasons has exactly 2 strings of 1-120 characters. summary is 1-200 characters. keywords has 3-8 strings of 1-20 characters each. No extra keys, no markdown.

## Interfaces and Dependencies

In `api/recommendation/openai_provider.go`, `chatJSON` returns `(content string, err error)`. `AssessArticle` remains `AssessArticle(ctx, ArticleAssessmentInput) (ArticleAssessmentResult, error)`. New unexported helpers: `appendArticleAssessmentRetryFeedback`, `articleAssessmentRetryUserMessage`, `articleAssessmentRetryCause`, and `articleAssessmentMissingFields`. In `api/recommendation/assessment_output_capability.go`, keep the per-endpoint cache but expose short locked `currentMode` and `rememberMode` accessors plus `isArticleAssessmentOutputRetryable` and `isArticleAssessmentUnsupportedSchema`.

Revision 2026-08-17 23:12+08:00: marked Progress complete, recorded the mutex/cache test rewrite and json unknown-field unwrap, and filled Outcomes after `go test ./recommendation ./observability -count=1` passed.
Revision 2026-08-17 23:20+08:00: stopped assistant replay, and json_object fallback now still runs after a retryable schema failure followed by a later non-retryable error.
