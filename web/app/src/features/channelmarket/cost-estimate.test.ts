import { describe, expect, it } from 'vitest'
import { defaultEstimateUsage, estimateMarketCost } from './cost-estimate'
import type { MarketPrice } from './market-pricing'

const price: MarketPrice = {
  mode: 'token',
  unit: 'tokens',
  input_per_million: '2',
  output_per_million: '8',
  cache_read_per_million: '0.2',
  cache_write_per_million: '2.5',
  per_unit: '0',
}

describe('market usage estimate', () => {
  it('uses the effective quote once, with separate uncached, output and cache amounts', () => {
    expect(estimateMarketCost(price, defaultEstimateUsage)).toEqual({
      kind: 'known',
      credits: '6',
      unit: 'tokens',
    })
    expect(
      estimateMarketCost(price, { ...defaultEstimateUsage, cacheRead: '200', cacheWrite: '40' }),
    ).toEqual({ kind: 'known', credits: '6.14', unit: 'tokens' })
  })
  it('retains exact decimal precision and large monetary totals', () => {
    expect(
      estimateMarketCost(
        { ...price, input_per_million: '9007199254740993.0000001' },
        {
          ...defaultEstimateUsage,
          requests: '1',
          input: '1000000',
          output: '0',
        },
      ),
    ).toEqual({ kind: 'known', credits: '9007199254740993.0000001', unit: 'tokens' })
  })
  it('uses the actual billable units for image, audio and request quotes', () => {
    expect(
      estimateMarketCost(
        { ...price, mode: 'per_request', unit: 'image', per_unit: '0.03' },
        {
          ...defaultEstimateUsage,
          units: '2',
        },
      ),
    ).toEqual({ kind: 'known', credits: '60', unit: 'image' })
    expect(
      estimateMarketCost(
        { ...price, mode: 'per_request', unit: 'request', per_unit: '0.03' },
        {
          ...defaultEstimateUsage,
          units: '2',
        },
      ),
    ).toEqual({ kind: 'known', credits: '30', unit: 'request' })
  })
  it('keeps absent and dynamic quotes unknown instead of pretending they cost zero', () => {
    expect(estimateMarketCost(undefined, defaultEstimateUsage)).toEqual({ kind: 'unknown' })
    expect(estimateMarketCost({ ...price, mode: 'expression' }, defaultEstimateUsage)).toEqual({
      kind: 'dynamic',
    })
    expect(estimateMarketCost({ ...price, output_per_million: '' }, defaultEstimateUsage)).toEqual({
      kind: 'unknown',
    })
    expect(
      estimateMarketCost(
        { ...price, cache_read_per_million: '' },
        {
          ...defaultEstimateUsage,
          cacheRead: '1',
        },
      ),
    ).toEqual({ kind: 'unknown' })
  })
  it('rejects negative, fractional, exponential and excessive quantities, but permits zero', () => {
    for (const requests of ['-1', '1.5', '1e3', '', '1000000000001']) {
      expect(estimateMarketCost(price, { ...defaultEstimateUsage, requests })).toEqual({
        kind: 'invalid',
      })
    }
    expect(estimateMarketCost(price, { ...defaultEstimateUsage, requests: '0' })).toEqual({
      kind: 'known',
      credits: '0',
      unit: 'tokens',
    })
  })
})
