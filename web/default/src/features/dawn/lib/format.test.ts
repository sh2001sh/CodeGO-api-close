import assert from 'node:assert/strict'
import { test } from 'node:test'
import { pct } from './format'

test('market percentages preserve sub-one-percent observations and unavailable values', () => {
  assert.equal(pct(0), '0.0')
  assert.equal(pct(0.5), '0.5')
  assert.equal(pct(1), '1.0')
  assert.equal(pct(99.9), '99.9')
  assert.equal(pct(100), '100.0')
  assert.equal(pct(null), '—')
  assert.equal(pct(Number.NaN), '—')
})
