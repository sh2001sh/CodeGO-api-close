import type { TFunction } from 'i18next'
import {
  formatSubscriptionQuotaAmount,
  parseSubscriptionQuotaUSDToUnits,
} from '@/features/subscriptions/lib'
import type { PlanRecord } from '@/features/subscriptions/types'
import { translateCollectiveTierLabel } from './display'

export interface PackageQuotaTier {
  label: string
  detail: string
  value: string
}

/** Builds the base and collective-benefit quota rows shown in plan details. */
export function buildPackageQuotaTiers(
  plan: PlanRecord['plan'],
  t: TFunction
): PackageQuotaTier[] {
  const baseQuota = Number(plan.total_amount || 0)
  const tiers: PackageQuotaTier[] = [
    {
      label: t('Base purchase'),
      detail: t('No waiting; active immediately after payment'),
      value: formatSubscriptionQuotaAmount(baseQuota),
    },
  ]

  if (!plan.group_buy_enabled || plan.plan_type === 'daily') return tiers

  for (const [count, bonus] of [
    [2, plan.group_buy_bonus_2],
    [3, plan.group_buy_bonus_3],
    [5, plan.group_buy_bonus_5],
  ] as const) {
    tiers.push({
      label: translateCollectiveTierLabel(count, t),
      detail: t('Tier bonus: +{{amount}} USD', {
        amount: Number(bonus || 0),
      }),
      value: formatSubscriptionQuotaAmount(
        baseQuota + parseSubscriptionQuotaUSDToUnits(bonus || 0)
      ),
    })
  }

  return tiers
}
