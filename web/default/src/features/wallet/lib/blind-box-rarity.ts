import interfaceI18n from '@/i18n/config'
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
  legendary:
    'border-amber-500/35 bg-gradient-to-br from-amber-500/10 via-orange-500/5 to-transparent',
  epic: 'border-indigo-500/25 bg-gradient-to-br from-indigo-500/8 via-purple-500/4 to-transparent',
  common: 'border-border/70 bg-card/60',
}

export const RARITY_BADGE: Record<
  RewardRarity,
  { label: string; cls: string } | null
> = {
  legendary: {
    label: '稀有',
    cls: 'border-amber-500/30 bg-gradient-to-r from-amber-500/15 to-orange-500/15 text-amber-600 dark:text-amber-400 font-semibold rounded-md',
  },
  epic: {
    label: '精品',
    cls: 'border-indigo-500/25 bg-indigo-500/10 text-indigo-600 dark:text-indigo-400 font-semibold rounded-md',
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
      ? `${tier.min_usd} credits`
      : `${tier.min_usd} - ${tier.max_usd} credits`

  return interfaceI18n.t('{{param0}} 通用额度', { param0: amount })
}

export function groupTiersByRewardType(tiers: BlindBoxTier[]) {
  return {
    credit: tiers.filter((tier) =>
      ['quota', 'claude_quota'].includes(resolveTierRewardType(tier))
    ),
    props: tiers.filter((tier) => resolveTierRewardType(tier) === 'prop'),
  }
}
