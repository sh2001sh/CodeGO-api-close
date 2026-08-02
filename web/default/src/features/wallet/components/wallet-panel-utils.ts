import type { TFunction } from 'i18next'
import { isMonthlyCardPlan } from '@/features/subscriptions/lib'
import type {
  PlanRecord,
  UserSubscriptionRecord,
} from '@/features/subscriptions/types'

export interface WalletPlanMeta {
  title: string
  subtitle: string
  plan: PlanRecord['plan']
}

export function formatWalletDateTime(timestamp?: number): string {
  if (!timestamp) return '--'
  return new Date(timestamp * 1000).toLocaleString()
}

export function getWalletRemainingDays(timestamp?: number): number {
  if (!timestamp) return 0
  const now = Date.now() / 1000
  return Math.max(0, Math.ceil((timestamp - now) / 86400))
}

export function getOrderedSubscriptions(
  subscriptions: UserSubscriptionRecord[],
  orderIds: number[]
): UserSubscriptionRecord[] {
  if (subscriptions.length === 0) return []
  const byId = new Map(
    subscriptions.map((record) => [record.subscription.id, record] as const)
  )
  const ordered: UserSubscriptionRecord[] = []

  for (const id of orderIds) {
    const record = byId.get(id)
    if (record) {
      ordered.push(record)
      byId.delete(id)
    }
  }

  for (const record of subscriptions) {
    if (byId.has(record.subscription.id)) {
      ordered.push(record)
      byId.delete(record.subscription.id)
    }
  }

  return ordered
}

export function getSubscriptionUsageStatus(
  record: UserSubscriptionRecord,
  plan: PlanRecord['plan'] | undefined,
  t: TFunction
): {
  label: string
  note: string | null
} {
  const subscription = record.subscription
  const active =
    subscription.status === 'active' &&
    Number(subscription.end_time || 0) > Date.now() / 1000

  if (!active) {
    return {
      label:
        subscription.status === 'cancelled' ? t('Cancelled') : t('Expired'),
      note: null,
    }
  }

  const totalAmount = Number(subscription.amount_total || 0)
  const usedAmount = Number(subscription.amount_used || 0)
  const totalRemain =
    totalAmount > 0 ? Math.max(0, totalAmount - usedAmount) : 0
  const periodAmount = Number(subscription.period_amount || 0)
  const periodUsed = Number(subscription.period_used || 0)
  const periodRemain =
    periodAmount > 0 ? Math.max(0, periodAmount - periodUsed) : 0
  const isMonthlyPlan = isMonthlyCardPlan(plan)

  if (totalAmount > 0 && totalRemain <= 0) {
    return {
      label: t('Exhausted'),
      note: t(
        'This subscription is skipped automatically after its total quota is used.'
      ),
    }
  }

  if (!isMonthlyPlan && periodAmount > 0 && periodRemain <= 0) {
    return {
      label: t('Pending reset'),
      note: t(
        'This period quota is used up and will rejoin billing after reset.'
      ),
    }
  }

  return { label: t('Available'), note: null }
}
