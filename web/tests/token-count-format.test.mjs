import assert from 'node:assert/strict'
import test from 'node:test'
import { formatTokenCount } from '../src/components/recommendations/format.ts'

test('token counts use abbreviations only strictly above each threshold', () => {
  const cases = [
    [0, '0'],
    [999, '999'],
    [1000, '1000'],
    [1001, '1.00K'],
    [1500, '1.50K'],
    [999999, '1000.00K'],
    [1000000, '1000.00K'],
    [1000001, '1.00M'],
    [1250000, '1.25M'],
    [999999999, '1000.00M'],
    [1000000000, '1000.00M'],
    [1000000001, '1.00B'],
    [1250000000, '1.25B'],
    [1234567890123, '1234.57B'],
  ]
  for (const [value, expected] of cases) {
    assert.equal(formatTokenCount(value), expected, String(value))
  }
})

test('abbreviated token counts round to exactly two decimal places', () => {
  assert.equal(formatTokenCount(1234), '1.23K')
  assert.equal(formatTokenCount(1236), '1.24K')
  assert.equal(formatTokenCount(1234000), '1.23M')
  assert.equal(formatTokenCount(1236000), '1.24M')
  assert.equal(formatTokenCount(1234000000), '1.23B')
  assert.equal(formatTokenCount(1236000000), '1.24B')
})

test('prompt and completion choose units independently and retain full source counts', () => {
  const totals = Object.freeze({ total: 1250001500, prompt: 1250000000, completion: 1500 })
  assert.equal(formatTokenCount(totals.total), '1.25B')
  assert.equal(`${formatTokenCount(totals.prompt)} / ${formatTokenCount(totals.completion)}`, '1.25B / 1.50K')
  assert.equal(totals.total, 1250001500)
  assert.equal(totals.prompt, 1250000000)
  assert.equal(totals.completion, 1500)
})
