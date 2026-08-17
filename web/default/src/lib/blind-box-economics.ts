export interface BlindBoxEconomicsTier {
  name: string
  min_usd: number
  max_usd: number
  probability: number
  reward_type?: string
}

export interface BlindBoxEconomics {
  probability: number
  expectedRewardUSD: number
  payoutRate: number
  immediateProfitProbability: number
  maxRewardUSD: number
}

/** Calculates account return from the balance before and after a simulation. */
export function calculateAccountReturnRate(
  initialBalance: number,
  currentBalance: number
) {
  if (initialBalance <= 0) return 0
  return ((currentBalance - initialBalance) / initialBalance) * 100
}

/** Calculates ordinary-pool economics, including chained free draws but excluding guarantees. */
export function calculateBlindBoxEconomics(
  tiers: BlindBoxEconomicsTier[],
  priceUSD: number
): BlindBoxEconomics {
  let probability = 0
  let expectedQuotaReward = 0
  let extraDrawProbability = 0
  let immediateProfitProbability = 0
  let maxRewardUSD = 0

  for (const tier of tiers) {
    probability += tier.probability
    if (tier.reward_type === 'prop') {
      if (tier.name.trim() === '再来一抽') {
        extraDrawProbability += tier.probability
      }
      continue
    }
    expectedQuotaReward +=
      ((tier.min_usd + tier.max_usd) / 2) * tier.probability
    maxRewardUSD = Math.max(maxRewardUSD, tier.max_usd)
    if (tier.min_usd >= priceUSD && tier.max_usd > priceUSD) {
      immediateProfitProbability += tier.probability
    }
  }

  const expectedRewardUSD =
    extraDrawProbability < 1
      ? expectedQuotaReward / (1 - extraDrawProbability)
      : 0
  return {
    probability,
    expectedRewardUSD,
    payoutRate: priceUSD > 0 ? (expectedRewardUSD / priceUSD) * 100 : 0,
    immediateProfitProbability,
    maxRewardUSD,
  }
}
