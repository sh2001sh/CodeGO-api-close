import type { Schema } from '../../lib/types'
import { currentQuality } from './market-quality'
import { compareMarketPrices, effectiveGroupQuote, type MarketPriceBasis } from './market-pricing'

type Group = Schema['ChannelMarketChannelView']

/** Prefer current evidence, then comparable model quotes; missing measurements never become zero. */
export function compareRecommendedGroups(
  a: Group,
  b: Group,
  model = '',
  priceBasis: MarketPriceBasis = 'input',
  now = Date.now(),
) {
  const left = currentQuality(a, now)
  const right = currentQuality(b, now)
  const evidence = (quality: typeof left) => (quality ? (quality.observing ? 1 : 2) : 0)
  const eligibility = evidence(right) - evidence(left)
  if (eligibility) return eligibility
  const reliability = (right?.wilson_success_rate ?? -1) - (left?.wilson_success_rate ?? -1)
  if (reliability) return reliability
  if (model) {
    const price = compareMarketPrices(
      effectiveGroupQuote(a, model)?.price,
      effectiveGroupQuote(b, model)?.price,
      priceBasis,
    )
    if (price) return price
  }
  return a.system_display_name.localeCompare(b.system_display_name) || a.id.localeCompare(b.id)
}
