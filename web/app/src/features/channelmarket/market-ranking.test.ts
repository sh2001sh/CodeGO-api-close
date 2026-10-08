import { describe, expect, it } from 'vitest'
import type { Schema } from '../../lib/types'
import { compareRecommendedGroups } from './market-ranking'

const now = Date.parse('2026-10-07T12:00:00Z')
const group = (id: string, lower: number, observing = false, at = now) =>
  ({
    id,
    system_display_name: id,
    multiplier: 1,
    effective_model_prices: {},
    quality: { calculated_at: new Date(at).toISOString(), observing, wilson_success_rate: lower },
  }) as Schema['ChannelMarketChannelView']

describe('market recommendation ordering', () => {
  it('does not promote a perfect rate with too few observations over established quality', () => {
    expect(
      compareRecommendedGroups(
        group('new', 0.9, true),
        group('established', 0.8),
        '',
        'input',
        now,
      ),
    ).toBeGreaterThan(0)
  })
  it('compares confidence bounds instead of raw perfect rates', () => {
    expect(
      compareRecommendedGroups(group('reliable', 0.95), group('weaker', 0.8), '', 'input', now),
    ).toBeLessThan(0)
  })
  it('places stale quality below fresh evidence without treating unknown success as zero', () => {
    expect(
      compareRecommendedGroups(
        group('stale', 1, false, now - 61 * 60_000),
        group('fresh', 0.5),
        '',
        'input',
        now,
      ),
    ).toBeGreaterThan(0)
  })
  it('uses exact comparable model prices to break equal quality ties', () => {
    const left = group('expensive', 0.9)
    const right = group('affordable', 0.9)
    const quote = (input: string) => ({
      mode: 'per_token',
      unit: 'tokens',
      input_per_million: input,
      output_per_million: '2',
      cache_read_per_million: '0',
      cache_write_per_million: '0',
      per_unit: '0',
    })
    left.effective_model_prices = { 'gpt-4o': quote('9007199254740993.000002') }
    right.effective_model_prices = { 'gpt-4o': quote('9007199254740993.000001') }
    expect(compareRecommendedGroups(left, right, 'gpt-4o', 'input', now)).toBeGreaterThan(0)
  })
})
