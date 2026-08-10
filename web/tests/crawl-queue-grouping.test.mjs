import assert from 'node:assert/strict'
import test from 'node:test'

import {
  CRAWL_TASK_GROUP_WINDOW_MS,
  groupCrawlQueueTasks,
} from '../src/utils/crawlQueueGrouping.mjs'

const baseTime = Date.parse('2026-07-20T08:20:00.000Z')
const crawlTaskKinds = [
  ['discovery_fetch_source', 'source'],
  ['discovery_scan_blogroll', 'site'],
  ['discovery_backfill_site', 'site'],
  ['discovery_process_candidate', 'candidate'],
]

function task(id, overrides = {}) {
  const offsetMs = overrides.offsetMs ?? Number(id) * 1000
  return {
    id: String(id),
    kind: 'discovery_process_candidate',
    targetType: 'candidate',
    targetId: Number(id),
    contentVersion: '0',
    status: 'succeeded',
    attempts: 1,
    finishedAt: new Date(baseTime - offsetMs).toISOString(),
    createdAt: new Date(baseTime - offsetMs - 5000).toISOString(),
    ...overrides,
  }
}

test('three matching article tasks remain individual', () => {
  const input = [task(1), task(2), task(3)]
  const output = groupCrawlQueueTasks(input)
  assert.deepEqual(output.map((item) => item.type), ['task', 'task', 'task'])
  assert.deepEqual(output.map((item) => item.task.id), ['1', '2', '3'])
  assert.deepEqual(input.map((item) => item.id), ['1', '2', '3'])
})

test('larger groups keep the newest detail and summarize the remainder', () => {
  const input = [task(1), task(2), task(3), task(4), task(5), task(6)]
  const output = groupCrawlQueueTasks(input)
  assert.equal(output.length, 2)
  assert.equal(output[0].type, 'task')
  assert.equal(output[0].task.id, '1')
  assert.equal(output[1].type, 'summary')
  assert.equal(output[1].collapsedCount, 5)
  assert.equal(output[1].contentVersion, '0')
})

test('the summary follows the newest retained detail when other job kinds are interleaved', () => {
  const input = [
    task(1, { offsetMs: 0 }),
    task(2, { offsetMs: 4 * 60 * 1000 }),
    task(3, { offsetMs: 6 * 60 * 1000 }),
    task('source', { kind: 'discovery_fetch_source', targetType: 'source', targetId: 9, offsetMs: 1000 }),
    task(4, { offsetMs: 2 * 60 * 1000 }),
    task(5, { offsetMs: 8 * 60 * 1000 }),
  ]
  const output = groupCrawlQueueTasks(input)
  assert.deepEqual(output.map((item) => item.type), ['task', 'summary', 'task'])
  assert.equal(output[0].task.id, '1')
  assert.equal(output[1].collapsedCount, 4)
  assert.equal(output[2].task.id, 'source')
})

test('the ten-minute window is inclusive and prevents chained long groups', () => {
  const inclusive = [
    task(1, { offsetMs: 0 }),
    task(2, { offsetMs: 2 * 60 * 1000 }),
    task(3, { offsetMs: 5 * 60 * 1000 }),
    task(4, { offsetMs: CRAWL_TASK_GROUP_WINDOW_MS }),
  ]
  assert.equal(groupCrawlQueueTasks(inclusive).at(-1).type, 'summary')

  const split = [
    task(1, { offsetMs: 0 }),
    task(2, { offsetMs: 4 * 60 * 1000 }),
    task(3, { offsetMs: 8 * 60 * 1000 }),
    task(4, { offsetMs: 12 * 60 * 1000 }),
  ]
  assert.deepEqual(groupCrawlQueueTasks(split).map((item) => item.type), ['task', 'task', 'task', 'task'])
})

test('processing attributes form separate groups', () => {
  const input = [
    task(1),
    task(2, { status: 'failed' }),
    task(3, { attempts: 2 }),
    task(4, { contentVersion: '1' }),
    task(5, { error: 'timeout' }),
  ]
  assert.deepEqual(groupCrawlQueueTasks(input).map((item) => item.type), ['task', 'task', 'task', 'task', 'task'])
})

