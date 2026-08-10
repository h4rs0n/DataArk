# Redesign article assessment around calibrated reading value

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds. This plan follows `PLANS.md` at the repository root.

## Purpose / Big Picture

DataArk recommends individual articles, but its current semantic assessor asks a model for only vague quality and depth scores, copies those values into dimensions the model never judged, sends unbounded extracted text, and activates scores that cluster near 1.0. After this change, every article is judged independently using a bounded title-and-clean-text evidence package, an anchored three-score rubric, deterministic evidence caps, and a compatible structured-output protocol. Operators can evaluate the new policy against a blind human-labelled sample before activating it, then re-assess old inventory in recoverable batches without replacing a working score on failure.

The behavior is observable in four ways. Unit tests show that raw HTML and source identity never reach the model, oversized text is sampled from its beginning, middle, and end, and only quality, depth, evergreen value, and two reasons are accepted. Structured `llm_call` logs show the evidence budget and response mode for every provider attempt. The owner-only recommendation-center workflow persists a deterministic two-pass blind sample, adjudication, model repeats, and activation metrics without downloadable article artifacts. Owner-only backfill and rollback endpoints change active assessments in batches of at most 250 while preserving immutable assessment rows and published recommendation snapshots.

## Progress

- [x] (2026-08-10 12:45+08:00) Reconciled `/tmp/Design.md` with the current DataArk implementation and production-shaped database; fixed the product boundary at article-independent assessment and ranking-first eligibility.
- [x] (2026-08-10 12:45+08:00) Fixed the evidence budget, score schema, local caps, gold-sample source, one-person two-pass annotation workflow, observe/active rollout, and 250-item backfill policy with the user.
- [x] (2026-08-10 13:05+08:00) Implemented bounded assessment evidence, the v3 anchored rubric, required three-score/two-reason schema, and all evidence cap bands.
- [x] (2026-08-10 13:08+08:00) Implemented serialized structured-output capability probing, one compatible fallback, and per-attempt safe token/evidence/duration telemetry.
- [x] (2026-08-10 13:10+08:00) Replaced invented semantic dimensions with quality, depth, evergreen value, local confidence, and a conservative deterministic fallback.
- [x] (2026-08-10 13:14+08:00) Added observe/active activation, direct activation of persisted observe rows, recoverable 250-item backfill, and immutable rollback.
- [x] (2026-08-10 13:18+08:00) Added deterministic database sampling, private offline labelling/adjudication HTML, two-run scoring, and evaluation reporting.
- [x] (2026-08-10 13:20+08:00) Updated flags, Compose defaults, README guidance, and the dedicated operations runbook.
- [x] (2026-08-10 13:24+08:00) Passed focused tests, the full backend suite, critical race tests, Go vet, Compose configuration validation, and whitespace review.
- [x] (2026-08-10 14:25+08:00) Replaced the offline HTML/JSON labelling path with an owner-only database workflow and a dedicated recommendation-center tab.
- [x] (2026-08-10 14:35+08:00) Added durable pass progress, the enforced 72-hour transition, blind 30-item repeat selection, conflict adjudication, background two-run model scoring, safe usage persistence, and the activation report.
- [x] (2026-08-10 16:17+08:00) Validated the online workflow with focused and full backend tests, race detection, Go vet, frontend tests/type-check/build, Compose image build, production migration 23, live authorization response, and embedded-tab inspection.

## Surprises & Discoveries

- Observation: the running database has 9,037 assessable representative article versions across 257 sites; 5,478 are Chinese and 3,535 English.
  Evidence: read-only production-shaped PostgreSQL queries on 2026-08-10.
- Observation: 8,871 of 9,613 ready candidates are eligible, while active model scores have median 0.90 and their 75th and 90th percentiles are both 1.00.
  Evidence: grouped queries over `discovery_candidates` and `discovery_article_assessments`.
- Observation: all 2,754 model assessments copied one quality score into information density, originality, evidence, and readability.
  Evidence: equality count across the four persisted columns was 2,754 of 2,754.
