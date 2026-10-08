import { describe, expect, it } from 'vitest'
import type { Schema } from './types'
import { packageComparison, subscriptionPolicy, validSubscription } from './subscription-policy'

describe('subscription policy', () => {
  it('treats missing historical versions as legacy and expiry equality as ineligible', () => {
    expect(subscriptionPolicy({})).toBe('legacy')
    const value = { state: 'active', expires_at: '2026-10-02T00:00:00Z' } as Schema['Subscription']
    expect(validSubscription(value, Date.parse(value.expires_at) - 1)).toBe(true)
    expect(validSubscription(value, Date.parse(value.expires_at))).toBe(false)
    expect(validSubscription({ ...value, state: 'canceled' }, 0)).toBe(false)
    expect(validSubscription({ ...value, converted_at: '2026-10-01T00:00:00Z' }, 0)).toBe(false)
  })

  it('compares exact same-currency provider credits without floating point money', () => {
    const plan = {
      policy_version: 'standard_v2',
      currency: 'cny',
      price_minor: '9007199254740993',
      credits: '927741523238322279',
    } as unknown as Schema['Plan']
    const method = { currency: 'CNY', credits_per_minor: 100 } as Schema['PaymentMethod']
    expect(packageComparison(plan, method)).toEqual({
      wallet: 900719925474099300n,
      extra: 27021597764222979n,
      percent: '3.00%',
    })
    expect(packageComparison(plan, { ...method, currency: 'usd' })).toBeNull()
    expect(packageComparison(plan, { ...method, credits_per_minor: 0 })).toBeNull()
    expect(packageComparison(plan)).toBeNull()
  })
})
