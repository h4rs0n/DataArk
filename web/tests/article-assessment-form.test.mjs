import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

import { hasArticleAssessmentInput } from '../src/utils/articleAssessmentForm.mjs'

const component = await readFile(new URL('../src/components/recommendations/ArticleAssessmentWorkflow.vue', import.meta.url), 'utf8')

function blankForm(overrides = {}) {
  return {
    scores: { quality: null, depth: null, evergreen: null },
    reason: '',
    genre: '',
    extractionBad: false,
    unjudgeable: false,
    durationSeconds: 0,
    ...overrides,
  }
}

test('a completely blank assessment has no savable input', () => {
  assert.equal(hasArticleAssessmentInput(blankForm()), false)
  assert.equal(hasArticleAssessmentInput(blankForm({ reason: '   ' })), false)
})

test('every assessment field counts as input when populated', () => {
  for (const form of [
    blankForm({ scores: { quality: 0, depth: null, evergreen: null } }),
    blankForm({ reason: '简短理由' }),
    blankForm({ genre: 'analysis' }),
    blankForm({ extractionBad: true }),
    blankForm({ unjudgeable: true }),
  ]) {
    assert.equal(hasArticleAssessmentInput(form), true)
  }
})

test('previous navigation bypasses saving only for a blank form', () => {
  assert.match(component, /delta < 0 && !hasArticleAssessmentInput\(form\)/)
  assert.match(component, /delta < 0[\s\S]*await loadItem\(item\.value\.position \+ delta\)[\s\S]*if \(!\(await saveCurrent\(\)\)\) return/)
  assert.match(component, /saveAndMove\(1\)/)
})
