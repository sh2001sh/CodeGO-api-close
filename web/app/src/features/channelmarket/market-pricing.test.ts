import { describe, expect, it } from 'vitest'
import {
  compareCredits,
  comparablePrice,
  compareMarketPrices,
  type MarketPrice,
} from './market-pricing'

const tokens = (input: string): MarketPrice => ({
  mode: 'token',
  unit: 'tokens',
  input_per_million: input,
  output_per_million: '2',
  cache_read_per_million: '0.1',
  cache_write_per_million: '1',
  per_unit: '0',
})

describe('effective market quotes', () => {
  it('compares decimal credits exactly, including large values and trailing zeros', () => {
    expect(compareCredits('9007199254740993.0000001', '9007199254740993.0000002')).toBe(-1)
    expect(compareCredits('0.20', '0.2')).toBe(0)
    expect(compareCredits('10', '2')).toBe(1)
  })
  it('sorts by actual price rather than a multiplier and keeps unknown prices last', () => {
    expect(compareMarketPrices(tokens('4'), tokens('2'), 'input')).toBe(1)
    expect(compareMarketPrices(undefined, tokens('2'), 'input')).toBe(1)
    expect(comparablePrice({ ...tokens('1'), mode: 'expression' }, 'input')).toBeUndefined()
  })
  it('does not compare token charges with per-image or per-request charges', () => {
    const image = { ...tokens('0'), mode: 'per_request', unit: 'image', per_unit: '0.3' }
    expect(comparablePrice(image, 'input')).toBeUndefined()
    expect(comparablePrice(tokens('0'), 'request')).toBeUndefined()
    expect(comparablePrice(image, 'request')).toEqual({ value: '0.3', unit: 'image' })
  })
})
