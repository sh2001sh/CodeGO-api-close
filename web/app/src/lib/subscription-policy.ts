import type { Schema } from './types'

export function subscriptionPolicy(value: object): string {
  return 'policy_version' in value && typeof value.policy_version === 'string'
    ? value.policy_version
    : 'legacy'
}

export function subscriptionPolicyLabel(value: object): string {
  return subscriptionPolicy(value) === 'standard_v2' ? '固定额度新版' : '原规则套餐'
}

export function validSubscription(value: Schema['Subscription'], now = Date.now()): boolean {
  return (
    !value.converted_at && value.state === 'active' && new Date(value.expires_at).getTime() > now
  )
}

export function frozenSubscriptionPlan(
  subscription: Schema['Subscription'],
  plans: Schema['Plan'][],
): Schema['Plan'] | undefined {
  const snapshot = subscription.plan_snapshot
  if (snapshot && BigInt(snapshot.id ?? 0) > 0n) return snapshot
  return plans.find((plan) => String(plan.id) === String(subscription.plan_id))
}

export function packageComparison(
  plan: Schema['Plan'],
  method?: Schema['PaymentMethod'],
): { wallet: bigint; extra: bigint; percent: string } | null {
  if (
    subscriptionPolicy(plan) !== 'standard_v2' ||
    !method ||
    plan.currency.toLowerCase() !== method.currency.toLowerCase()
  )
    return null
  const wallet = BigInt(plan.price_minor) * BigInt(method.credits_per_minor)
  if (wallet <= 0n) return null
  const extra = BigInt(plan.credits) - wallet
  const hundredths = (extra * 10_000n) / wallet
  const negative = hundredths < 0n
  const absolute = negative ? -hundredths : hundredths
  return {
    wallet,
    extra,
    percent: `${negative ? '-' : ''}${absolute / 100n}.${String(absolute % 100n).padStart(2, '0')}%`,
  }
}