test('successful historical backfill tasks use the article task grouping behavior', () => {
  const input = [
    task(1, { kind: 'discovery_backfill_site', targetType: 'site' }),
    task(2, { kind: 'discovery_backfill_site', targetType: 'site' }),
    task(3, { kind: 'discovery_backfill_site', targetType: 'site' }),
    task(4, { kind: 'discovery_backfill_site', targetType: 'site' }),
  ]
  const output = groupCrawlQueueTasks(input)
  assert.deepEqual(output.map((item) => item.type), ['task', 'summary'])
  assert.equal(output[0].task.id, '1')
  assert.equal(output[1].kind, 'discovery_backfill_site')
  assert.equal(output[1].status, 'succeeded')
  assert.equal(output[1].collapsedCount, 3)
})

test('non-failed historical backfill tasks group separately by status', () => {
  const input = [
    ...[1, 2, 3, 4].map((id) => task(id, {
      kind: 'discovery_backfill_site',
      targetType: 'site',
      status: 'pending',
    })),
    ...[5, 6, 7, 8].map((id) => task(id, {
      kind: 'discovery_backfill_site',
      targetType: 'site',
      status: 'running',
    })),
    ...[9, 10, 11, 12].map((id) => task(id, {
      kind: 'discovery_backfill_site',
      targetType: 'site',
    })),
  ]
  const output = groupCrawlQueueTasks(input)
  assert.deepEqual(output.map((item) => item.type), ['task', 'summary', 'task', 'summary', 'task', 'summary'])
  assert.deepEqual(output.map((item) => item.type === 'task' ? item.task.status : item.status), [
    'pending',
    'pending',
    'running',
    'running',
    'succeeded',
    'succeeded',
  ])
  assert.deepEqual(output.filter((item) => item.type === 'summary').map((item) => item.collapsedCount), [3, 3, 3])
})

test('waiting tasks fold for every crawl queue kind', () => {
  for (const [kind, targetType] of crawlTaskKinds) {
    const input = [1, 2, 3, 4].map((id) => task(id, {
      kind,
      targetType,
      status: 'pending',
    }))
    const output = groupCrawlQueueTasks(input)
    assert.deepEqual(output.map((item) => item.type), ['task', 'summary'], kind)
    assert.equal(output[0].task.kind, kind)
    assert.equal(output[1].kind, kind)
    assert.equal(output[1].status, 'pending')
    assert.equal(output[1].collapsedCount, 3)
  }
})

test('new non-failed crawl task kinds use the same grouping rule', () => {
  const input = [1, 2, 3, 4].map((id) => task(id, {
    kind: 'discovery_future_task',
    status: 'running',
  }))
  assert.deepEqual(groupCrawlQueueTasks(input).map((item) => item.type), ['task', 'summary'])
})

test('article processing and historical backfill tasks never share a group', () => {
  const input = [
    ...[1, 2, 3, 4].map((id) => task(id, { status: 'pending' })),
    ...[5, 6, 7, 8].map((id) => task(id, {
      kind: 'discovery_backfill_site',
      targetType: 'site',
      status: 'pending',
    })),
  ]
  const output = groupCrawlQueueTasks(input)
  assert.deepEqual(output.map((item) => item.type), ['task', 'summary', 'task', 'summary'])
  assert.deepEqual(output.map((item) => item.type === 'task' ? item.task.kind : item.kind), [
    'discovery_process_candidate',
    'discovery_process_candidate',
    'discovery_backfill_site',
    'discovery_backfill_site',
  ])
})

test('failed tasks of every crawl queue kind remain individual with their error details', () => {
  for (const [kind, targetType] of crawlTaskKinds) {
    const input = [1, 2, 3, 4].map((id) => task(id, {
      kind,
      targetType,
      status: 'failed',
      error: `${kind} failure ${id}`,
    }))
    const output = groupCrawlQueueTasks(input)
    assert.deepEqual(output.map((item) => item.type), ['task', 'task', 'task', 'task'])
    assert.deepEqual(output.map((item) => item.task.error), input.map((item) => item.error))
  }
})

test('tasks with invalid timestamps stay individual', () => {
  const input = [1, 2, 3, 4].map((id) => task(id, {
    finishedAt: 'not-a-date',
    createdAt: 'also-not-a-date',
  }))
  assert.equal(groupCrawlQueueTasks(input).length, input.length)
  assert.ok(groupCrawlQueueTasks(input).every((item) => item.type === 'task'))
})
