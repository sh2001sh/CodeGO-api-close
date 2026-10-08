import type { MarketPrice } from './market-pricing'

export type EstimateUsage = {
  requests: string
  input: string
  output: string
  cacheRead: string
  cacheWrite: string
  units: string
}

export const defaultEstimateUsage: EstimateUsage = {
  requests: '1000',
  input: '1000',
  output: '500',
  cacheRead: '0',
  cacheWrite: '0',
  units: '1',
}

type EstimateResult =
  { kind: 'known'; credits: string; unit: string } | { kind: 'invalid' | 'unknown' | 'dynamic' }

const integer = (value: string) => {
  if (!/^\d{1,13}$/.test(value)) return undefined
  const count = BigInt(value)
  return count <= 1_000_000_000_000n ? count : undefined
}

/** Effective quotes already include the group multiplier; amounts never pass through Number. */
export function estimateMarketCost(
  price: MarketPrice | undefined,
  usage: EstimateUsage,
): EstimateResult {
  const requests = integer(usage.requests)
  if (requests === undefined) return { kind: 'invalid' }
  if (!price) return { kind: 'unknown' }
  if (price.mode === 'expression' || price.mode === 'tiered_expr') return { kind: 'dynamic' }
  const fields =
    price.mode === 'token' || price.mode === 'per_token'
      ? [
          [usage.input, price.input_per_million],
          [usage.output, price.output_per_million],
          [usage.cacheRead, price.cache_read_per_million],
          [usage.cacheWrite, price.cache_write_per_million],
        ]
      : price.mode === 'per_request'
        ? [[price.unit === 'request' ? '1' : usage.units, price.per_unit]]
        : undefined
  if (!fields) return { kind: 'unknown' }
  const terms = []
  for (const [countValue, quote] of fields) {
    const count = integer(countValue)
    if (count === undefined) return { kind: 'invalid' }
    // A missing cache quote is harmless only when no cache tokens were specified.
    if (count === 0n || requests === 0n) continue
    if (!/^\d+(?:\.\d{1,18})?$/.test(quote)) return { kind: 'unknown' }
    const [whole, fraction = ''] = quote.split('.')
    terms.push({ count, amount: BigInt(whole + fraction), precision: fraction.length })
  }
  const tokenMode = price.mode === 'token' || price.mode === 'per_token'
  const precision = Math.max(0, ...terms.map((term) => term.precision))
  const amount =
    requests *
    terms.reduce(
      (sum, term) => sum + term.count * term.amount * 10n ** BigInt(precision - term.precision),
      0n,
    )
  const decimals = precision + (tokenMode ? 6 : 0)
  const denominator = 10n ** BigInt(decimals)
  const fraction = (amount % denominator).toString().padStart(decimals, '0').replace(/0+$/, '')
  return {
    kind: 'known',
    credits: `${amount / denominator}${fraction ? `.${fraction}` : ''}`,
    unit: tokenMode ? 'tokens' : price.unit,
  }
}
