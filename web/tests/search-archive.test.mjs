import assert from 'node:assert/strict'
import test from 'node:test'
import { submitSearchArchive } from '../src/utils/searchArchive.mjs'

const item = { id: 'material_1', link: 'https://example.org/story', archived: false }

test('one click queues without claiming a local archive; repeated clicks share submission', async () => {
  const actions = {}
  let resolve
  let calls = 0
  const submit = () => { calls++; return new Promise((done) => { resolve = done }) }
  const pending = submitSearchArchive(actions, item, submit)
  assert.equal(actions[item.id].status, 'submitting')
  assert.equal(await submitSearchArchive(actions, item, submit), null)
  resolve({ Data: { status: 'pending' } })
  assert.equal(await pending, '已加入归档队列')
  assert.equal(actions[item.id].status, 'queued')
  assert.equal(item.archived, false)
  assert.equal(await submitSearchArchive(actions, item, submit), null)
  assert.equal(calls, 1)
})

test('failed submission is visible and can be retried', async () => {
  const actions = {}
  await submitSearchArchive(actions, item, async () => { throw new Error('服务不可用') })
  assert.deepEqual(actions[item.id], { status: 'failed', error: '服务不可用' })
  assert.equal(await submitSearchArchive(actions, item, async () => ({ Data: { status: 'running' } })), '已加入归档队列')
})

test('existing success differs from enqueue, and invalid responses allow retry', async () => {
  for (const response of [{}, { Data: { status: 'failed', error: '抓取失败' } }]) {
    const actions = {}
    await submitSearchArchive(actions, item, async () => response)
    assert.equal(actions[item.id].status, 'failed')
    assert.ok(actions[item.id].error)
  }
  const actions = {}
  assert.equal(await submitSearchArchive(actions, item, async () => ({ Data: { status: 'success' } })), '已归档，请重新搜索查看本地入口')
  assert.equal(actions[item.id].status, 'success')
})