- Observation: ready body text has median 2,074 characters, 90th percentile 8,879 characters, and maximum 578,548 characters, so completion limits alone cannot bound assessment cost.
  Evidence: PostgreSQL percentile query over ready candidate body text.
- Observation: the deployed MiMo-compatible endpoint produced 19 invalid structured outputs in 20 newly logged assessment attempts, usually with six to ten completion tokens.
  Evidence: safe `llm_call` token logs; response content was not inspected or persisted.
- Observation: a required integer field needs a pointer-backed decode boundary because unmarshalling a missing JSON number into an `int` is indistinguishable from a valid zero score.
  Evidence: the local schema-violation matrix now rejects missing, fractional, out-of-range, extra, overlong, and incorrectly counted fields independently of provider schema enforcement.
- Observation: `CandidateID` contains the substring `date`, so source-boundary reflection tests must use semantic forbidden names rather than an unqualified `date` substring.
  Evidence: the first full test run caught the false positive; the final full run passed after retaining explicit `publication` and `published` exclusions.
- Observation: a downloadable self-contained HTML page necessarily contains all selected article bodies and browser-local state, even when it has no network dependencies.
  Evidence: the original `api/assessmenteval/html.go` embedded the entire blind page payload as base64 and relied on `localStorage`; the replacement API returns one blind item at a time with `Cache-Control: no-store`.
- Observation: Compose had built the new image while retaining the two-hour-old API container on the first ordinary `up -d`; comparing image IDs exposed the stale process.
  Evidence: the built image was `sha256:243bf…` while the container still referenced `sha256:5cca…`; `up -d --force-recreate dataarkapi` loaded the new binary, and startup logged successful migration to version 23.

## Decision Log

- Decision: Keep assessment strictly article-level and exclude source, site, popularity, graph, recency, and user-feedback inputs.
  Rationale: DataArk recommends articles and already guarantees that the same text receives the same quality result regardless of source yield. Blog-level reputation would hide long-tail gems.
  Date/Author: 2026-08-10 / user and Codex.
- Decision: Optimize ranking accuracy; eligibility rejects only semantic quality below 20/100 after existing article-validity gates.
  Rationale: Quality scores should order usable inventory rather than act as an unstable high-recall classifier.
  Date/Author: 2026-08-10 / user and Codex.
- Decision: Persist only quality, depth, evergreen value, local confidence, and explanations for new assessments; retain legacy database columns without populating fake values.
  Rationale: These are the only semantic signals used by eligibility, ranking, or evergreen inventory.
  Date/Author: 2026-08-10 / user and Codex.
- Decision: Limit model evidence to approximately 6,000 tokens and sample oversized bodies as 45 percent beginning, 20 percent middle, and 35 percent end.
  Rationale: This bounds cost while preserving introductions, representative analysis, and conclusions.
  Date/Author: 2026-08-10 / user and Codex.
- Decision: Probe strict JSON Schema, retry compatible JSON object output once when warranted, and cache a successful downgrade per endpoint and model for the process lifetime.
  Rationale: Strict output remains preferred, but an OpenAI-compatible endpoint must not turn an entire crawl batch into fallback assessments merely because its schema implementation differs.
  Date/Author: 2026-08-10 / user and Codex.
- Decision: Build a 120-article gold set from immutable current DataArk content versions, label it in a local offline HTML artifact, and use one person in two blind rounds separated by at least 72 hours.
  Rationale: BlogClaw's labels are blog-level and cannot validate DataArk article ranking. Local artifacts avoid committing public article bodies.
  Date/Author: 2026-08-10 / user and Codex.
- Decision: Default production rollout to observe mode and re-assess old inventory in manually triggered batches of at most 250.
  Rationale: Existing scores remain available while the new scale is measured, provider quota and RPM stay bounded, and each activation remains reversible.
  Date/Author: 2026-08-10 / user and Codex.
- Decision: Supersede private offline HTML and CLI artifacts with an owner-only workflow inside the recommendation center.
  Rationale: The operator asked to make human labelling part of the product and remove offline processing. Server-side rows survive browser closure and restarts, enforce the 72-hour boundary centrally, keep hidden baseline/source fields out of blind HTTP responses, and let the configured production model generate the gate report without copying credentials or full article manifests to local files.
  Date/Author: 2026-08-10 / user and Codex.

