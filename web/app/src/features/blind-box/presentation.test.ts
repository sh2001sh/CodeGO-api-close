import { describe, expect, it } from 'vitest'
import type { Schema } from '../../lib/types'
import {
  boxOperationIDs,
  enabledPools,
  recipientID,
  rewardTotal,
  weightPercentage,
} from './presentation'

const reward = (weight: number | string | bigint): Schema['MarketplaceReward'] => ({
  kind: 'credits',
  title: 'Reward',
  weight,
  amount_micro: 1,
  multiplier_ppm: 1000000,
  duration_seconds: 0,
  plan_id: 0,
})

describe('blind-box presentation boundaries', () => {
  it('preserves int64 precision and visibly distinguishes a tiny chance from zero', () => {
    expect(weightPercentage('9007199254740993', '9007199254740994', 6)).toBe('99.999999%')
    expect(weightPercentage(1, '9223372036854775807')).toBe('<0.01%')
    expect(weightPercentage(0, 10)).toBe('0.00%')
    expect(weightPercentage(3, 10)).toBe('30.00%')
  })
  it('does not render fake probabilities for empty, zero, overflowing or unsafe weights', () => {
    expect(rewardTotal([])).toBeUndefined()
    expect(rewardTotal([reward(0)])).toBeUndefined()
    expect(rewardTotal([reward('9223372036854775807'), reward(1)])).toBeUndefined()
    expect(rewardTotal([reward(Number.MAX_SAFE_INTEGER + 1)])).toBeUndefined()
    expect(weightPercentage(1, 0)).toBeUndefined()
    expect(weightPercentage(-1, 10)).toBeUndefined()
    expect(weightPercentage(11, 10)).toBeUndefined()
    expect(rewardTotal([reward('9007199254740993'), reward(1)])).toBe(9007199254740994n)
  })
  it('does not offer a disabled pool as a purchase option', () => {
    const pool = { id: 1, enabled: false } as Schema['MarketplacePool']
    expect(enabledPools([pool])).toEqual([])
    expect(enabledPools(null)).toEqual([])
    expect(enabledPools([{ ...pool, enabled: true }, pool])).toHaveLength(1)
  })
  it('retains the same retry identity while separating purchases, openings and recipients', () => {
    let sequence = 0
    const ids = boxOperationIDs(() => `operation-${++sequence}`)
    const first = ids.forPayload('purchase:1')
    expect(ids.forPayload('purchase:1')).toBe(first)
    expect(ids.forPayload('purchase:2')).not.toBe(first)
    expect(ids.forPayload('open')).not.toBe(first)
    const recipient = ids.forPayload('gift:3')
    expect(ids.forPayload('gift:3')).toBe(recipient)
    expect(ids.forPayload('gift:4')).not.toBe(recipient)
    ids.complete('purchase:1')
    expect(ids.forPayload('purchase:1')).not.toBe(first)
    expect(ids.forPayload('gift:3')).toBe(recipient)
  })
  it('validates recipient identities exactly without exponential or fractional coercion', () => {
    expect(recipientID(' 9007199254740993 ')).toBe('9007199254740993')
    for (const value of ['0', '-1', '1.5', '1e3', '', '9223372036854775808'])
      expect(recipientID(value)).toBeUndefined()
  })
})
