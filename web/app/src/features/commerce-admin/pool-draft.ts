import { parse, stringify } from 'lossless-json'
import type { Schema } from '../../lib/types'

type Reward = Schema['MarketplaceReward']
type Guarantees = Schema['MarketplaceGuarantees']
type StandardPolicy = Schema['MarketplaceStandardPolicy']
const maxInt64 = 9223372036854775807n

export const newPoolRewards: Reward[] = [
  {
    kind: 'credits',
    title: '1 credit',
    weight: 1,
    amount_micro: 1000000,
    multiplier_ppm: 0,
    duration_seconds: 0,
    plan_id: 0,
  },
]

export const newStandardPolicy: StandardPolicy = {
  enabled: false,
  subscription_probability_ppb: 0,
  subscription_plan_id: 0,
  first_purchase_minimum_micro: 0,
  pity_minimum_micro: 0,
  low_reward_threshold_micro: 0,
  pity_after: 0,
}

export function poolConfigJSON(value: unknown): string {
  return stringify(value, undefined, 2) ?? ''
}

function object(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value)
}

function integer(value: unknown, minimum = 0n, maximum = maxInt64): boolean {
  if (typeof value === 'number' && !Number.isSafeInteger(value)) return false
  if (typeof value !== 'number' && typeof value !== 'bigint' && typeof value !== 'string')
    return false
  if (typeof value === 'string' && !/^-?\d+$/.test(value)) return false
  const number = BigInt(value)
  return number >= minimum && number <= maximum
}

function optionalInteger(value: Record<string, unknown>, key: string): boolean {
  return value[key] === undefined || integer(value[key])
}

function rewards(value: unknown): value is Reward[] {
  if (!Array.isArray(value) || value.length === 0 || value.length > 1000) return false
  let total = 0n
  for (const reward of value) {
    if (
      !object(reward) ||
      typeof reward.title !== 'string' ||
      !reward.title ||
      typeof reward.kind !== 'string' ||
      !integer(reward.weight, 1n)
    )
      return false
    total += BigInt(reward.weight as number | string | bigint)
    if (total > maxInt64) return false
    for (const key of ['amount_micro', 'multiplier_ppm', 'duration_seconds', 'plan_id']) {
      if (!integer(reward[key])) return false
    }
    for (const key of [
      'minimum_micro',
      'maximum_micro',
      'step_micro',
      'discount_rate_ppm',
      'max_discount_micro',
    ]) {
      if (!optionalInteger(reward, key)) return false
    }
    for (const key of ['prop_type', 'legacy_reward_type', 'reward_tier', 'wallet_type']) {
      if (reward[key] !== undefined && typeof reward[key] !== 'string') return false
    }
    const number = (key: string) => BigInt((reward[key] ?? 0) as number | string | bigint)
    switch (reward.kind) {
      case 'credits':
        if (
          number('amount_micro') === 0n &&
          (number('minimum_micro') <= 0n || number('maximum_micro') < number('minimum_micro'))
        )
          return false
        break
      case 'multiplier':
        if (
          number('multiplier_ppm') > 1000000n ||
          number('duration_seconds') <= 0n ||
          number('duration_seconds') > 31622400n ||
          (!reward.prop_type && number('max_discount_micro') > 0n)
        )
          return false
        break
      case 'subscription':
        if (number('plan_id') <= 0n) return false
        break
      case 'topup_discount':
      case 'subscription_discount':
        if (number('discount_rate_ppm') <= 0n || number('discount_rate_ppm') > 1000000n)
          return false
        break
      case 'extra_draw':
        break
      default:
        return false
    }
  }
  return true
}

function guarantees(value: unknown): value is Guarantees {
  if (!object(value)) return false
  for (const key of ['first', 'small', 'big']) {
    if (
      value[key] !== undefined &&
      value[key] !== null &&
      (!Array.isArray(value[key]) || (value[key].length > 0 && !rewards(value[key])))
    )
      return false
  }
  for (const key of ['small_after', 'big_after']) {
    if (
      value[key] !== undefined &&
      (typeof value[key] !== 'number' || !integer(value[key], 0n, 1000000n))
    )
      return false
  }
  for (const key of ['small_reset_micro', 'big_reset_micro']) {
    if (!optionalInteger(value, key)) return false
  }
  for (const key of ['small', 'big']) {
    if (
      Number(value[`${key}_after`] ?? 0) > 0 &&
      (!integer(value[`${key}_reset_micro`], 1n) || !rewards(value[key]))
    )
      return false
  }
  return true
}

function standard(value: unknown): value is StandardPolicy {
  if (!object(value) || typeof value.enabled !== 'boolean') return false
  if (!integer(value.subscription_probability_ppb, 0n, 1000000000n)) return false
  for (const key of [
    'subscription_plan_id',
    'first_purchase_minimum_micro',
    'pity_minimum_micro',
    'low_reward_threshold_micro',
  ]) {
    if (!integer(value[key])) return false
  }
  if (typeof value.pity_after !== 'number' || !integer(value.pity_after, 0n, 1000000n)) return false
  if (
    value.enabled &&
    BigInt(value.subscription_probability_ppb as number | string | bigint) > 0n &&
    !integer(value.subscription_plan_id, 1n)
  )
    return false
  return true
}

export function parsePoolConfig(
  rewardText: string,
  guaranteeText: string,
  standardText: string,
  scope: string,
) {
  const decode = (text: string): unknown => {
    try {
      return parse(text, undefined, (value) => {
        const number = Number(value)
        return /^-?\d+$/.test(value) && !Number.isSafeInteger(number) ? BigInt(value) : number
      })
    } catch {
      throw new Error('配置 JSON 格式无效，请检查后重试')
    }
  }
  const rewardValue = decode(rewardText)
  const guaranteeValue = decode(guaranteeText)
  const standardValue = decode(standardText)
  if (!rewards(rewardValue)) throw new Error('奖励配置无效，请检查类型、金额和权重')
  if (!guarantees(guaranteeValue)) throw new Error('保底配置无效，请检查次数、重置金额和奖励')
  if (!standard(standardValue) || (standardValue.enabled && scope !== 'standard'))
    throw new Error('标准池策略无效，请检查概率、套餐和范围')
  return { rewards: rewardValue, guarantees: guaranteeValue, standard_policy: standardValue }
}
