import type { Schema } from '../../lib/types'

type Integer = number | string | bigint
const int64Max = 9_223_372_036_854_775_807n

function integer(value: Integer): bigint | undefined {
  if (typeof value === 'number' && !Number.isSafeInteger(value)) return undefined
  if (typeof value === 'string' && !/^\d+$/.test(value)) return undefined
  try {
    const result = BigInt(value)
    return result >= 0n && result <= int64Max ? result : undefined
  } catch {
    return undefined
  }
}

/** Fixed decimal percentages without converting reward weights to Number. */
export function weightPercentage(weight: Integer, total: Integer, digits = 2): string | undefined {
  const numerator = integer(weight)
  const denominator = integer(total)
  if (
    numerator === undefined ||
    denominator === undefined ||
    denominator === 0n ||
    numerator > denominator
  )
    return undefined
  if (!Number.isInteger(digits) || digits < 0 || digits > 6) return undefined
  const scale = 10n ** BigInt(digits)
  const scaled = (numerator * 100n * scale) / denominator
  if (numerator > 0n && scaled === 0n) return `<${digits ? `0.${'0'.repeat(digits - 1)}1` : '1'}%`
  return `${scaled / scale}${digits ? `.${String(scaled % scale).padStart(digits, '0')}` : ''}%`
}

export function rewardTotal(rewards: readonly Schema['MarketplaceReward'][]): bigint | undefined {
  let total = 0n
  if (!rewards.length) return undefined
  for (const reward of rewards) {
    const weight = integer(reward.weight)
    if (weight === undefined || weight === 0n || total > int64Max - weight) return undefined
    total += weight
  }
  return total
}

export function enabledPools(
  pools: readonly Schema['MarketplacePool'][] | null,
): Schema['MarketplacePool'][] {
  return (pools ?? []).filter((pool) => pool.enabled)
}

export function recipientID(text: string): string | undefined {
  const value = text.trim()
  if (!/^[1-9]\d*$/.test(value)) return undefined
  return integer(value) === undefined ? undefined : value
}

/** A retry keeps its identity; a different payload gets an independent identity. */
export function boxOperationIDs(create: () => string = () => crypto.randomUUID()) {
  const pending = new Map<string, string>()
  return {
    forPayload(key: string) {
      const id = pending.get(key) ?? create()
      pending.set(key, id)
      return id
    },
    complete(key: string) {
      pending.delete(key)
    },
  }
}
