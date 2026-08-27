# Keep one article per source in each recommendation batch

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds. Maintain this document in accordance with `PLANS.md` at the repository root.

## Purpose / Big Picture

A user opening “猜你喜欢” or “今日推荐” should see a mixed set of blogs first. After this change, each generated batch and each daily digest prefers at most one article per displayed source. If unique sources are fewer than the requested count, leftover slots are filled with additional articles from sources already chosen, preferring unused sources first.

## Progress

- [x] (2026-08-18 22:50+08:00) Confirmed both panels share `diversifyRecommendationCandidatesV3` in `api/recommendation/selection_v3.go`, and the old cap was thirty percent of the limit with a `source_limit` relaxation.
- [x] (2026-08-18 22:54+08:00) Changed that diversify pass so source uniqueness is a hard rule of one article, and only relax author or topic when a remaining unique-source candidate is actually blocked by that cap.
- [x] (2026-08-18 22:54+08:00) Prevented daily supplements from appending a second article from a source already in that day’s digest.
- [x] (2026-08-18 22:56+08:00) Updated fixtures and added regressions: unit diversify, discovery-feed unique sources, same-source supplement skip, and daily one-per-source.
- [x] (2026-08-18 22:58+08:00) Ran `cd api && go test ./recommendation -count=1` and `cd api && go test ./...`. Both passed.
- [x] (2026-08-18 23:08+08:00) Relaxed source uniqueness only after unique sources cannot fill the requested count; supplement prefers unused sources then repeats a source to fill.
- [x] (2026-08-18 23:08+08:00) Re-ran recommendation tests after the fill-on-shortage change.
- [x] (2026-08-19 00:05+08:00) Diagnosed live “Security Lab” flooding: the quality-ranked pool of 100 was filled by one source, so source_limit relaxed even though other sources existed later in rank order.
- [x] (2026-08-19 00:08+08:00) Changed pool collection to keep one article per source first, and only append extra same-source articles after unique sources in the scan are exhausted. Added discovery-feed regressions.

## Surprises & Discoveries

- Observation: “猜你喜欢” (`RefreshDiscoveryFeed` in `api/recommendation/discovery_feed.go`) and “今日推荐” (`generateDailyRecommendationsWithOptions` in `api/recommendation/service.go`) both call `diversifyRecommendationCandidatesV3`. Changing that function covers both panels.
  Evidence: call sites at `discovery_feed.go` around the diversify invocation and `service.go` around daily publish and supplement.
- Observation: Source uniqueness was a soft cap. For a ten-item batch the code allowed `ceil(10 * 0.3) = 3` articles per source, then relaxed `source_limit` when it could not fill the quota.
  Evidence: `maxSource` and the `relaxSource` branch in `diversifyRecommendationCandidatesV3`.
- Observation: Daily supplement only skipped duplicate candidate identities, so a later article from the same source could still be appended.
  Evidence: `appendRecommendationSupplement` matches `candidate_id` / `dedupe_key` only.
- Observation: A high-volume source such as “Security Lab” can occupy the entire quality-ranked pool of 100 unseen eligible articles. Diversify then sees too few unique sources in that truncated pool and relaxes `source_limit`, filling the batch with that one source even when thousands of other sources exist later in rank order.
  Evidence: `collectHardEligibleSelectionPool` previously appended every hard-eligible row until `poolSize`. A fixture of 100 high-scoring “Security Lab” articles plus 12 lower-scoring others reproduced a 10-item Security Lab batch before the pool change.

## Decision Log

- Decision: Identify a source by the same string the card shows: `SourceName`, falling back to the article URL host.
  Rationale: The Vue cards render `candidate.sourceName || sourceHost(url)`. Matching that identity is what the user means by “一个来源”. Do not key only on `SourceID`, because many tests and some real paths reuse a placeholder source id while still showing distinct names; do not key only on registrable domain, because several independent blogs can share a host such as Medium.
  Date/Author: 2026-08-18 / Cursor Grok 4.6
- Decision: Never relax the one-article-per-source rule to fill the requested count. Author and topic caps remain soft.
  Rationale: The user asked to ensure the property. A truthful shorter list is better than a second card from the same blog.
  Date/Author: 2026-08-18 / Cursor Grok 4.6
- Decision: Daily supplement must honor sources already published that day.
  Rationale: “今日推荐” is one recommendation, including later appends. Filling a shortage with another article from a source already shown would violate the product rule.
  Date/Author: 2026-08-18 / Cursor Grok 4.6
- Decision: Record author or topic relaxation only when a leftover unique-source candidate is actually blocked by that cap.
  Rationale: Otherwise a source-only shortage pollutes the stored audit with `author_limit` and `topic_limit`, which would hide the real reason the batch is short.
  Date/Author: 2026-08-18 / Cursor Grok 4.6
- Decision: Prefer one article per source, then relax `source_limit` only after unique sources cannot fill the requested count. Daily supplement seeds already-used sources so unused sources still win first.
  Rationale: The user later asked to fill the list when sources are insufficient, rather than leaving it short. Repeating a source is a last resort, not the default.
  Date/Author: 2026-08-18 / Cursor Grok 4.6
- Decision: Collect the selection pool with at most one article per displayed source first, and only use extra same-source articles to pad the pool when the scan cannot find enough unique sources.
  Rationale: “来源不够填满 10 条” must mean the eligible inventory, not the first 100 rows of a single prolific feed. Otherwise a source like Security Lab floods “猜你喜欢”.
  Date/Author: 2026-08-19 / Cursor Grok 4.6

