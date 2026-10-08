import type {
  BoxBatch,
  BoxBatchReward,
  BoxEntitlement,
  BoxInteger,
  LegacyBoxInventory,
  PlanSnapshot,
} from './batch-contract'

const maxInt64 = 9_223_372_036_854_775_807n

export function boxInteger(value: BoxInteger): bigint {
  if (typeof value === 'number' && !Number.isSafeInteger(value))
    throw new Error('整数超出安全范围，请使用精确字符串')
  if (!/^\d+$/.test(String(value))) throw new Error('整数格式无效')
  const integer = BigInt(value)
  if (integer > maxInt64) throw new Error('整数超出允许范围')
  return integer
}

export function entitlementCount(batch: BoxBatch, entitlements: readonly BoxEntitlement[]): bigint {
  return entitlements
    .filter((entry) => String(entry.batch_id) === String(batch.id))
    .reduce((total, entry) => total + boxInteger(entry.available_count), 0n)
}

export function canDrawBatch(batch: BoxBatch, available: bigint): boolean {
  if (boxInteger(batch.remaining_count) <= 0n) return false
  if (batch.purpose === 'consumption')
    return available > 0n && (batch.state === 'published' || batch.state === 'paused')
  return batch.state === 'published'
}

/** Session-scoped IDs survive an ambiguous timeout and a page refresh. */
export function persistentBoxOperations(
  scope: string,
  storage: Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>,
  create: () => string = () => crypto.randomUUID(),
) {
  const keyFor = (key: string) => `codego:box-operation:${scope}:${key}`
  return {
    pending(key: string): string | null {
      return storage.getItem(keyFor(key))
    },
    forPayload(key: string): string {
      const storageKey = keyFor(key)
      const existing = storage.getItem(storageKey)
      if (existing) return existing
      const id = create()
      storage.setItem(storageKey, id)
      return id
    },
    complete(key: string) {
      storage.removeItem(keyFor(key))
    },
  }
}

export function orderedLegacyInventory(items: readonly LegacyBoxInventory[]): LegacyBoxInventory[] {
  return [...items]
    .filter((item) => boxInteger(item.available_count) > 0n)
    .sort((left, right) => {
      const a = left.expires_at ? Date.parse(left.expires_at) : Infinity
      const b = right.expires_at ? Date.parse(right.expires_at) : Infinity
      return a - b || String(left.pool_id).localeCompare(String(right.pool_id))
    })
}

/** Full-face reserve preview; service cost assumptions never reduce the promise. */
export function batchReservePreview(
  batch: Pick<BoxBatch, 'base_credits_micro' | 'rewards' | 'ancillary_cost_ppm'>,
):
  | {
      count: bigint
      required: bigint
    }
  | undefined {
  try {
    let count = 0n
    let required = 0n
    for (const reward of batch.rewards) {
      const quantity = boxInteger(reward.quantity)
      if (quantity <= 0n) return undefined
      const amount =
        reward.kind === 'subscription' ? reward.plan_snapshot?.credits : reward.amount_micro
      if (amount === undefined) return undefined
      required += boxInteger(amount) * quantity
      count += quantity
    }
    required += boxInteger(batch.base_credits_micro) * count
    if (count <= 0n || count > maxInt64 || required > maxInt64) return undefined
    return { count, required }
  } catch {
    return undefined
  }
}

export function batchRewardTotal(rewards: readonly BoxBatchReward[], remaining: boolean): bigint {
  return rewards.reduce(
    (sum, row) => sum + boxInteger(remaining ? row.remaining : row.quantity),
    0n,
  )
}

export function boxPlanSnapshot(value: unknown): PlanSnapshot | undefined {
  if (!value || typeof value !== 'object') return undefined
  const input = value as Record<string, unknown>
  const numeric = (value: unknown): value is BoxInteger =>
    typeof value === 'number' || typeof value === 'string' || typeof value === 'bigint'
  if (!numeric(input.credits) || !numeric(input.period_seconds)) return undefined
  try {
    boxInteger(input.credits)
    boxInteger(input.period_seconds)
    const modelLimits: Record<string, BoxInteger> = {}
    if (input.model_limits && typeof input.model_limits === 'object') {
      for (const [model, limit] of Object.entries(input.model_limits)) {
        if (!numeric(limit)) return undefined
        boxInteger(limit)
        modelLimits[model] = limit
      }
    }
    return {
      name: typeof input.name === 'string' ? input.name : undefined,
      credits: input.credits,
      period_seconds: input.period_seconds,
      policy_version: typeof input.policy_version === 'string' ? input.policy_version : undefined,
      model_limits: modelLimits,
    }
  } catch {
    return undefined
  }
}
