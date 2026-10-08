import type { PublicCatalogModel } from '../../lib/public-catalog'
import type { Schema } from '../../lib/types'

export type MarketPrice = NonNullable<PublicCatalogModel['groups'][number]['price']>
export type MarketPriceBasis = 'input' | 'output' | 'cache-read' | 'cache-write' | 'request'

/** Compare the decimal credits returned by billing without rounding through Number. */
export function compareCredits(a: string, b: string): number {
  const decimal = (value: string) => {
    if (!/^\d+(?:\.\d+)?$/.test(value)) return null
    const [whole, fraction = ''] = value.split('.')
    return { whole: BigInt(whole), fraction }
  }
  const left = decimal(a)
  const right = decimal(b)
  if (!left || !right) return 0
  if (left.whole !== right.whole) return left.whole < right.whole ? -1 : 1
  const width = Math.max(left.fraction.length, right.fraction.length)
  const l = left.fraction.padEnd(width, '0')
  const r = right.fraction.padEnd(width, '0')
  return l < r ? -1 : l > r ? 1 : 0
}

/** Group-authorized quotes cover private listings as well as the public catalog. */
export function effectiveGroupQuote(
  group: Schema['ChannelMarketChannelView'],
  model: string,
): PublicCatalogModel['groups'][number] | undefined {
  const price = group.effective_model_prices?.[model]
  return price
    ? {
        slug: group.public_slug,
        name: group.system_display_name,
        multiplier: String(group.multiplier),
        verified: group.verification_status === 'passed',
        price,
      }
    : undefined
}

export function comparablePrice(
  price: MarketPrice | undefined,
  basis: MarketPriceBasis,
): { value: string; unit: string } | undefined {
  if (!price) return undefined
  if (price.mode === 'per_request')
    return basis === 'request' && /^\d+(?:\.\d+)?$/.test(price.per_unit)
      ? { value: price.per_unit, unit: price.unit }
      : undefined
  if (price.mode !== 'token' && price.mode !== 'per_token') return undefined
  const field = {
    input: price.input_per_million,
    output: price.output_per_million,
    'cache-read': price.cache_read_per_million,
    'cache-write': price.cache_write_per_million,
    request: '',
  }[basis]
  return /^\d+(?:\.\d+)?$/.test(field) ? { value: field, unit: 'million_tokens' } : undefined
}

/** Unquoted/dynamic prices follow known quotes; unlike units never imply cheaper usage. */
export function compareMarketPrices(
  a: MarketPrice | undefined,
  b: MarketPrice | undefined,
  basis: MarketPriceBasis,
): number {
  const left = comparablePrice(a, basis)
  const right = comparablePrice(b, basis)
  if (!left || !right) return left ? -1 : right ? 1 : 0
  return left.unit.localeCompare(right.unit) || compareCredits(left.value, right.value)
}