## Outcomes & Retrospective

Both “猜你喜欢” and “今日推荐” prefer one article per displayed source. The candidate pool itself is collected with one article per source first, so a prolific feed cannot hide other sources behind the ranking limit. When unique sources in the eligible inventory can fill the requested count, no source is repeated. When they cannot, leftover slots are filled from remaining articles. Daily supplement uses the same order. Backend tests passed.

What remains: rebuild and restart the API so the running container picks up the pool change, then press “换一批”. Existing batches stay as stored.

## Context and Orientation

DataArk recommends discovered articles in two user-visible lists. “猜你喜欢” is a per-user `RecommendationFeedBatch` created by `RefreshDiscoveryFeed`. “今日推荐” is an immutable `RecommendationDay` created by `GenerateDailyRecommendations` and optionally extended by `SupplementDailyRecommendations`.

A candidate is a row in `discovery_candidates`. Its displayed source is `source_name`, copied onto `recommendation_items.snapshot_source` when a card is published. Selection scores eligible candidates in `selectRecommendationCandidatesV3`, then `diversifyRecommendationCandidatesV3` picks a bounded set while applying exploration, topic, author, and source caps.

The card UI lives in `web/src/components/recommendations/RecommendationArticleCard.vue` and does not choose which articles appear. This change is backend-only.

## Plan of Work

Add `recommendationSourceKey` in `api/recommendation/selection_v3.go`. It lowercases `firstNonEmpty(SourceName, host)` and, if both are empty, uses `candidate:<id>` so blank labels do not collapse unrelated articles.

In `diversifyRecommendationCandidatesV3`, replace the thirty-percent `maxSource` with a hard count of one. Remove the `relaxSource` / `source_limit` branch so the loop stops when only duplicate sources remain. Keep author and topic relaxations, but only record them when a leftover unique-source candidate is blocked by that cap. Apply the same hard cap in the legacy `diversifyRecommendationCandidates` in `api/recommendation/service.go`, including its leftover fill loop, so the unused legacy path cannot reintroduce duplicates.

In `SupplementDailyRecommendationsWithReranker`, drop scored candidates whose source key is already present on that day’s items before calling diversify.

Update tests whose fixtures put many articles on one host or one source name. Give those articles distinct displayed sources when the test needs a full-size batch. Change `TestRecommendationV3SoftRelaxationIsAuditedButHardIdentityIsNot` so same-source inventory yields one item and does not record `source_limit` as a soft relaxation. Add a focused diversify test that keeps the highest-scoring article from a repeated source.

## Concrete Steps

From `/home/harson/sideProj/DataArk`:

1. Edit `api/recommendation/selection_v3.go` and `api/recommendation/service.go` as described above.
2. Edit recommendation tests listed in Plan of Work.
3. Run `gofmt` on the touched Go files.
4. Run `cd api && go test ./recommendation -count=1`. Expect a pass, including the new one-per-source test.
5. Run `cd api && go test ./...`.

These steps were completed on 2026-08-18.

## Validation and Acceptance

A unit-level diversify call with three Alpha articles and one Beta article, requesting two items, returns the highest Alpha and Beta. `GenerateDailyRecommendations` for a user with five eligible articles from one source and a daily limit of five publishes one item. `RefreshDiscoveryFeed` with twenty-five articles across twenty-five source names returns ten items with ten distinct sources. Supplement after a one-item digest does not append a second article whose `SourceName` matches the first item.

## Idempotence and Recovery

The change only affects future selection. Existing published days and feed batches stay as stored. Re-running tests is safe. Rolling back is reverting the touched recommendation Go files and this plan.

## Artifacts and Notes

    cd api && go test ./recommendation -count=1
    ok   DataArk/recommendation	1.358s

    cd api && go test ./...
    ok   DataArk/recommendation	1.516s
    all packages passed

## Interfaces and Dependencies

In `api/recommendation/selection_v3.go` keep:

    func diversifyRecommendationCandidatesV3(candidates []recommendationCandidateScore, limit int, explorationRate float64) ([]recommendationCandidateScore, []string)

Add an unexported helper:

    func recommendationSourceKey(candidate DiscoveryCandidate, host string) string

Daily supplement seeds already-used source counts into `diversifyRecommendationCandidatesV3WithReserved`, so unused sources still fill first and a repeated source is only used after that. Pool collection in `collectHardEligibleSelectionPool` now prefers unique sources. Newly published days store policy `v3-selection-4`. New feed batches store `discovery-feed-v4`. No API, migration, or frontend change.

Revision 2026-08-18: implementation completed. After the first recommendation-package run, a unit test requesting ten items recorded spurious author and topic relaxations, and a shared-database soft-relaxation case under-filled. Diversify now relaxes those caps only when they actually block a unique-source leftover, the unit test requests two items, and author/topic relaxation lives in its own SQLite world.

Revision 2026-08-18 later: the user asked to fill the requested count when unique sources are insufficient. Source uniqueness is again a last-resort soft cap (`source_limit`) after author and topic, not a hard stop.

Revision 2026-08-19: Security Lab still dominated “猜你喜欢” because the quality pool was source-homogeneous. Pool collection now skips extra articles from a source already represented until unique sources are exhausted.
