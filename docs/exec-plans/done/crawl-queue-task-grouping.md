# Condense repeated article-processing tasks in the crawl queue

> 归档说明（2026-09-30 核对）：本文保留实施当时的背景、决策、路径与验证记录，部分内容已被后续变更取代。现行行为与操作入口见 [文档索引](../../README.md)。历史测试输出不代表当前部署状态。

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds. This document is maintained in accordance with `PLANS.md` in the repository root.

## Purpose / Big Picture

The owner-facing crawl queue receives many article-body processing jobs in a batch. The first grouping implementation still retained three detail rows for every large group, so the page continued to repeat “文章正文处理” several times for one batch. After this correction, article-processing jobs with the same processing attributes inside a ten-minute window remain individual only when there are at most three; a group of four or more retains only its newest detail and adds one compact summary for the remaining tasks. Queue execution, counts, and the backend response remain unchanged.

## Progress

- [x] (2026-07-20 16:27+08:00) Inspected the queue snapshot contract, live River job distribution, current frontend rendering, polling, styles, and test setup.
- [x] (2026-07-20 16:34+08:00) Added and unit-tested the deterministic frontend grouping helper.
- [x] (2026-07-20 16:36+08:00) Rendered grouped display items and responsive summary styling in the queue tab.
- [x] (2026-07-20 16:37+08:00) Passed both frontend Node test files and the production Vue/Vite build.
- [x] (2026-07-20 16:37+08:00) Rebuilt the Docker Compose application and verified real queue data at desktop and 760-pixel widths with Chrome DevTools.
- [x] (2026-07-20 16:40+08:00) Created the single focused implementation commit without staging unrelated worktree files.
- [x] (2026-07-20 21:03+08:00) Reproduced the reported redundancy against live Docker data and identified the retained-three-details presentation as its cause.
- [x] (2026-07-20 21:03+08:00) Changed large groups to retain one newest detail plus one summary row and updated the grouping and frontend contract tests after the user clarified the desired presentation.
- [x] (2026-07-20 21:08+08:00) Passed frontend tests/build and the full Go suite, rebuilt Docker Compose, and verified the corrected live DOM plus console/network state at desktop and 760-pixel widths in Chrome DevTools.
- [x] (2026-07-20 21:10+08:00) Committed the focused fix without staging unrelated worktree files.

## Surprises & Discoveries

- Observation: the API already scans up to ten thousand River rows for accurate queue counts but returns at most the requested fifty task details.
  Evidence: `api/jobqueue/jobqueue.go` computes counts before appending up to `limit` tasks, while the frontend requests `limit=50`.
- Observation: the live queue is dominated by article processing history.
  Evidence: the database contained 1,026 completed one-attempt jobs and 372 completed two-attempt jobs of kind `discovery_process_candidate`, much more than any other crawl kind.
- Observation: bulk candidate tasks have distinct candidate IDs and naturally different millisecond timestamps even when every processing attribute is identical.
  Evidence: recent jobs 44281 through 44290 shared kind, state, attempt, and content version while their candidate IDs and completion times differed.
- Observation: River task ID order and completion-time order can differ when workers finish concurrently.
  Evidence: the first Docker browser pass correctly retained candidate 6805 as one of the three newest completions, but a lower-ID Blogroll row caused the initial summary insertion point to appear before that retained detail.
- Observation: the ten-minute grouping and signatures were working on the current live data, but the retained detail rows made the result still look unmerged.
  Evidence: Chrome showed pending candidates 6818, 6817, and 6816 followed by “另有 7 条”, and successful candidates 6808, 6807, and 6805 followed by “另有 18 条”. The corresponding database rows had matching status, attempt count, content version, and sub-minute timestamps.

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
- Decision: supersede the retained-three-details presentation; a group of four or more now retains its one newest detail and uses one non-expandable summary for all remaining members.
  Rationale: live verification proved that retaining three rows was the source of the remaining visual redundancy, and the user explicitly selected one detail plus one summary. The threshold still preserves all details when only one to three tasks match.
  Date/Author: 2026-07-20 / Codex
- Decision: insert a large group's summary immediately after its retained newest detail.
  Rationale: the detail and aggregate read as one unit, while unrelated task rows keep their relative order.
  Date/Author: 2026-07-20 / Codex

## Outcomes & Retrospective

The original implementation condensed repeated article-processing rows but retained three details per large batch. Live feedback exposed that this was still visually redundant, so the corrected implementation retains only the newest member of a large group and summarizes all others while preserving individual display for groups of at most three. The backend counters remain the raw values returned by the API.

The updated frontend tests and production build pass, as does the full Go suite. Docker Compose rebuilt successfully and all four services are running, with PostgreSQL healthy. In current live data, ten matching waiting article tasks render candidate 6818 plus “另有 9 条”, and the six matching successful article tasks present in the fifty-row response render candidate 6808 plus “另有 5 条”. Chrome reported repeated HTTP 200 responses from the queue endpoint, no console warnings or errors, and no horizontal overflow at 760 pixels. The correction was committed as `Fix: reduce repeated crawl task details`.

