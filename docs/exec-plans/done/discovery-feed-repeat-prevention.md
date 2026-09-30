# Prevent repeated discovery-feed recommendations

> 归档说明（2026-09-30 核对）：本文保留实施当时的背景、决策、路径与验证记录，部分内容已被后续变更取代。现行行为与操作入口见 [文档索引](../../README.md)。历史测试输出不代表当前部署状态。

This ExecPlan is a living document. Maintain it according to `PLANS.md` at the repository root.

## Purpose / Big Picture

The “猜你喜欢” panel should keep showing new eligible articles while any new articles remain in the database. Previously, it only inspected the top 100 scored candidates; after those had all been shown, a refill path deliberately repeated recent articles even though thousands of unseen candidates remained. After this change, refreshing the panel searches beyond exhausted candidates, preserves source and topic diversity, and reports a short batch instead of silently repeating articles when genuinely new content is unavailable.

## Progress

- [x] (2026-08-16 00:00+08:00) Investigated the live local database and identified exhausted top-100 retrieval plus a repeat-permitting fallback.
- [x] (2026-08-16 01:15+08:00) Filtered discovery-feed retrieval to unseen or higher-content-version candidates before its fixed ranking limit, removed recent-exposure refill, and retained the normal diversity pass.
- [x] (2026-08-16 01:16+08:00) Added regression coverage for an exhausted top page, genuine inventory exhaustion, and a content-version update.
- [x] (2026-08-16 01:18+08:00) Ran focused and complete Go tests plus the frontend production build.

## Surprises & Discoveries

- Observation: The local database has 11,292 eligible representative candidates and 10,989 are unseen by user 1, but every candidate in the code’s top-100 query has already been exposed.
  Evidence: A read-only PostgreSQL query against the running local Compose database returned `pool_size=100`, `unseen_for_user_1=0`, and `eligible=11292`, `unseen_for_user_1=10989`.
- Observation: The current feed has ten distinct candidate identities but its latest batch records `reexposure_cooldown`; five items share one source and all ten lack topic metadata.
  Evidence: Batch 39’s stored shortage metadata and read-only item/source aggregates.
- Observation: Joining user exposure state makes generic `id` and ordering references ambiguous in SQLite and PostgreSQL.
  Evidence: The first focused test run failed with `ambiguous column name: id`; qualifying all selection predicates and sort columns with `discovery_candidates` corrected the query.

## Decision Log

- Decision: Expand the selection scan through ordered candidates before allowing recent exposures, rather than simply increasing the fixed pool size.
  Rationale: The implemented form performs this expansion in SQL: the exposure predicate applies before `LIMIT`, so the existing bounded result set is filled from unseen rows beyond the formerly exhausted first page. This is more efficient and robust than fetching repeatedly in application code.
  Date/Author: 2026-08-16 / Codex
- Decision: Do not use recent re-exposure as a discovery-feed fallback. Return a short batch after unseen and higher-content-version candidates are exhausted.
  Rationale: “换一换” promises a different set; a short truthful batch is preferable to hidden repetition. The stored batch metadata now explicitly marks a new-inventory shortage.
  Date/Author: 2026-08-16 / Codex
- Decision: Include historical feed-batch items in recommendation history, not only daily digest items.
  Rationale: A higher candidate content version must be recognized as an update after an article was shown in “猜你喜欢”, otherwise the SQL predicate would find it but the recurrence guard would reject it.
  Date/Author: 2026-08-16 / Codex

## Outcomes & Retrospective

The discovery feed now selects unseen candidates from the full eligible relation before applying its 100-row ranking limit. It does not silently repeat recent articles to fill ten slots. A version increase is an explicit exception and appears with `contentUpdated=true`. The separate lack of topic metadata remains observable in existing content and is not fabricated by this change; the regular source/topic/author diversity pass still applies whenever those attributes are available.

## Context and Orientation

`api/recommendation/discovery_feed.go` creates a per-user `RecommendationFeedBatch` for the panel. It calls `selectDailyRecommendationCandidatesV3` in `api/recommendation/selection_v3.go`; this function scores eligible representative `discovery_candidates` and excludes recently exposed candidates using `user_candidate_states`. A candidate is an article discovered by the crawler. An exposure is the persisted fact that a user was shown that article. The existing query applies `LIMIT poolSize` before the exposure check, so unseen rows beyond the first page are invisible. The old `appendDiscoveryFeedFallback` then allows recently exposed rows and does not apply diversity caps.

