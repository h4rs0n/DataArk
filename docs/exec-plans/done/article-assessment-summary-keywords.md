# Produce summary and keywords during article assessment

> 归档说明（2026-09-30 核对）：本文保留实施当时的背景、决策、路径与验证记录，部分内容已被后续变更取代。现行行为与操作入口见 [文档索引](../../README.md)。历史测试输出不代表当前部署状态。
>
> 当前仅支持 active；输出文本要求简体中文，summary/topics 写回 material，见 `api/assessment/openai.go`、`api/assessment/material.go`。

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds.

This document must be maintained in accordance with `PLANS.md` at the repository root.

## Purpose / Big Picture

Recommendation cards currently show a feed meta description or fall back to the article URL, and most cards have empty topic chips. After this change, one `article_assessment` model call returns the existing three reading-value scores plus a short summary and topical keywords. The immutable assessment row stores that payload. The live discovery candidate is updated so new “猜你喜欢” and “今日推荐” cards can show a real summary and tags. Observe mode still refuses to activate model scores, but it does write the summary and keywords because those fields do not affect ranking.

## Progress

- [x] (2026-08-17 22:20+08:00) Authored this ExecPlan and locked the output contract, version bump, observe write-back, and observe-safe backfill behavior.
- [x] (2026-08-17 22:25+08:00) Extended the assessment prompt, JSON Schema, and local validation; bumped `PolicyVersion` to `article-value-v4` and `PromptVersion` to `openai-compatible-article-assessment-v4`.
- [x] (2026-08-17 22:28+08:00) Added Goose migration 000027 and persist `summary`/`keywords` on assessment rows.
- [x] (2026-08-17 22:30+08:00) Wrote summary and keywords back to `discovery_candidates` after a successful model assessment, including observe mode.
- [x] (2026-08-17 22:32+08:00) Allowed observe-mode backfill to enqueue missing v4 assessments without activating model scores.
- [x] (2026-08-17 22:40+08:00) Updated tests and the operations runbook; focused packages passed `go test ./recommendation ./discovery ./articlevalue ./assessmenteval ./bootstrap ./api -count=1`.

## Surprises & Discoveries

- Observation: the Goose collector test asserted exactly 26 migrations and did not inspect 000026 individually. Adding 000027 required bumping that count to 27 and adding a dedicated content assertion.
  Evidence: `TestV3GooseMigrationIsAdditiveAndParseable` in `api/bootstrap/database_v3_test.go`.
- Observation: active backfill that reactivates a stored observe row also needs write-back, otherwise a later activation would turn on model scores without filling empty candidate summaries.
  Evidence: `PrepareArticleAssessmentBackfill` now calls `applyAssessmentArticleMetadata` on the active reactivation path.

## Decision Log

- Decision: Keep scoring, summary, and keywords in one required JSON object. A missing or invalid summary fails the whole assessment and falls back to the deterministic rule scores without writing metadata.
  Rationale: The operator asked for one assessment-stage call, not a second enrichment hop. Partial success would leave cards with scores from one model output and summaries from another.
  Date/Author: 2026-08-17 / Codex
- Decision: Map LLM `keywords` onto `discovery_candidates.topics`. Do not write `entities` or `enrichment_status`.
  Rationale: The recommendation card already renders `topics`. Re-enabling the unused `candidate_enrichment` path would put a remote model call back on a retired publish-path.
  Date/Author: 2026-08-17 / Codex
- Decision: Write summary and keywords in observe mode after a valid model row is persisted. Do not change `current_assessment_id` in observe mode.
  Rationale: Docker currently defaults to observe. Summaries are display metadata and do not change eligibility or ranking.
  Date/Author: 2026-08-17 / Codex
- Decision: Bump `articlevalue.PolicyVersion` to `article-value-v4` and `PromptVersion` to `openai-compatible-article-assessment-v4`.
  Rationale: Assessment rows are unique on `(candidate_id, content_version, assessor, assessor_version, policy_version)` and are immutable. Bumping only the prompt version would reuse v3 rows that have no summary.
  Date/Author: 2026-08-17 / Codex
- Decision: In observe mode, backfill selects candidates that lack a persisted row for the current assessor triple and only enqueues `process-candidate`. It never activates a stored model row.
  Rationale: The previous backfill query looked at `current_assessment_id`. Observe leaves that pointer on the rule row, so the same articles would be selected forever if observe backfill were naively enabled.
  Date/Author: 2026-08-17 / Codex

## Outcomes & Retrospective

`article_assessment` now requires `summary` and `keywords` in the same JSON object as the three scores. Valid model output is stored on the immutable assessment row and copied onto `discovery_candidates.summary` and `topics`. Observe mode still keeps rule scores active, but cards can show the new summary after the next feed refresh. Observe backfill enqueues missing v4 assessments instead of refusing. Focused tests passed. Remaining work for operators is to deploy migration 000027 and run bounded observe backfill for existing inventory. The unused `Enrich()` path was not restored.

## Context and Orientation