## Outcomes & Retrospective

Article assessment now receives only candidate identity, title, and clean body text. The evidence builder caps the complete prompt evidence at approximately 6,000 tokens, including an abnormal-title cap, and samples overlong bodies from the beginning, middle, and end. The provider requests only three 0–100 integer scores and exactly two short reasons, disables supported reasoning modes, keeps the 1,024 completion limit, rejects every schema violation locally, and records one safe usage event for every real HTTP attempt. No prompt, completion, reasoning text, URL, source identity, or credentials are logged.

Discovery persists only quality, depth, evergreen value, confidence, and reasons for v3 rows. It no longer copies quality into legacy dimensions or treats confidence as an eligibility gate. Observe mode is the default, failures retain a usable prior pointer, and new degraded articles use a conservative deterministic row. Owner-only backfill and rollback endpoints are bounded at 250 and covered for dry run, direct observe-row activation, idempotence, partial queue failure recovery, and pointer preservation.

The original `api/cmd/article-assessment-eval`, self-contained HTML generator, browser-local persistence, and filesystem JSON/log readers have now been removed. `api/assessmenteval/workflow.go` persists the frozen sample, labels, repeat subset, conflicts, model scores, safe call metrics, and final report in PostgreSQL. `web/src/components/recommendations/ArticleAssessmentWorkflow.vue` exposes the workflow only to owners under the recommendation center and fetches one blind article at a time. The live Compose database migrated additively from 22 to 23 and the replacement container serves the new tab while rejecting unauthenticated workflow requests with 401. The service deliberately remains in observe mode until one human completes the two passes at least 72 hours apart and the resulting report says `activationReady:true`.

Validation passed with `go test ./... -count=1`; `go test -race ./articlevalue ./assessmenteval ./discovery ./recommendation ./observability ./api ./jobqueue -count=1`; `go vet ./...`; `docker compose -f docker/docker-compose.yml config --quiet`; and `git diff --check`.

## Context and Orientation

`api/discovery/candidate_processing.go` fetches an article, passes rendered HTML through `api/discovery/extractor.go`, and stores DOM Distiller's flattened clean text in the candidate and immutable content-version rows. `api/discovery/assessor.go` runs a deterministic assessor, optionally runs an enhanced assessor, saves immutable rows in `discovery_article_assessments`, and points the candidate at one active row. `api/recommendation/article_assessor.go` adapts the OpenAI-compatible provider into that discovery boundary. `api/recommendation/openai_provider.go` builds chat requests and logs safe provider usage through `api/observability`.

An assessment is immutable for one candidate content version, assessor identity, assessor version, and policy version. Active assessment means the row referenced by `discovery_candidates.current_assessment_id`; recommendation items copy that reference into immutable daily snapshots. Observe mode means a v3 model assessment may be stored without changing this pointer. Degraded means a new article has no usable model assessment and therefore uses the conservative deterministic row until an explicit retry succeeds.

The legacy assessment table contains information-density, originality, completeness, evidence, and readability columns. They remain readable for historical rows, but v3 does not invent values for them. The consumed columns are `overall_quality`, `depth`, `evergreen_value`, `confidence`, and `reasons`.

## Plan of Work

First, add a reusable evidence builder in the recommendation package. It will estimate tokens conservatively, normalize already-clean text, reserve the title and omission markers inside a 6,000-token budget, and return either the full text or non-overlapping beginning, middle, and ending excerpts. The model prompt will explicitly define the three axes and six score bands, identify article content as untrusted data, and forbid source, popularity, recency, topic preference, or length alone from changing quality. The strict response schema will require integer `qualityScore`, `depthScore`, and `evergreenScore` values from 0 through 100 plus exactly two reasons of no more than 120 characters. Assessment requests remain non-thinking with 1,024 maximum completion tokens and temperature 0.1.

