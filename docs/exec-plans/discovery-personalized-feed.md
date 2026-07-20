# Add a personalized discovery feed with refreshable batches

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds. This document must be maintained in accordance with `PLANS.md` at the repository root.

## Purpose / Big Picture

Users currently see up to eighty unread discovery candidates in one score-sorted list. After this change, the unread state in “内容发现” becomes a “猜你喜欢” feed that presents at most ten personalized articles at a time. Users can press “换一换” for another batch, and every card offers the same original-link, archive, provenance, feedback, scope-adjustment, and revert actions as the cards in “今日推荐”. Read, ignored, and archived candidate states remain unchanged.

## Progress

- [x] (2026-07-20 20:30+08:00) Inspected the current discovery list, daily recommendation selection, feedback, provenance, exposure tracking, migrations, and frontend behavior.
- [x] (2026-07-20 20:35+08:00) Confirmed product decisions: full action parity, only unread state changes, refresh avoids recent repeats, and shortages return fewer than ten eligible articles.
- [x] (2026-07-20 23:20+08:00) Added the feed-batch schema, nullable recommendation-item parent, selection service, protected endpoints, and focused backend tests.
- [x] (2026-07-20 23:35+08:00) Shared the daily recommendation card UI and implemented current-batch loading, first-batch creation, shortages, and explicit refresh.
- [x] (2026-07-20 23:45+08:00) Passed full Go tests, frontend tests, Node 24 production build, Docker Compose rebuild, PostgreSQL migration, and Chrome DevTools validation.
- [x] (2026-07-20 23:50+08:00) Recorded results and prepared the complete implementation as one focused change for commit.

## Surprises & Discoveries

- Observation: The existing `RecommendationItem` ID is the identity used by feedback, scope blocking, feedback reversal, and provenance lookup.
  Evidence: All corresponding routes are under `/api/recommendations/items/:itemId`, and `RecommendationFeedback` references `recommendation_items`.
- Observation: `UserCandidateState` already records exposure count and timestamps, so refresh de-duplication can reuse the existing recommendation recurrence policy.
  Evidence: `discovery.RecordUserCandidateExposure` increments `exposure_count`, and the v3 selector excludes candidates inside the configured re-exposure cooldown.
- Observation: GORM creates a single-column unique index unless both members declare the same composite index name.
  Evidence: The first SQLite feed test rejected the second item with `UNIQUE constraint failed: recommendation_items.feed_batch_id`; adding the feed composite index tag to `CandidateID` corrected the schema and the tests passed.
- Observation: A parent view's scoped responsive rule does not reach the action container inside a child component.
  Evidence: Chrome at 760px showed a one-column card but a 236px action container; moving the 820px action rule into the shared component produced a full-width, left-aligned action area with no overflow.
- Observation: The persisted browser login expired during validation.
  Evidence: The production page first loaded a real 10-item batch through a 201 refresh and 200 feedback-state requests, then a later unauthenticated diagnostic request returned 401. Remaining interaction checks used an isolated browser context with mocked API responses and no credentials.

## Decision Log

- Decision: A feed batch is independent of `RecommendationDay`, while its cards reuse `RecommendationItem`.
  Rationale: This preserves all existing item-level actions without polluting immutable daily digests or historical reports.
  Date/Author: 2026-07-20 / Codex
- Decision: The live feed uses deterministic v3 selection and diversification without the external LLM reranker.
  Rationale: “换一换” must be responsive and reliable while still using preferences, feedback, hard filters, and diversity rules.
  Date/Author: 2026-07-20 / Codex
- Decision: A displayed batch remains stable after link, archive, or feedback actions; updated state affects the next batch.
  Rationale: This matches daily recommendation behavior and leaves the card available for feedback reversal and context inspection.
  Date/Author: 2026-07-20 / Codex

## Outcomes & Retrospective

The unread discovery state now presents a persistent personalized batch of up to ten articles with explicit refresh, shortage feedback, and the complete daily-card action set. Feed items reuse the recommendation feedback and provenance audit path while remaining separate from immutable daily digests. Backend, frontend, migration, Docker, and browser checks passed. The production database is at Goose version 22, and the final image is `sha256:e52cc8c4ad0cee0d9db2993ff9e6decf8338824fe8770dd7da6dfff76974a527`.

## Context and Orientation

`web/src/views/RecommendationsView.vue` owns the recommendation center and currently renders both daily cards and compact discovery candidates. `api/recommendation/service.go` generates daily items, records exposures, and implements feedback. `api/recommendation/selection_v3.go` applies hard eligibility filters, user blocks, recurrence cooldowns, preference scoring, and diversity. PostgreSQL schema changes are embedded Goose migrations under `api/migrations/`; SQLite tests use the model list returned by `recommendation.V3Models()`.

