import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import type { PricingModel } from '../types'
import { mergePricingModels } from './merge-pricing-models.ts'

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
})
