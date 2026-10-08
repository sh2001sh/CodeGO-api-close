import { describe, expect, it } from 'vitest'
import { ratioPrice } from './ratio-prices'
import type { Schema } from '../lib/types'

const price: Schema['CatalogPrice'] = {
  model: 'model',
  mode: 'per_token',
  input_per_mtok: '2000000',
  output_per_mtok: '6000000',
  cache_read_per_mtok: '500000',
  cache_write_per_mtok: '2500000',
  per_request: 0,
  rules: {},
}
const difference = (value: string, confidence = true): Schema['RatioSyncDifference'] => ({
  current: null,
  upstreams: { upstream: value },
  confidence: { upstream: confidence },
})

describe('upstream native price conversion', () => {
  it('converts decimal ratios with half-up rounding and preserves exact integers above JS range', () => {
    const result = ratioPrice(
      'model',
      {
        model_ratio: difference('4503599627.370497'),
        completion_ratio: difference('1.5'),
        cache_ratio: difference('0.25'),
        create_cache_ratio: difference('1.25'),
      },
      'upstream',
      price,
    )
    expect(result.input_per_mtok).toBe('9007199254740994')
    expect(result.output_per_mtok).toBe('13510798882111491')
    expect(result.cache_read_per_mtok).toBe('2251799813685249')
    expect(result.cache_write_per_mtok).toBe('11258999068426243')
  })
  it('retains existing multipliers for fields with no reported difference', () => {
    expect(ratioPrice('model', { model_ratio: difference('2') }, 'upstream', price)).toMatchObject({
      input_per_mtok: '4000000',
      output_per_mtok: '12000000',
      cache_read_per_mtok: '1000000',
    })
  })
  it('rejects incomplete, untrusted, overflow and special media pricing rather than inventing prices', () => {
    expect(() => ratioPrice('new', { model_ratio: difference('2') }, 'upstream')).toThrow(
      '价格不完整',
    )
    expect(() =>
      ratioPrice('model', { model_ratio: difference('2', false) }, 'upstream', price),
    ).toThrow('可信度不足')
    expect(() =>
      ratioPrice('model', { model_ratio: difference('9223372036854775807') }, 'upstream', price),
    ).toThrow('超出允许范围')
    expect(() => ratioPrice('model', { image_ratio: difference('3') }, 'upstream', price)).toThrow(
      '特殊计费规则',
    )
  })
})