## Context and Orientation

`web/src/views/RecommendationsView.vue` owns the task queue tab. Its `crawlQueue.tasks` array contains the latest task details returned by `/api/admin/discovery/crawl-queue?limit=50`, ordered newest first. A task has its River job ID, kind, target type and target ID, content version, status, attempts, timestamps, and optional error. The four summary counters above the list describe the entire backend snapshot and must not be recomputed from the condensed display.

The grouping algorithm will live in `web/src/utils/crawlQueueGrouping.mjs` as a pure ECMAScript module so the existing Node test runner can import it directly. `web/src/utils/crawlQueueGrouping.d.mts` will provide TypeScript declarations to the Vue type checker. The helper will return tagged display items: either an unchanged task or a summary with the hidden count, common processing fields, and window bounds.

## Plan of Work

Implement a deterministic helper that derives the effective task time using the same precedence as the current UI: finished, started, scheduled, then created. It collects article-processing tasks by signature into buckets whose minimum and maximum timestamps differ by no more than ten minutes. Invalid timestamps remain standalone. For any bucket larger than three, the helper retains the newest member, inserts a summary for the remaining count immediately after it, and suppresses every other member detail. All ungrouped tasks retain their original relative position.

Change the Vue template to iterate over the computed display items. Detail items use the existing row and labels. Summary items show “另有 N 条相同任务已合并”, the common status, attempts, optional content version and error, plus the oldest-to-newest time range. Keep the subdued dashed summary style compatible with the existing mobile grid. Do not change the request limit, API model, queue counts, polling, or execution controls.

Add fixture-based Node tests for the threshold, newest-one retention, inclusive ten-minute boundary, window splitting, signature differences, other task kinds, invalid dates, and input immutability. Extend the existing frontend contract test to ensure the queue template renders derived display items and the summary wording.

## Concrete Steps

From `web/`, run `npm test` and expect all Node tests to pass. Run `npm run build` and expect Vue type checking and Vite production compilation to exit successfully. From `docker/`, run `docker compose -p dataark up -d --build` and expect the API container plus database, Meilisearch, and SingleFile services to be running.

Open `http://127.0.0.1:7845` with Chrome DevTools, authenticate as the owner, and open the task queue tab. Verify that each large matching article batch shows its newest candidate detail followed by exactly one summary for all other members, while source, scan, and backfill rows remain unchanged. Inspect DOM, network, and console state at desktop and narrow widths, and save a screenshot as evidence.

## Validation and Acceptance

A set of three matching article tasks renders three rows. A set of four or more matching tasks whose effective timestamps span at most ten minutes renders exactly one newest detail plus one summary whose count equals the group size minus one. Exactly ten minutes is accepted; any larger span creates a separate bucket. Changes to status, attempts, content version, or error create separate groups. Missing or invalid timestamps and every non-article task remain individual.

The four queue counters retain their backend values, the API remains polled at the existing cadence, and clicking “执行待处理任务” retains its current behavior. The production build has no type errors, Chrome shows no runtime console errors, and the condensed rows remain readable on a narrow viewport.

## Idempotence and Recovery

The transformation is pure and operates only on the current response, so polling can repeat it without state drift. No database or API migration is required. If rendering fails, reverting the computed display list to `crawlQueue.tasks` fully restores the old behavior.

## Artifacts and Notes

The worktree already contains unrelated changes in `docker/docker-compose.yml`, `go.work.sum`, and `makefile`, plus untracked `.codex` and `passwd.txt`. They must not be staged or modified for this work.

The initial Chrome screenshots were saved as `/tmp/dataark-crawl-queue-grouping-desktop.png` and `/tmp/dataark-crawl-queue-grouping-narrow.png`. Corrected one-detail-plus-summary screenshots were saved as `/tmp/dataark-crawl-queue-grouping-fix-desktop.png` and `/tmp/dataark-crawl-queue-grouping-fix-narrow.png`.

## Interfaces and Dependencies

The helper exports `groupCrawlQueueTasks(tasks)`, the ten-minute window constant, the three-task grouping threshold, and the one-detail retention limit. Its output is a discriminated union with `type: 'task'` carrying the original task or `type: 'summary'` carrying `collapsedCount`, common status/attempt/version/error data, and ISO window bounds. It uses no new package dependency.

Revision note (2026-07-20): created this plan after the user fixed the grouping scope, presentation, and ten-minute window behavior; later recorded the concurrent completion-order discovery and initial Docker/Chrome evidence. Updated at 21:03+08:00 after live data showed that retaining three detail rows caused the reported remaining redundancy, then revised the correction to the user-selected one-detail-plus-one-summary presentation. Updated at 21:10+08:00 with final validation and commit status.
