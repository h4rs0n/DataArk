# Condense repeated article-processing tasks in the crawl queue

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds. This document is maintained in accordance with `PLANS.md` in the repository root.

## Purpose / Big Picture

The owner-facing crawl queue currently renders every recent article-body processing job as a separate row. Bulk discovery can fill all fifty visible rows with nearly identical jobs, making the page difficult to scan. After this change, article-processing jobs with the same processing attributes inside a ten-minute window retain their three newest details and replace the remainder with one compact summary row. Queue execution, counts, and the backend response remain unchanged.

## Progress

- [x] (2026-07-20 16:27+08:00) Inspected the queue snapshot contract, live River job distribution, current frontend rendering, polling, styles, and test setup.
- [x] (2026-07-20 16:34+08:00) Added and unit-tested the deterministic frontend grouping helper.
- [x] (2026-07-20 16:36+08:00) Rendered grouped display items and responsive summary styling in the queue tab.
- [x] (2026-07-20 16:37+08:00) Passed both frontend Node test files and the production Vue/Vite build.
- [x] (2026-07-20 16:37+08:00) Rebuilt the Docker Compose application and verified real queue data at desktop and 760-pixel widths with Chrome DevTools.
- [x] (2026-07-20 16:40+08:00) Created the single focused implementation commit without staging unrelated worktree files.

## Surprises & Discoveries

- Observation: the API already scans up to ten thousand River rows for accurate queue counts but returns at most the requested fifty task details.
  Evidence: `api/jobqueue/jobqueue.go` computes counts before appending up to `limit` tasks, while the frontend requests `limit=50`.
- Observation: the live queue is dominated by article processing history.
  Evidence: the database contained 1,026 completed one-attempt jobs and 372 completed two-attempt jobs of kind `discovery_process_candidate`, much more than any other crawl kind.
- Observation: bulk candidate tasks have distinct candidate IDs and naturally different millisecond timestamps even when every processing attribute is identical.
  Evidence: recent jobs 44281 through 44290 shared kind, state, attempt, and content version while their candidate IDs and completion times differed.
- Observation: River task ID order and completion-time order can differ when workers finish concurrently.
  Evidence: the first Docker browser pass correctly retained candidate 6805 as one of the three newest completions, but a lower-ID Blogroll row caused the initial summary insertion point to appear before that retained detail.

## Decision Log

- Decision: group only `discovery_process_candidate`; source fetch, Blogroll scan, and backfill jobs remain individual rows.
  Rationale: the user identified article-body processing as the visual problem and asked to preserve other queue detail.
  Date/Author: 2026-07-20 / Codex
- Decision: a grouping signature consists of status, attempts, content version, and exact compact error text. Task IDs, candidate IDs, and timestamps are not signature fields.
  Rationale: differing execution outcomes must remain visible, while candidate identity is the expected difference in a bulk batch.
  Date/Author: 2026-07-20 / Codex
- Decision: every group must have a maximum timestamp span of ten minutes, including the boundary, so adjacent jobs cannot chain into an hours-long group.
  Rationale: this is the user-selected interpretation of the time window.
  Date/Author: 2026-07-20 / Codex
- Decision: groups of four or more keep the three newest detail rows and replace all remaining rows with a non-expandable summary.
  Rationale: the user selected the “keep three” presentation instead of an expandable aggregate.
  Date/Author: 2026-07-20 / Codex
- Decision: insert the summary immediately after the last retained detail in API order, even if hidden members appeared earlier in that order.
  Rationale: all three promised details must be visually encountered before the row that summarizes the remainder.
  Date/Author: 2026-07-20 / Codex

## Outcomes & Retrospective

The queue tab now condenses only repeated article-processing rows. In the live Docker data, one ten-minute group containing 39 matching completed jobs rendered three candidate details and one summary for the remaining 36, while interleaved Blogroll and source rows stayed visible in their original order. The backend counters remained the raw values returned by the API.

Both frontend test files and the production build passed. The Compose rebuild completed successfully. Chrome DevTools observed repeated 200 responses from the unchanged `limit=50` queue endpoint, no console errors or warnings, and a stable 15-row DOM across polling. The desktop layout showed a subdued dashed summary row, while a 760-pixel viewport used the responsive two-column grid with no horizontal overflow.