A “feed batch” in this plan means one persisted set of up to ten personalized recommendation items for one user. Exactly one batch is current for each user. A “hard filter” means a rule that must never be relaxed: processing ready, article eligible, dedupe ready and representative, not blocked, and not explicitly read, archived, or given feedback that removes it from unread candidates.

## Plan of Work

Add an additive Goose migration and Go model for feed batches. Change `RecommendationItem.DayID` to nullable, add a nullable feed-batch relationship, and enforce that exactly one parent is present. Keep daily JSON and query behavior unchanged; feed items have no day and are excluded naturally by existing joins against recommendation days.

Add a feed service that returns the current batch without mutation and creates a new batch transactionally. The service uses the existing v3 score and diversity logic, first choosing candidates allowed by the normal recurrence cooldown. If fewer than ten are available, it progressively admits previously exposed unread candidates ordered by oldest exposure and lowest exposure count, preferring candidates outside the current batch. It never relaxes hard filters or duplicates a candidate inside a batch. Persist the batch, items, and exposure updates together.

Expose authenticated GET and POST endpoints at `/api/recommendations/discovery-feed` and `/api/recommendations/discovery-feed/refresh`. GET returns an empty snapshot if no batch exists. POST returns HTTP 201 with the new snapshot. Existing item feedback and context routes accept feed item IDs without special frontend behavior.

Extract the daily recommendation card into a reusable component and render it for both today and the unread discovery feed. The unread view loads the current batch, creates the first batch only when none exists, and renders a centered “换一换” button. Fewer than ten results produce an explicit count message. Other candidate states keep the compact list and current API.

## Concrete Steps

From the repository root, edit the migration/model/service/controller files and add focused tests. Run:

    cd api && go test ./...

Then implement the Vue component and view changes and run:

    cd web && npm test
    cd web && npm run build

Build and start the integrated stack from the repository root:

    docker compose -f docker/docker-compose.yml -p dataark up -d --build

Use Chrome DevTools against `http://127.0.0.1:7845/#/recommendations` to inspect DOM, network requests, console output, responsive layout, feedback/context behavior, and before/after batch IDs.

## Validation and Acceptance

Backend tests must prove that the first batch contains at most ten hard-eligible unread candidates; current GET does not change exposure; two refreshes are disjoint when inventory permits; recent items only return after the preferred pool is exhausted; a hard-eligible pool smaller than ten returns its actual size; different users have independent batches; concurrent refresh preserves one current batch; feed item feedback, revert, and provenance work; and daily snapshots, history, and metrics omit feed items.

Frontend tests must prove that unread mode renders the shared daily card actions, first-load generation works, refresh uses POST and replaces the batch, shortage text is accurate, and other statuses still use the candidate list. The production build must pass TypeScript checking.

Browser acceptance requires the unread state to show ten cards or an explicit shortage, identical action labels to today, a successful 201 refresh request, no repeated cards when sufficient inventory exists, working feedback/revert/context calls, no console errors, no failed application requests, and no horizontal overflow at desktop and narrow widths.

Observed results: the real production page created and displayed ten items, each with all eight action buttons, through a 201 refresh; all subsequent feedback-state requests returned 200. The isolated UI run changed all ten titles after “换一换”, displayed four cards plus `当前仅有 4 篇符合推荐条件` for a shortage response, opened the provenance drawer, and reported no console warnings or errors. At 1280px cards used content plus a 236px action column; at 760px cards and actions used one full-width column without horizontal overflow. Screenshots are `/tmp/dataark-discovery-feed.png` and `/tmp/dataark-discovery-feed-cards.png`.

## Idempotence and Recovery

The schema migration is additive and its rollback preserves user data. Batch generation is transactional: a failure leaves the prior current batch intact and records no partial exposure. Retrying GET is read-only; retrying refresh produces at most one current batch because the user’s batch transition is serialized and protected by a unique active-batch index.

The existing unrelated modified and untracked files (`docker/docker-compose.yml`, `go.work.sum`, `makefile`, `.codex`, and `passwd.txt`) are outside this task and must not be edited or committed.

## Artifacts and Notes

Expected feed response shape:

    {
      "batch": {"id": 12, "requestedCount": 10, "actualCount": 10, "status": "active", "createdAt": "..."},
      "items": [{"id": 101, "dayId": null, "feedBatchId": 12, "candidateId": 55, "candidate": {"id": 55, "url": "..."}}]
    }

## Interfaces and Dependencies

Define `RecommendationFeedBatch` and `RecommendationFeedSnapshot` in the recommendation package. Add `GetCurrentDiscoveryFeed(userID uint)` and `RefreshDiscoveryFeed(ctx context.Context, userID uint, limit int)` service functions. The controller uses the authenticated user ID and fixes the public batch size at ten. No new third-party dependency is required.