Second, extend the chat helper so an assessment can perform a strict-schema attempt and one compatible JSON-object attempt without hiding either HTTP call. A concurrency-safe registry keyed by normalized endpoint and model will serialize only the first capability probe. Unsupported-schema provider errors, or an invalid strict response followed by a valid compatible response, establish JSON-object mode until process restart. Provider access, quota, timeout, and rate errors do not trigger a format retry. Each attempt logs stage, response mode, attempt number, estimated evidence tokens, truncation, model, duration, and returned usage counts without logging prompt or completion content.

Third, simplify the discovery assessment result and deterministic assessor around quality, depth, evergreen value, confidence, and reasons. Apply the agreed evidence caps after a valid model response. A deterministic row uses conservative length-bucket scores and confidence 0.25 rather than lexical-marker pseudo-semantics. Semantic quality below 0.20 is ineligible; confidence alone no longer removes an otherwise usable article. When enhanced assessment fails, an existing active row for the same content remains active and only the error is updated. With no prior row, the deterministic result becomes active with assessment state `degraded` so recommendation generation remains available.

Fourth, add observe and active modes plus a two-call assessment concurrency guard. In observe mode, persist valid v3 model rows but retain the current pointer. In active mode, atomically activate a valid v3 row. Add owner-only dry-run-capable backfill and rollback endpoints. Backfill first activates an already persisted matching v3 row, otherwise marks candidates pending and reuses the existing idempotent process-candidate queue. A queue insertion failure is recoverable because pending state is durable. Rollback selects the most recent non-v3 valid row for the same content version, falling back to the v3 deterministic row, and never edits published recommendation items.

Fifth, build deterministic sample and report primitives in `api/assessmenteval`. Sampling reads immutable representative content versions, applies fixed hash ordering and a maximum of two articles per host. The core 80-item language/length quotas are Chinese 11/18/14/4/2 and English 5/10/11/4/1 across the five agreed character buckets. The remaining 40 items are eight each from low active score, 0.45–0.55 boundary, 0.95-or-higher score, model/rule disagreement, and 20,000-or-more-character bodies. Report logic identifies cross-band or greater-than-15-point conflicts and computes rank, error, head/tail, gate, saturation, protocol, stability, duration, and token metrics against the stored active baseline.

Sixth, replace the initial offline adapter with `api/assessmenteval/workflow.go`, migration `000023_article_assessment_workflow.sql`, owner-only controller endpoints, and `web/src/components/recommendations/ArticleAssessmentWorkflow.vue`. A workflow row freezes one sample; item rows retain only hidden sample metadata and immutable content-version references; label rows enforce editable phases; score and call rows retain repeat output and payload-safe usage. The item API serves only title, body, language, character count, sample ID, and the current label. The service centrally freezes pass one, enforces 72 hours, selects the blind repeat, derives conflicts, launches two-concurrency model scoring, and persists the report. Remove `api/cmd/article-assessment-eval`, `api/assessmenteval/html.go`, and filesystem artifact helpers after their behavior is covered by database workflow tests.

Finally, document the new flags and rollout commands, add comprehensive tests, exercise the real gold workflow only with user-provided local credentials and artifacts, and switch from observe to active only when all fixed gates pass. Run 48 hours on new articles before manually starting each 250-item backfill batch.

## Concrete Steps

From the repository root, implement and format the Go changes, then run:

    cd /home/harson/sideProj/DataArk/api
    go test ./discovery ./recommendation ./observability ./api ./jobqueue -count=1
    go test ./... -count=1
    go test -race ./discovery ./recommendation ./observability ./api ./jobqueue -count=1

Validate configuration and patch hygiene from the repository root:

    cd /home/harson/sideProj/DataArk
    docker compose -f docker/docker-compose.yml config --quiet
    git diff --check
    git status --short

The recommendation-center page is the supported evaluation entry point. No manifest, article HTML, label, score, or report file is generated under `/tmp`; all workflow state is durable in the application database and remains protected by the existing owner authorization boundary.

## Validation and Acceptance

Evidence tests must prove that full short bodies are retained, oversized bodies contain content from all three positions without overlap, the estimated budget is bounded, and source/URL/date/HTML fields cannot enter the provider input. Schema tests must prove that missing, extra, fractional, out-of-range, overlong, or incorrectly counted fields fail locally.

