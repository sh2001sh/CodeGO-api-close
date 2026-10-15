import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import type { PricingModel } from '../types'
import { getDynamicPricingTiers } from './dynamic-price'
import { formatGroupPrice, formatPrice } from './price'

const model: PricingModel = {
  id: 1,
  model_name: 'gpt-5.6-sol',
  quota_type: 0,
  model_ratio: 2.5,
  completion_ratio: 6,
  cache_ratio: 0.1,
  enable_groups: ['official'],
  group_ratio: { official: 0.5 },
  billing_mode: 'tiered_expr',
  billing_expr:
    'len <= 272000 ? tier("0_272k", p * 5 + c * 30 + cr * 0.5 + cc * 6.25) : tier("272k_plus", p * 10 + c * 45 + cr * 1 + cc * 12.5)',
}

describe('public cache prices', () => {
  test('reads tier cache prices even without legacy cache write ratio', () => {
    assert.equal(formatPrice(model, 'create_cache', 'M'), '$3.125')
    assert.equal(formatPrice(model, 'cache', 'M'), '$0.25')
    assert.equal(formatPrice(model, 'create_cache', 'K'), '$0.003125')
  })

  test('applies the selected group multiplier to each price', () => {
    for (const [type, expected] of [
      ['input', '$1'],
      ['output', '$6'],
      ['create_cache', '$1.25'],
      ['cache', '$0.1'],
    ] as const) {
      assert.equal(
        formatGroupPrice(model, 'third-party', type, 'M', false, 1, 1, {
          'third-party': 0.2,
        }),
        expected
      )
    }
  })

  test('retains both cache prices for every tier', () => {
    assert.deepEqual(
      getDynamicPricingTiers(model).map((tier) => [
        tier.cacheCreatePrice,
        tier.cacheReadPrice,
      ]),
      [
        [6.25, 0.5],
        [12.5, 1],
      ]
    )
  })

  test('distinguishes absent ratio from explicit zero and respects free groups', () => {
    const fixed = {
      ...model,
      billing_mode: undefined,
      create_cache_ratio: undefined,
    }
    assert.equal(formatPrice(fixed, 'create_cache', 'M'), '-')
    assert.equal(
      formatPrice({ ...fixed, create_cache_ratio: 0 }, 'create_cache', 'M'),
      '$0'
    )
    assert.equal(
      formatGroupPrice(model, 'free', 'create_cache', 'M', false, 1, 1, {
        free: 0,
      }),
      '$0'
    )
  })

  test('does not invent tier prices when an expression cannot be parsed', () => {
    assert.equal(
      formatPrice({ ...model, billing_expr: 'custom(p)' }, 'cache', 'M'),
      '-'
    )
    assert.equal(formatPrice({ ...model, quota_type: 1 }, 'cache', 'M'), '-')
  })
})
