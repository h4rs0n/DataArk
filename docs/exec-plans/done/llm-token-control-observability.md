# Bound and observe LLM token use

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds. Maintain this document in accordance with `PLANS.md` at the repository root.

## Purpose / Big Picture

Article processing currently sends every extracted article to an OpenAI-compatible model without explicitly disabling model reasoning, without bounding completion length, and without retaining the provider's token usage. After this change, article assessment and recommendation reranking explicitly run without reasoning and cap output at 1024 tokens. Every chat call emits one durable structured log record with its stage, model, prompt/completion/reasoning/cache token counts, duration, outcome, and safe request identity. Article assessment uses a dedicated strict JSON Schema containing only the quality score, depth score, and short reasons that DataArk consumes.

## Progress

- [x] (2026-08-10 10:56+08:00) Profiled the production database and current provider path; confirmed article assessment dominates calls and the client discards response usage.
- [x] (2026-08-10 11:00+08:00) Inspected the provider, article assessor, structured logging boundary, tests, and current runtime logging retention.
- [x] (2026-08-10 11:03+08:00) Implemented bounded non-reasoning assessment and rerank requests plus the dedicated strict assessment schema and local validation.
- [x] (2026-08-10 11:04+08:00) Implemented one safe structured usage event for every chat request, including failures and invalid model output, with coverage for payload non-disclosure.
- [x] (2026-08-10 11:06+08:00) Passed focused tests, the full backend suite, race tests, and whitespace review; prepared only task files for the focused commit.

## Surprises & Discoveries

- Observation: `OpenAICompatibleProvider.chatJSON` only decodes `choices[].message.content`; TokenHub's `usage` object and `reasoning_content` are silently discarded.
  Evidence: `api/recommendation/openai_provider.go` and the production profile of 9,133 assessment attempts.
- Observation: the current article assessor reuses metadata enrichment and asks for nine fields, but it consumes only `qualityScore` and `depthScore`.
  Evidence: `api/recommendation/article_assessor.go`.
- Observation: application logs are already persisted by a retained daily writer, so the required durable operational record does not need a database migration.
  Evidence: `api/logging/runtime.go`.
- Observation: a strict provider-side JSON Schema is not sufficient by itself because an OpenAI-compatible gateway can ignore or partially implement it.
  Evidence: the assessment path now also rejects unknown fields, trailing JSON, out-of-range scores, excess reasons, and reasons longer than 160 characters locally.

## Decision Log

- Decision: Treat “prompt, completion, reasoning, cached token” as provider-reported token counts, never prompt or completion text.
  Rationale: Article bodies and model output can contain private or hostile content; existing observability policy forbids payload logging, while the requested profiling data is represented by TokenHub's usage counters.
  Date/Author: 2026-08-10 / Codex
- Decision: Extend the fixed `dataark_event` schema and rely on the existing daily retained log writer rather than add a database table.
  Rationale: The user explicitly requested records in logs, and process logs are already persisted and retained.
  Date/Author: 2026-08-10 / Codex
- Decision: Use model-aware reasoning switches: Qwen receives `enable_thinking:false`; DeepSeek, MiMo, and other TokenHub thinking models receive `thinking:{type:"disabled"}`.
  Rationale: Qwen documents a distinct top-level switch, while DeepSeek and MiMo use the `thinking` object. Sending every vendor-specific field to every OpenAI-compatible model risks request rejection.
  Date/Author: 2026-08-10 / Codex
- Decision: Apply the 1024-token ceiling and model-aware thinking switch in the shared chat helper, not only at two call sites.
  Rationale: this guarantees consistent controls for assessment, rerank, and the compatibility enrichment path and prevents a future chat caller from silently escaping the limit.
  Date/Author: 2026-08-10 / Codex

## Outcomes & Retrospective

Assessment now calls a dedicated provider method and requests only `qualityScore`, `depthScore`, and one to three short reasons. All chat payloads carry `max_tokens:1024`; Qwen models receive `enable_thinking:false`, while the configured MiMo and other recognized thinking model families receive `thinking:{"type":"disabled"}`. Unknown model families deliberately receive no guessed vendor-specific switch, so adding another family requires mapping its documented parameter.

Every chat outcome emits one `llm_call` event through the standard logger. The event includes stage, model, duration, status, safe candidate/user identity when available, and a nested provider-usage object. Missing usage is explicit through `available:false` with zero counters. The standard logger is mirrored to retained daily files by `logging.Configure`.

Validation passed with `go test ./observability ./recommendation -count=1`, `go test ./... -count=1`, `go test -race ./observability ./recommendation -count=1`, and `git diff --check`. No migration or new dependency was needed.

