import type { TFunction } from 'i18next'
import {
  formatSubscriptionPlanTitle,
  getSubscriptionPlanSubtitle,
} from '@/features/subscriptions/lib'
import type { SubscriptionPlan } from '@/features/subscriptions/types'

export function translatePlanTitle(
  value: string | null | undefined,
  t: TFunction
) {
  const normalized = formatSubscriptionPlanTitle(value)
  if (!normalized) return t('Plan')
  if (normalized.includes('新人体验卡')) return t('Starter experience card')
  if (normalized.includes('标准周卡')) return t('Standard weekly card')

  const dayPassMatch = normalized.match(/(\d+)\s*刀日卡/)
  if (dayPassMatch) {
    return t('{{amount}} USD day pass', { amount: dayPassMatch[1] })
  }

  if (normalized.endsWith('月卡')) {
    const prefix = normalized.slice(0, -2).trim()
    return prefix ? `${prefix} ${t('Monthly plan')}` : t('Monthly plan')
  }
  if (normalized === '周卡') return t('Weekly plan')
  if (normalized === '日卡') return t('Day pass')
  return normalized
}

export function translatePlanSubtitle(
  plan: Partial<SubscriptionPlan> | null | undefined,
  t: TFunction
) {
  const subtitle = getSubscriptionPlanSubtitle(plan)
  switch (subtitle) {
    case '新人专区':
      return t('Starter zone')
    case '月卡':
      return t('Monthly plan')
    case '周卡':
      return t('Weekly plan')
    case '日卡':
      return t('Day pass')
    default:
      return subtitle
  }
}

export function translatePlanAction(action: string | undefined, t: TFunction) {
  switch (action) {
    case 'renew':
      return t('Renew now')
    case 'upgrade':
      return t('Upgrade now')
    case 'disabled':
      return t('Unavailable for subscription')
    case 'subscribe':
      return t('Subscribe now')
    default:
      return t('Subscribe')
  }
}

export function translateCollectiveTierLabel(count: number, t: TFunction) {
  return t('{{count}}-participant tier', { count })
}

export function translateDisabledReason(
  value: string | null | undefined,
  t: TFunction
) {
  const normalized = String(value || '').trim()
  if (!normalized) return ''
  if (
    normalized === '当前还有更高档且未用完的生效套餐，暂不支持直接降级。' ||
    normalized.includes('cannot subscribe to a lower-tier plan')
  ) {
    return t('A higher active plan with remaining quota prevents downgrading.')
  }
  if (
    normalized.includes(
      'renewal requires at least 30% of the current package quota to be used'
    )
  ) {
    return t(
      'Renewal is available after at least 30% of the current package quota is used.'
    )
  }
  return normalized
}
