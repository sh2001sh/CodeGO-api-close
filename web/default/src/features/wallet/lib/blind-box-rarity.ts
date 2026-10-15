/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import type { BlindBoxTier } from '../types'

export type RewardRarity = 'legendary' | 'epic' | 'common'

export const RARITY_RING: Record<RewardRarity, string> = {
  legendary: 'border-primary/45 bg-primary/[0.07]',
  epic: 'border-primary/25 bg-primary/[0.03]',
  common: 'border-border/70 bg-background/72',
}

export const RARITY_BADGE: Record<
  RewardRarity,
  { label: string; cls: string } | null
> = {
  legendary: {
    label: '稀有',
    cls: 'border-primary/50 bg-primary text-primary-foreground',
  },
  epic: {
    label: '精品',
    cls: 'border-primary/40 bg-primary/10 text-primary',
  },
  common: null,
}

export function resolveTierRewardType(tier: BlindBoxTier) {
  if (tier.reward_type) return tier.reward_type
  if (tier.min_usd === 0 && tier.max_usd === 0) return 'prop'
  if (
    tier.wallet_type === 'claude' ||
    tier.name.toLowerCase().includes('claude')
  ) {
    return 'claude_quota'
  }
  return 'quota'
}

/** Mirrors classifyReward for pool entries so pre-purchase and post-open visuals match. */
export function classifyTier(tier: BlindBoxTier): RewardRarity {
  const rewardType = resolveTierRewardType(tier)
  if (rewardType === 'subscription') return 'legendary'
  if (rewardType === 'claude_quota' || rewardType === 'quota') {
    return tier.max_usd >= 2 ? 'epic' : 'common'
  }
  return 'common'
}

export function formatTierAmount(tier: BlindBoxTier) {
  const rewardType = resolveTierRewardType(tier)
  if (rewardType === 'prop' || rewardType === 'subscription') return tier.name

  const amount =
    tier.min_usd === tier.max_usd
      ? `$${tier.min_usd}`
      : `$${tier.min_usd} - $${tier.max_usd}`

  return `${amount} 通用额度`
}

export function groupTiersByRewardType(tiers: BlindBoxTier[]) {
  return {
    credit: tiers.filter((tier) =>
      ['quota', 'claude_quota'].includes(resolveTierRewardType(tier))
    ),
    props: tiers.filter((tier) => resolveTierRewardType(tier) === 'prop'),
  }
}
