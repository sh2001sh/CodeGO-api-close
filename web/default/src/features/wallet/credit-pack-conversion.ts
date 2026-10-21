import type { UserSubscription } from '@/features/subscriptions/types'

export function estimateCreditPackConversion(
  subscription: UserSubscription,
  percent: number
): { sourceQuota: number; walletQuota: number; remainingQuota: number } {
  const granted = subscription.credit_pack_granted_quota ?? 0
  const paid = subscription.credit_pack_paid_quota ?? 0
  const remaining = subscription.amount_total - subscription.amount_used
  if (
    ![granted, paid, remaining, percent].every(Number.isSafeInteger) ||
    granted <= 0 ||
    subscription.amount_total !== granted ||
    paid <= 0 ||
    paid > granted ||
    remaining <= 0 ||
    remaining > granted ||
    percent < 1 ||
    percent > 100
  ) {
    return { sourceQuota: 0, walletQuota: 0, remainingQuota: remaining }
  }
  const source = (BigInt(remaining) * BigInt(percent)) / 100n
  return {
    sourceQuota: Number(source),
    walletQuota: Number((source * BigInt(paid)) / BigInt(granted)),
    remainingQuota: remaining - Number(source),
  }
}