Compatibility tests must prove that a supported provider receives one schema request, an incompatible provider receives only one probe plus one compatible retry across concurrent calls, and quota, rate, or timeout failures are not retried as format failures. Logs must contain one record per HTTP attempt with no sentinel prompt, completion, reasoning text, API key, or URL.

Assessment tests must prove the four evidence cap bands, the 0.20 eligibility floor, conservative degraded fallback, preservation of an old active row on model failure, observe versus active behavior, source-independent equality, immutable content-version history, idempotent backfill, partial queue failure recovery, and rollback.

The online workflow must additionally prove that only owners can create/read/mutate a run, an item response contains no host/stratum/baseline/model fields, progress survives a new request, pass two cannot start before 72 hours, exactly 30 repeat items are selected, conflicts require adjudication, and a server restart changes `evaluating` to retryable `evaluation_failed`. The human labels are acceptable when quality repeat Spearman is at least 0.85, depth and evergreen repeat Spearman are at least 0.75, per-axis repeat MAE is no more than 10, and score-band agreement is at least 80 percent. Model activation requires core quality Spearman at least 0.70, Kendall at least 0.50, quality MAE no more than 12, depth and evergreen Spearman at least 0.60, top- and bottom-quintile hit rates at least 70 percent, quality Spearman at least 0.10 above the stored active baseline, at least 95 percent eligible recall for human quality at least 40, at least 70 percent rejection for human quality below 20, and repeat-model MAE no more than 5 with at least 90 percent band agreement. Valid output rate after compatibility fallback must be at least 98 percent, reasoning tokens must remain zero, prompt-token p95 must not exceed 7,500, and no prompt may exceed 8,500 tokens.

## Idempotence and Recovery

Assessment rows remain immutable and uniquely versioned. Each workflow freezes its stored seed, content-version references, baseline scores, and order. Repeating a transition is rejected after the phase is frozen; retrying a failed model evaluation increments an evaluation generation and retains prior score/call audit rows. Pass-two generation derives its subset from manifest hashes and is repeatable after the 72-hour boundary.

Observe mode and dry-run endpoints do not change active pointers. Backfill never clears a working pointer before success. If enqueueing stops halfway, pending candidates are found by existing recovery. Repeating a batch skips the configured model and policy rows already present. Rollback changes only the current pointer and candidate score projection; it does not delete v3 assessments or mutate recommendation history.

## Artifacts and Notes

Do not place real article text, gold labels, model answers, credentials, or generated reports in this repository. They now remain in the application database behind owner-only APIs. Record only aggregate metrics and redacted command transcripts in this plan. Preserve the user's unrelated modifications to `go.work.sum`, `makefile`, `web/public/favicon.ico`, `.codex`, and `passwd.txt`.

## Interfaces and Dependencies

The provider response adds `evergreenScore` and removes no externally served HTTP response field. Historical assessment JSON remains backward compatible because the GORM model keeps legacy columns. New candidate assessment state `degraded` is additive.

The server gains `-article-assessment-mode` with values `observe` and `active`, and `-article-assessment-concurrency` with default 2. Docker configuration exposes matching environment values. Migration 23 adds workflow run, item, label, score, and safe call-audit tables. Owner-only endpoints under `/api/admin/recommendations/article-assessment-workflow` expose summary, run creation, one blind item, label upsert, phase advancement, and evaluation start. No new third-party runtime dependency is required; synchronization, hashing, statistics, persistence, and UI use existing Go, GORM/PostgreSQL, Vue, and Arco dependencies.

Revision note (2026-08-10): Initial self-contained plan created after the product and rollout decisions were finalized with the user.

Revision note (2026-08-10): Recorded the completed implementation, final validation evidence, and the intentional human-label/activation handoff.

Revision note (2026-08-10): Replaced the original private offline artifact handoff with the user-requested recommendation-center workflow, documented its durable schema/API/security boundary, and recorded removal of the CLI/HTML/filesystem adapter.

Revision note (2026-08-10): Recorded final online-workflow validation, the successful production-shaped migration to version 23, and the Compose stale-container discovery/recovery.