DataArk recommends individual articles discovered from blogs. After extract and dedupe, `discovery.AssessCandidate` in `api/discovery/assessor.go` always writes a deterministic rule assessment, then optionally calls an enhanced assessor. The production enhanced assessor is `recommendation.EnrichmentArticleAssessor` in `api/recommendation/article_assessor.go`. It calls `OpenAICompatibleProvider.AssessArticle` in `api/recommendation/openai_provider.go`.

The model returns `qualityScore`, `depthScore`, `evergreenScore`, exactly two `reasons`, `summary`, and `keywords`. Those integers are divided by 100, capped by evidence length in `api/articlevalue/evidence.go`, and stored on `discovery_article_assessments` together with the summary and keyword list. `ARTICLE_ASSESSMENT_MODE=observe` persists the model row but keeps `discovery_candidates.current_assessment_id` on the rule row. `active` points the candidate at the model row and copies quality/depth into eligibility. Both modes write summary and keywords onto the live candidate after a valid model row exists.

The card in `web/src/components/recommendations/RecommendationArticleCard.vue` displays `candidate.summary || candidate.url` and up to four `candidate.topics`. Feed and HTML extraction often leave `summary` empty. An older LLM enrichment method still exists as `OpenAICompatibleProvider.Enrich`, but the production pipeline does not call it.

Terms used in this plan: “assessment row” means one immutable `discovery_article_assessments` record. “Write-back” means copying summary and keywords onto the live `discovery_candidates` row. “Snapshot” means `recommendation_items.snapshot_summary` / `snapshot_topics` frozen when a digest or personalized feed is published. Changing the live candidate does not rewrite already published snapshots.

## Plan of Work

Create this ExecPlan first. Then change the model contract, persistence, write-back, observe backfill, tests, and the runbook. Do not re-enable `Enrich()`. Do not add summary labeling to the human workflow. Do not force Docker onto `active`.

## Concrete Steps

From the repository root, implement the files named below. After the code exists, run this from `api/`:

    go test ./recommendation ./discovery ./articlevalue ./assessmenteval ./bootstrap ./api -count=1

Expect every package to pass. The new tests named in Validation must fail if summary/keywords are omitted from the schema or if observe backfill activates model scores.

## Validation and Acceptance

A valid model JSON object contains three 0–100 integer scores, exactly two reasons of at most 120 characters, a summary of 1–200 characters, and 3–8 keywords of 1–20 characters each. `TestOpenAICompatibleProviderArticleAssessmentUsesCompactSchemaAndQwenSwitch` must see six schema properties. `TestObserveAssessmentWritesSummaryWithoutActivatingModelScores` must leave `current_assessment_id` on the rule row while `discovery_candidates.summary` and `topics` change. `TestArticleAssessmentBackfillObserveEnqueuesWithoutActivating` must enqueue in observe mode and must not point the candidate at the model row. Rule-only assessment must leave an existing summary untouched.

After deployment, a newly processed article in observe mode shows a non-URL summary on the next personalized-feed refresh. Existing digest cards stay on their snapshots until a new batch is generated. Stock inventory needs the owner backfill endpoint, which now works in observe mode as enqueue-only.

## Idempotence and Recovery

Goose migration 000027 only adds nullable columns. Re-running assessment for the same `(candidate, content_version, assessor, assessor_version, policy_version)` hits `ON CONFLICT DO NOTHING` and reloads the stored row, then write-back reapplies the same summary and keywords. Observe backfill is idempotent because a persisted v4 model row excludes the candidate from the observe selection query. Rollback of scores remains the existing admin rollback; it does not delete assessment rows and does not clear written summaries.

## Artifacts and Notes

The required model JSON object is:

    {
      "qualityScore": 0-100,
      "depthScore": 0-100,
      "evergreenScore": 0-100,
      "reasons": ["evidence", "limitation"],
      "summary": "1 to 200 characters",
      "keywords": ["topic", "topic", "topic"]
    }

`depthScore` in the example above is 0-100; the typed range is the same three integer axes as v3. Keywords are stored on the assessment row as a JSON array text field named `keywords` and copied onto `discovery_candidates.topics`.

## Interfaces and Dependencies

In `api/recommendation/provider.go`, `ArticleAssessmentResult` gains `Summary string` and `Keywords []string`.

In `api/discovery/assessor.go`, `ArticleAssessmentResult` gains the same two fields. `persistArticleAssessment` writes them. `applyAssessmentArticleMetadata` updates the candidate when they are non-empty.

In `api/discovery/model_v3.go`, `DiscoveryArticleAssessment` gains `Summary` and `Keywords` text columns.

`OpenAICompatibleProvider.AssessArticle` remains the only production HTTP chat call for this feature. `max_tokens` stays 2048. Prompt and policy constants live in `api/articlevalue/evidence.go`.

## Revision notes

2026-08-17: Marked every Progress item complete after implementation and focused tests. Recorded the Goose count bump and active-path write-back as discoveries. Replaced the in-progress Outcomes section with the delivered behavior.
