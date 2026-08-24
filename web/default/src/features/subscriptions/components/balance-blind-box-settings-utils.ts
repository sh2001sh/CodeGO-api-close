import { z } from 'zod'
import type { BlindBoxTierSetting } from '@/features/system-settings/types'
import { DEFAULT_BALANCE_BLIND_BOX_TIERS } from './balance-blind-box-settings-data'

const tierSchema = z.object({
  name: z.string().min(1),
  min_usd: z.number().min(0),
  max_usd: z.number().min(0),
  probability: z.number().min(0).max(1),
  reward_type: z.string().optional(),
  wallet_type: z.string().optional(),
})

export function parseBlindBoxTiers(
  value: string
): BlindBoxTierSetting[] | null {
  try {
    const parsed = z.array(tierSchema).safeParse(JSON.parse(value))
    return parsed.success ? parsed.data : null
  } catch {
    return null
  }
}

export function calculateTierProbability(value: string) {
  const tiers = parseBlindBoxTiers(value)
  return tiers?.reduce((sum, tier) => sum + tier.probability, 0) ?? null
}

export function formatBlindBoxTiers(value: string) {
  return JSON.stringify(JSON.parse(value), null, 2)
}

export function normalizeDisplayedTiers(tiers: BlindBoxTierSetting[]) {
  const hasRemovedReward = tiers.some(
    (tier) =>
      tier.reward_type === 'prop' &&
      !['再来一抽', '15 分钟 0.1 倍率卡'].includes(tier.name.trim())
  )
  const exceedsCurrentCap = tiers.some((tier) => tier.max_usd > 500)
  const usesPreviousCommonRange = tiers.some(
    (tier) => tier.name.trim() === '2.50-3.9124 统一额度'
  )
  return hasRemovedReward || exceedsCurrentCap || usesPreviousCommonRange
    ? DEFAULT_BALANCE_BLIND_BOX_TIERS
    : tiers
}
