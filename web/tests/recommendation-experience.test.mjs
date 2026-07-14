import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const view = await readFile(new URL('../src/views/RecommendationsView.vue', import.meta.url), 'utf8')
const feedback = await readFile(new URL('../src/components/recommendations/FeedbackControls.vue', import.meta.url), 'utf8')

test('recommendation experience exposes scoped reversible feedback', () => {
  for (const action of ['valuable', 'not_interested', 'too_repetitive', 'deep_read', 'block_source', 'reduce_topic', 'reduce_style']) {
    assert.match(view + feedback, new RegExp(action))
  }
  assert.match(view, /method: 'DELETE'.*feedback/s)
  assert.match(view, /feedbackByItem/)
})

test('recommendation experience shows digest and discovery audit context', () => {
  for (const contract of ['DigestSummary', 'SiteInsightPanel', '/context', '/graph', '/backfill', '/operations', 'explorationReason', 'shortageReasons']) {
    assert.match(view, new RegExp(contract.replace('/', '\\/')))
  }
})

test('frontend has no destructive daily regeneration interaction', () => {
  assert.doesNotMatch(view, /admin\/recommendations\/generate/)
  assert.match(view, /admin\/recommendations\/supplement/)
})
