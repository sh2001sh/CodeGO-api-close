import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import type { PricingModel } from '../types'
import {
  availablePricingModels,
  mergePricingModels,
} from './merge-pricing-models.ts'

function model(
  name: string,
  overrides: Partial<PricingModel> = {}
): PricingModel {
  return {
    id: 1,
    model_name: name,
    quota_type: 0,
    model_ratio: 1,
    completion_ratio: 1,
    enable_groups: [],
    ...overrides,
  }
}

describe('mergePricingModels', () => {
  test('keeps marketplace-only priced models', () => {
    const result = mergePricingModels(
      [model('catalog-model')],
      [model('marketplace-model')]
    )

    assert.deepEqual(
      result.map((item) => item.model_name),
      ['catalog-model', 'marketplace-model']
    )
  })

  test('matches normalized names and lets billing data override pricing', () => {
    const result = mergePricingModels(
      [model(' GPT-5 ', { description: 'catalog metadata', model_ratio: 1 })],
      [model('gpt-5', { model_ratio: 2 })]
    )

    assert.equal(result.length, 1)
    assert.equal(result[0].description, 'catalog metadata')
    assert.equal(result[0].model_ratio, 2)
  })

  test('hides configured prices without a usable group and keeps third-party-only models', () => {
    const result = availablePricingModels(
      [model('official', { enable_groups: ['vip'] })],
      [model('official'), model('third-party'), model('price-only')],
      ['official', 'THIRD-PARTY']
    )
    assert.deepEqual(
      result.map((item) => item.model_name),
      ['official', 'third-party']
    )
    assert.deepEqual(result[0].enable_groups, ['vip'])
  })

  test('an empty availability list does not fall back to billing configuration', () => {
    assert.deepEqual(
      availablePricingModels([model('stale')], [model('priced')], []),
      []
    )
  })

  test('older API responses only expose catalog entries with groups', () => {
    const result = availablePricingModels(
      [model('available', { enable_groups: ['default'] }), model('no-group')],
      [model('price-only')]
    )
    assert.deepEqual(
      result.map((item) => item.model_name),
      ['available']
    )
  })
})
