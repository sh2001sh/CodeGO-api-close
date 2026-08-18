import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { calculateAccountYieldRate } from './blind-box-economics.ts'

describe('blind box account yield rate', () => {
  test('measures growth from the initial simulation balance', () => {
    assert.ok(
      Math.abs(calculateAccountYieldRate(100, 213.36) - 113.36) < 0.000001
    )
  })

  test('returns zero when no initial balance is available', () => {
    assert.equal(calculateAccountYieldRate(0, 213.36), 0)
  })
})