`api/recommendation/discovery_feed_test.go` uses SQLite and already covers a refresh when 25 new candidates exist. New tests must cover an exhausted first page with unseen candidates later in sort order, and a real inventory shortage where repeats must not fill the batch.

## Plan of Work

Refactor selection retrieval in `api/recommendation/selection_v3.go` by adding a discovery-feed-only option. That option left-joins the per-user exposure row and keeps candidates that have never been exposed, plus candidates whose persisted `content_version` is higher than any prior recommendation item for the user. This predicate is applied before the existing deterministic `LIMIT`, so the database returns unseen rows beyond an exhausted top page without an unbounded application scan. Qualify all candidate columns because the join also has an `id` column.

In `api/recommendation/discovery_feed.go`, use the discovery-feed-only selection option, diversify the returned candidates with the established source/topic/author limits, and remove the recent-exposure fallback entirely. If fewer than the requested number are available, create a shorter batch and store `newInventoryShortage: true`. Keep existing per-batch candidate uniqueness and exposure recording unchanged. Extend `loadRecommendationHistoryV3` to include feed-batch items so a content update is detected consistently.

Add tests that create 100 high-ranked exposed candidates followed by unseen candidates and assert a refresh selects the unseen candidates. Add a shortage test that refreshes after all eligible items are exposed and asserts the second batch is short and has no repeated candidate. Add a content-version test that proves a previously shown candidate can return only after its version rises.

## Concrete Steps

From `/home/harson/sideProj/DataArk`:

1. Edit the recommendation selection and discovery-feed modules described above.
2. Run `cd api && gofmt -w recommendation/selection_v3.go recommendation/discovery_feed.go recommendation/discovery_feed_test.go`.
3. Run `cd api && env GOCACHE=/tmp/dataark-go-cache go test ./recommendation -run 'TestDiscoveryFeed' -count=1` and expect all discovery-feed tests to pass.
4. Run `cd api && env GOCACHE=/tmp/dataark-go-cache go test ./...` and `cd web && npm run build` before handoff.

## Validation and Acceptance

The new deep-inventory test reproduces the former fault: the first 100 high-ranked candidates are already exposed while later candidates are unseen. A refresh returns ten unseen later candidates and does not record `reexposure_cooldown`. A true four-item inventory displays zero items after the same user asks for another batch; it never repeats those four merely to reach ten. A candidate whose content version rises returns once with `contentUpdated=true`. Existing tests continue proving that a current feed is read-only on GET and that separate users have separate feed batches.

## Idempotence and Recovery

The change only changes future selection. It does not migrate or delete recommendation batches, candidates, feedback, or exposures. Re-running tests is safe. If a changed test exposes an incompatibility, revert only the changed recommendation files and this plan; do not alter the existing user-owned worktree changes.

## Artifacts and Notes

No data mutations were used in the investigation. The running local PostgreSQL service was queried read-only. Existing live batches retain historical repeat metadata, while the next refresh is governed by the corrected selection. Verification completed with:

    cd api && env GOCACHE=/tmp/dataark-go-cache go test ./recommendation -run 'TestDiscoveryFeed' -count=1
    ok   DataArk/recommendation

    cd api && env GOCACHE=/tmp/dataark-go-cache go test ./...
    all packages passed

    cd web && npm run build
    transforming... ✓ 1260 modules transformed.

## Interfaces and Dependencies

Keep `selectDailyRecommendationCandidatesV3(ctx, userID, settings, profile, selectionLimit)` and `RefreshDiscoveryFeed(ctx, userID, limit)` as their public package interfaces. The internal `recommendationSelectionOptions.UnseenOrUpdatedOnly` option applies only to discovery-feed refreshes. It retains context cancellation, domain blacklist filtering, user block rules, duplicate-cluster identity, candidate content-version handling, and per-user exposure state. No new dependency or database migration is required.

Plan created on 2026-08-16 because the live feed investigation proved the fixed first-page retrieval and re-exposure fallback were the direct cause of repeated recommendations. Updated on 2026-08-16 after implementation: SQL-side unseen-or-updated filtering was chosen over application pagination, and the repeat fallback was removed to preserve the user-visible “换一换” contract.