## Context and Orientation

`web/src/views/RecommendationsView.vue` owns the task queue tab. Its `crawlQueue.tasks` array contains the latest task details returned by `/api/admin/discovery/crawl-queue?limit=50`, ordered newest first. A task has its River job ID, kind, target type and target ID, content version, status, attempts, timestamps, and optional error. The four summary counters above the list describe the entire backend snapshot and must not be recomputed from the condensed display.

The grouping algorithm will live in `web/src/utils/crawlQueueGrouping.mjs` as a pure ECMAScript module so the existing Node test runner can import it directly. `web/src/utils/crawlQueueGrouping.d.mts` will provide TypeScript declarations to the Vue type checker. The helper will return tagged display items: either an unchanged task or a summary with the hidden count, common processing fields, and window bounds.

## Plan of Work

Implement a deterministic helper that derives the effective task time using the same precedence as the current UI: finished, started, scheduled, then created. It will collect article-processing tasks by signature into buckets whose minimum and maximum timestamps differ by no more than ten minutes. Invalid timestamps remain standalone. For any bucket larger than three, the helper identifies the three newest members, inserts a summary after the last retained detail in API order, and suppresses the hidden members. All ungrouped tasks retain their original relative position.

Change the Vue template to iterate over the computed display items. Detail items use the existing row and labels. Summary items show “当前列表内另有 N 条相同任务已合并”, the common status, attempts, optional content version and error, plus the oldest-to-newest time range. Add a subdued dashed summary style compatible with the existing mobile grid. Do not change the request limit, API model, queue counts, polling, or execution controls.

Add fixture-based Node tests for the threshold, newest-three retention, inclusive ten-minute boundary, window splitting, signature differences, other task kinds, invalid dates, and input immutability. Extend the existing frontend contract test to ensure the queue template renders derived display items and the summary wording.

## Concrete Steps

From `web/`, run `npm test` and expect all Node tests to pass. Run `npm run build` and expect Vue type checking and Vite production compilation to exit successfully. From `docker/`, run `docker compose -p dataark up -d --build` and expect the API container plus database, Meilisearch, and SingleFile services to be running.

Open `http://127.0.0.1:7845` with Chrome DevTools, authenticate as the owner, and open the task queue tab. Verify that a large matching article batch shows three newest candidate details plus one summary, while source, scan, and backfill rows remain unchanged. Inspect DOM, network, and console state at desktop and narrow widths, and save a screenshot as evidence.

## Validation and Acceptance

A set of three matching article tasks renders three rows. A set of four or more matching tasks whose effective timestamps span at most ten minutes renders the three newest details and one summary whose hidden count is the group size minus three. Exactly ten minutes is accepted; any larger span creates a separate bucket. Changes to status, attempts, content version, or error create separate groups. Missing or invalid timestamps and every non-article task remain individual.

The four queue counters retain their backend values, the API remains polled at the existing cadence, and clicking “执行待处理任务” retains its current behavior. The production build has no type errors, Chrome shows no runtime console errors, and the condensed rows remain readable on a narrow viewport.

## Idempotence and Recovery

The transformation is pure and operates only on the current response, so polling can repeat it without state drift. No database or API migration is required. If rendering fails, reverting the computed display list to `crawlQueue.tasks` fully restores the old behavior.

## Artifacts and Notes

The worktree already contains unrelated changes in `docker/docker-compose.yml`, `go.work.sum`, and `makefile`, plus untracked `.codex` and `passwd.txt`. They must not be staged or modified for this work.

Chrome screenshots were saved as `/tmp/dataark-crawl-queue-grouping-desktop.png` and `/tmp/dataark-crawl-queue-grouping-narrow.png`.

## Interfaces and Dependencies

The helper exports `groupCrawlQueueTasks(tasks)`, the ten-minute window constant, and the three-detail threshold. Its output is a discriminated union with `type: 'task'` carrying the original task or `type: 'summary'` carrying `collapsedCount`, common status/attempt/version/error data, and ISO window bounds. It uses no new package dependency.

Revision note (2026-07-20): created this plan after the user fixed the grouping scope, presentation, and ten-minute window behavior; later recorded the concurrent completion-order discovery and final Docker/Chrome evidence.
