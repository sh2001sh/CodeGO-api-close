import interfaceI18n from '@/i18n/config'

export type ReelItem = {
  key: string
  label: string
  tag?: string
  strong?: boolean
}

/** Convert pool tiers into reel cells. */
export function tiersToReelItems(
  tiers: Array<{
    name?: string
    min_usd?: number
    max_usd?: number
    probability?: number
    reward_type?: string
  }>
): ReelItem[] {
  return tiers.map((tier, index) => {
    const probability = Number(tier.probability || 0)
    const rare = probability > 0 && probability < 0.02
    const legendary = probability > 0 && probability < 0.002
    const label =
      tier.min_usd != null && tier.max_usd != null
        ? `$${tier.min_usd} - $${tier.max_usd}`
        : tier.name || interfaceI18n.t('奖励 {{param0}}', { param0: index + 1 })
    return {
      key: tier.name || `tier-${index}`,
      label,
      tag: resolveTierTag(legendary, rare),
      strong: legendary,
    }
  })
}

function resolveTierTag(legendary: boolean, rare: boolean) {
  if (legendary) return interfaceI18n.t('稀有')
  if (rare) return interfaceI18n.t('精品')
  return undefined
}
