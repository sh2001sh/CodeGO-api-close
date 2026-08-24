import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import type { MarketplaceGroup } from '@/features/marketplace/types'
import type { PricingModel } from '../types'
import { buildThirdPartyPricingModels } from './third-party-models'

const group = (modelName: string): MarketplaceGroup =>
  ({
    id: 'group-1',
    system_display_name: '23-CC-Max-1x',
    source_label: 'CC-Max',
    multiplier: 1,
    models: [modelName],
    success_rate: 99,
    wilson_success_rate: 98,
    observing: false,
  }) as MarketplaceGroup

const detail = (modelName: string): PricingModel => ({
  id: -1,
  model_name: modelName,
  vendor_id: 2,
  vendor_name: 'Anthropic',
  quota_type: 0,
  model_ratio: 0.5,
  completion_ratio: 5,
  cache_ratio: 0.1,
  create_cache_ratio: 1.25,
  enable_groups: [],
  supported_endpoint_types: ['anthropic', 'openai'],
})

describe('third-party pricing models', () => {
  test('uses complete site billing details for marketplace-only models', () => {
    const models = buildThirdPartyPricingModels(
      [group('claude-fable-5')],
      [],
      [detail('claude-fable-5')]
    )

    assert.equal(models[0]?.pricing_available, true)
    assert.equal(models[0]?.model_ratio, 0.5)
    assert.equal(models[0]?.completion_ratio, 5)
    assert.equal(models[0]?.cache_ratio, 0.1)
    assert.equal(models[0]?.create_cache_ratio, 1.25)
    assert.equal(models[0]?.vendor_name, 'Anthropic')
    assert.deepEqual(models[0]?.enable_groups, ['23-CC-Max-1x'])
    assert.deepEqual(models[0]?.group_ratio, { '23-CC-Max-1x': 1 })
  })

  test('matches billing details case-insensitively', () => {
    const models = buildThirdPartyPricingModels(
      [group('Claude-Fable-5')],
      [],
      [detail('claude-fable-5')]
    )

    assert.equal(models[0]?.pricing_available, true)
    assert.equal(models[0]?.model_name, 'Claude-Fable-5')
    assert.equal(models[0]?.model_ratio, 0.5)
  })

  test('keeps models without complete details in the pending state', () => {
    const models = buildThirdPartyPricingModels(
      [group('unconfigured-model')],
      [],
      []
    )

    assert.equal(models[0]?.pricing_available, false)
    assert.equal(models[0]?.model_ratio, 0)
  })
})