## Context and Orientation

`api/api/controller.go` injects `recommendation.ConfiguredArticleAssessor()` into each ready, deduplicated article job. `api/recommendation/article_assessor.go` adapts that provider result to the discovery package's versioned assessment. `api/recommendation/openai_provider.go` owns embedding and chat HTTP requests; both article assessment and `Rerank` currently pass through its generic JSON helper. `api/observability/event.go` defines the bounded JSON event written through the standard logger. `api/logging/runtime.go` mirrors that logger to stdout and retained daily files.

A token usage record means integer counts returned by the provider: prompt tokens, completion tokens, completion reasoning tokens, cached prompt tokens, and total tokens. It must not contain the prompt, article body, completion content, API key, or full request URL. A call stage is one of `article_assessment`, `recommendation_rerank`, or the compatibility stage `candidate_enrichment`.

## Plan of Work

Add a dedicated article-assessment provider interface and compact result type in `api/recommendation/provider.go`. Change `EnrichmentArticleAssessor` to call that method and retain the returned short reasons. Implement the method in `OpenAICompatibleProvider` with a strict JSON Schema that requires only `qualityScore`, `depthScore`, and one to three short reasons.

Refactor `chatJSON` to accept fixed request options containing the call stage, safe candidate/user identity, response schema, the 1024 output limit, and whether reasoning must be disabled. Parse TokenHub/OpenAI usage and reasoning fields. For every outcome, including transport errors, HTTP errors, empty completions, and JSON decoding errors, write exactly one `llm_call` event. Extend `observability.Event` only with numeric usage/duration fields plus bounded stage/model strings; do not add arbitrary maps or payload fields.

Update provider and assessor tests to inspect outbound payloads, prove model-specific reasoning switches and `max_tokens:1024`, validate the compact schema, and capture structured log lines for successful and invalid responses. Extend the observability schema test to prohibit prompt/completion/reasoning text fields while allowing numeric token counts.

## Concrete Steps

From the repository root, edit files with `apply_patch`, then run:

    cd api
    gofmt -w observability/event.go observability/event_test.go recommendation/provider.go recommendation/article_assessor.go recommendation/article_assessor_test.go recommendation/openai_provider.go recommendation/openai_provider_test.go
    go test ./observability ./recommendation -count=1
    go test ./... -count=1
    go test -race ./observability ./recommendation -count=1

Return to the repository root and run `git diff --check`. Update this ExecPlan with observed output and stage only the files named by this task. Commit them as one focused `Change:` commit without including the user's pre-existing changes.

## Validation and Acceptance

An article assessment request must contain `max_tokens:1024`, a disabled reasoning switch appropriate to the configured model, and `response_format.type:"json_schema"`. Its schema must reject extra properties and expose only `qualityScore`, `depthScore`, and `reasons`. A rerank request must contain the same output limit and disabled reasoning switch.

Each chat request must emit exactly one `dataark_event` whose event name is `llm_call`. A successful fake response with usage must produce the exact prompt, completion, reasoning, cached, and total counts plus nonnegative duration and stage. An invalid completion must still log the provider usage with failed status. A request failure without usage must log zero counts and `llm_usage.available:false`. No log may contain a prompt, article body, completion content, reasoning content, authorization header, or API key.

## Idempotence and Recovery

The change has no migration and is safe to build or restart repeatedly. Existing assessments and recommendation days are untouched. If a configured provider rejects JSON Schema or its documented reasoning switch, the existing deterministic rule assessment and rerank fallback remain active and the failed usage event provides the diagnostic. Preserve all pre-existing worktree modifications and do not stage them.

## Artifacts and Notes

The intended event shape is:

    dataark_event {"event":"llm_call","status":"success","llm_stage":"article_assessment","llm_model":"mimo-v2.5-pro","duration_ms":842,"llm_usage":{"available":true,"prompt_tokens":1234,"completion_tokens":90,"reasoning_tokens":0,"cached_tokens":0,"total_tokens":1324}}

The event deliberately contains counts only, never the associated text.

## Interfaces and Dependencies

`ArticleAssessmentProvider` exposes `AssessArticle(context.Context, ArticleAssessmentInput) (ArticleAssessmentResult, error)`. The input contains title, body text, and publication time only. The result contains quality score, depth score, short reasons, model, and prompt version. `OpenAICompatibleProvider.chatJSON` accepts an internal options struct rather than exporting transport details. No new third-party dependency or database migration is required.

Revision note (2026-08-10): completed implementation and replaced the provisional event example and validation checklist with the verified nested usage schema, provider compatibility boundary, and test results.
