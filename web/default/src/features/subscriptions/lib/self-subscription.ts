import type {
  SelfSubscriptionData,
  UserSubscription,
  UserSubscriptionRecord,
} from '../types'

export function isCreditPackSubscription(
  subscription: UserSubscription
): boolean {
  return Number(subscription.credit_pack_granted_quota || 0) > 0
}

export function isActiveUserSubscription(
  subscription: UserSubscription,
  now = Date.now() / 1000
): boolean {
  return (
    subscription.status === 'active' &&
    (subscription.end_time > now ||
      (isCreditPackSubscription(subscription) && subscription.end_time === 0))
  )
}

export const EMPTY_SUBSCRIPTIONS: SelfSubscriptionData = {
  billing_preference: 'subscription_first',
  funding_source_order: ['subscription', 'wallet'],
  subscription_order_ids: [],
  subscriptions: [],
  all_subscriptions: [],
  conversion_config: {
    enabled: true,
  },
  recent_conversions: [],
  reset_opportunity: {
    available_count: 0,
    earned_total: 0,
    used_total: 0,
    used_this_month: false,
    current_month: '',
    last_used_month: '',
  },
}

/**
 * Order user subscriptions by the explicit billing order, appending any
 * records not present in the order list while preserving their original order.
 */
export function getOrderedSubscriptions(
  subscriptions: UserSubscriptionRecord[],
  orderIds: number[]
) {
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
