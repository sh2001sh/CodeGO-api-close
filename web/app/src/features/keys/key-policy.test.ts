import { describe, expect, it } from 'vitest'
import type { APIKey } from '../../lib/types'
import { keyInput, keyStatusInput, restrictionMode, scaledDecimal } from './key-policy'

function fields(changes: Record<string, string> = {}) {
  const data = new FormData()
  for (const [key, value] of Object.entries({
    name: 'Production',
    status: 'active',
    group: '',
    models_mode: 'all',
    cidrs_mode: 'all',
    max_multiplier: '1',
    ...changes,
  }))
    data.set(key, value)
  return data
}

const existing: APIKey = {
  id: '9223372036854775807',
  name: 'Production',
  status: 'active',
  group: 'pool:user:1',
  allowed_models: [],
  allowed_cidrs: [],
  expires_at: null,
  budget_limited: true,
  budget_micro_credits: '1234567890123456789',
  cross_group_retry: false,
  max_marketplace_multiplier_ppm: '1000000',
  key_prefix: 'sk-test',
  created_at: '2026-10-15T00:00:00Z',
  last_used_at: null,
  budget_account_id: '9',
  user_id: '1',
  spent_micro_credits: '0',
}

describe('API key policy writes', () => {
  it('sends the routing group for an owned pool with exact allowance and multiplier values', () => {
    const input = keyInput(
      fields({
        group: 'pool:user:1',
        budget_limited: 'on',
        budget: '1234567890123.456789',
        max_multiplier: '1.234567',
        cross_group_retry: 'on',
        models_mode: 'selected',
        models: 'gpt-5, claude-sonnet-4, gpt-5',
        cidrs_mode: 'selected',
        cidrs: '192.0.2.0/24\n2001:db8::/32',
        expires_at: '2026-11-15T10:11:12',
      }),
    )
    expect(input).toMatchObject({
      group: 'pool:user:1',
      budget_limited: true,
      budget_micro_credits: '1234567890123456789',
      max_marketplace_multiplier_ppm: '1234567',
      cross_group_retry: true,
      allowed_models: ['gpt-5', 'claude-sonnet-4'],
      allowed_cidrs: ['192.0.2.0/24', '2001:db8::/32'],
    })
    expect(input.expires_at).toBe(new Date('2026-11-15T10:11:12').toISOString())
  })

  it('distinguishes unrestricted, denied and explicit lists and clears expiry', () => {
    expect(keyInput(fields())).toMatchObject({
      allowed_models: null,
      allowed_cidrs: null,
      group: null,
      expires_at: null,
    })
    expect(keyInput(fields({ models_mode: 'none', cidrs_mode: 'none' }), existing)).toMatchObject({
      allowed_models: [],
      allowed_cidrs: [],
    })
    expect(restrictionMode(null)).toBe('all')
    expect(restrictionMode([])).toBe('none')
    expect(restrictionMode(['gpt-5'])).toBe('selected')
    expect(() => keyInput(fields({ models_mode: 'selected', models: ' , ' }))).toThrow(
      '请检查访问限制',
    )
  })

  it('never restores stale allowance when renaming or toggling status', () => {
    const input = keyInput(
      fields({ name: 'Renamed', budget_limited: 'on', budget: '1234567890123.456789' }),
      existing,
    )
    expect(input).not.toHaveProperty('budget_micro_credits')
    expect(keyStatusInput(existing, 'disabled')).toEqual({ id: existing.id, status: 'disabled' })
    const disabled = keyInput(fields(), existing)
    expect(disabled.budget_limited).toBe(false)
    expect(disabled).not.toHaveProperty('budget_micro_credits')
    const exhausted = { ...existing, budget_micro_credits: '-1' }
    expect(
      keyInput(fields({ budget_limited: 'on', budget: '-0.000001' }), exhausted),
    ).not.toHaveProperty('budget_micro_credits')
  })

  it('accepts a deliberate zero allowance and the int64 boundary without rounding', () => {
    expect(keyInput(fields({ budget_limited: 'on', budget: '0' })).budget_micro_credits).toBe('0')
    expect(
      keyInput(fields({ budget_limited: 'on', budget: '9223372036854.775807' }))
        .budget_micro_credits,
    ).toBe('9223372036854775807')
    expect(scaledDecimal('9223372036854775807')).toBe('9223372036854.775807')
    expect(scaledDecimal(1)).toBe('0.000001')
    expect(keyInput(fields({ multiplier_unlimited: 'on' })).max_marketplace_multiplier_ppm).toBe(
      '0',
    )
  })

  it('rejects overflow, precision loss, negative budgets, invalid times and zero finite multiplier', () => {
    for (const budget of ['9223372036854.775808', '-1', '1.0000001', '1e6', '', 'NaN'])
      expect(() => keyInput(fields({ budget_limited: 'on', budget })), budget).toThrow()
    expect(() => keyInput(fields({ expires_at: 'invalid' }))).toThrow('到期时间无效')
    expect(() => keyInput(fields({ max_multiplier: '0' }))).toThrow('倍率必须大于零')
  })
})
