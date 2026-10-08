import { describe, expect, it } from 'vitest'
import type { BoxBatch, BoxBatchReward, BoxEntitlement } from './batch-contract'
import {
  batchReservePreview,
  batchAverageCredits,
  boxInteger,
  boxPlanSnapshot,
  canDrawBatch,
  entitlementCount,
  orderedLegacyInventory,
  persistentBoxOperations,
} from './batch-presentation'
import { newBoxBatch, validateBatchDraft } from '../commerce-admin/batch-draft'

const reward = (updates: Partial<BoxBatchReward> = {}): BoxBatchReward => ({
  id: 'credits',
  kind: 'credits',
  title: 'Credits',
  amount_micro: 500000,
  plan_id: 0,
  quantity: 2,
  remaining: 2,
  initial_probability: 1,
  remaining_probability: 1,
  ...updates,
})
const batch = (updates: Partial<BoxBatch> = {}): BoxBatch => ({
  ...newBoxBatch(),
  id: '9007199254740993',
  name: 'Test',
  purpose: 'consumption',
  price_micro: 0,
  base_credits_micro: 0,
  rewards: [reward()],
  state: 'published',
  budget_micro: 999999999,
  remaining_count: 2,
  total_count: 2,
  ...updates,
})

describe('finite batch financial and retry boundaries', () => {
  it('prices the default random box at 2.5 credits with fully reserved jackpots and no extra base', () => {
    const draft = newBoxBatch()
    expect(draft.purpose).toBe('paid_random')
    expect(draft.price_micro).toBe(2500000)
    expect(draft.base_credits_micro).toBe(0)
    expect(draft.costs_confirmed).toBe(false)
    expect(() => validateBatchDraft(draft)).not.toThrow()
    expect(batchReservePreview(draft)).toEqual({ count: 10000n, required: 51700000000n })
    expect(batchAverageCredits(draft, false)).toBe(2495000n)
    const live = { ...draft, rewards: draft.rewards.map((r) => ({ ...r, remaining: r.quantity })) }
    expect(batchAverageCredits(live, true)).toBe(2495000n)
    live.rewards.at(-1)!.remaining = 0
    expect(batchAverageCredits(live, true)).toBe(2470247n)
    expect(batchAverageCredits(draft, true)).toBeUndefined()
    const sameOrHigher = draft.rewards
      .filter((r) => boxInteger(r.amount_micro) >= boxInteger(draft.price_micro))
      .reduce((total, r) => total + boxInteger(r.quantity), 0n)
    expect(sameOrHigher).toBe(6500n)
    expect(draft.rewards.at(-1)?.amount_micro).toBe(250000000)
    expect(() => validateBatchDraft({ ...draft, base_credits_micro: 1 })).toThrow('基础额度')
    expect(() => validateBatchDraft({ ...draft, price_micro: 0 })).toThrow('售价')
    expect(() => validateBatchDraft({ ...draft, price_micro: '9223372036854775807' })).toThrow(
      '金额',
    )
  })
  it('retains request identities across reloads, isolates users and creates a new identity only after success', () => {
    const values = new Map<string, string>()
    const storage = {
      getItem: (key: string) => values.get(key) ?? null,
      setItem: (key: string, value: string) => {
        values.set(key, value)
      },
      removeItem: (key: string) => {
        values.delete(key)
      },
    }
    let sequence = 0
    const create = () => `request-${++sequence}`
    const first = persistentBoxOperations('1', storage, create).forPayload('batch:1:1')
    const reloaded = persistentBoxOperations('1', storage, create)
    expect(reloaded.pending('batch:1:1')).toBe(first)
    expect(reloaded.forPayload('batch:1:1')).toBe(first)
    expect(reloaded.forPayload('batch:2:1')).not.toBe(first)
    expect(persistentBoxOperations('2', storage, create).forPayload('batch:1:1')).not.toBe(first)
    reloaded.complete('batch:1:1')
    expect(reloaded.pending('batch:1:1')).toBe(null)
    expect(reloaded.forPayload('batch:1:1')).not.toBe(first)
    const failedStorage = {
      ...storage,
      setItem: () => {
        throw new Error('Storage blocked')
      },
    }
    expect(() =>
      persistentBoxOperations('3', failedStorage, create).forPayload('batch:1:1'),
    ).toThrow('Storage blocked')
  })
  it('honors existing free claims after a pause while forbidding new paid purchases and exhausted draws', () => {
    expect(canDrawBatch(batch({ state: 'paused' }), 1n)).toBe(true)
    expect(canDrawBatch(batch({ state: 'paused' }), 0n)).toBe(false)
    expect(canDrawBatch(batch({ state: 'paused', purpose: 'credits' }), 1n)).toBe(false)
    expect(canDrawBatch(batch({ state: 'published', purpose: 'credits' }), 0n)).toBe(true)
    expect(canDrawBatch(batch({ remaining_count: 0 }), 100n)).toBe(false)
    expect(canDrawBatch(batch({ state: 'draft' }), 100n)).toBe(false)
    const entitlements = [
      { batch_id: '9007199254740993', available_count: '9007199254740993' },
      { batch_id: '9007199254740994', available_count: 9 },
      { batch_id: '9007199254740993', available_count: 1 },
    ] as BoxEntitlement[]
    expect(entitlementCount(batch(), entitlements)).toBe(9007199254740994n)
  })
  it('budgets all base and reward face value and frozen plans without reducing obligations by contribution assumptions', () => {
    const mixed = batch({
      base_credits_micro: 1000000,
      ancillary_cost_ppm: 33333,
      rewards: [
        reward(),
        reward({
          id: 'plan',
          kind: 'subscription',
          amount_micro: 0,
          plan_id: 4,
          quantity: 3,
          plan_snapshot: {
            credits: 2000000,
            period_seconds: 86400,
            policy_version: 'standard_v2',
          } as BoxBatchReward['plan_snapshot'],
        }),
      ],
    })
    expect(batchReservePreview(mixed)).toEqual({ count: 5n, required: 12000000n })
    expect(
      batchReservePreview(
        batch({ ancillary_cost_ppm: 1, rewards: [reward({ quantity: 1, amount_micro: 1 })] }),
      ),
    ).toEqual({ count: 1n, required: 1n })
  })
  it('rejects unpriceable plans, empty/unsafe quantities and overflowing promises', () => {
    expect(
      batchReservePreview(batch({ rewards: [reward({ kind: 'subscription', plan_id: 4 })] })),
    ).toBeUndefined()
    expect(batchReservePreview(batch({ rewards: [] }))).toBeUndefined()
    expect(batchReservePreview(batch({ rewards: [reward({ quantity: 0 })] }))).toBeUndefined()
    expect(
      batchReservePreview(batch({ rewards: [reward({ quantity: Number.MAX_SAFE_INTEGER + 1 })] })),
    ).toBeUndefined()
    expect(
      batchReservePreview(
        batch({ rewards: [reward({ quantity: 2, amount_micro: '9223372036854775807' })] }),
      ),
    ).toBeUndefined()
    expect(boxInteger('9007199254740993')).toBe(9007199254740993n)
    for (const value of [-1, 1.5, '1e3', '9223372036854775808'])
      expect(() => boxInteger(value)).toThrow()
  })
  it('keeps dynamic/frozen inventory distinct and orders the earliest expiry first', () => {
    const inventory = [
      { pool_id: 1, pool_name: 'Legacy', available_count: 4, draw_current_pool: true },
      {
        pool_id: 1,
        pool_name: 'Legacy',
        available_count: 2,
        draw_current_pool: false,
        expires_at: '2026-10-15T00:00:00Z',
      },
      {
        pool_id: 2,
        pool_name: 'Expiring',
        available_count: 1,
        draw_current_pool: true,
        expires_at: '2026-10-10T00:00:00Z',
      },
      { pool_id: 3, pool_name: 'Empty', available_count: 0, draw_current_pool: false },
    ]
    const ordered = orderedLegacyInventory(inventory)
    expect(ordered.map((item) => item.pool_id)).toEqual([2, 1, 1])
    expect(
      ordered.filter((item) => item.pool_id === 1).map((item) => item.draw_current_pool),
    ).toEqual([false, true])
    expect(inventory[0].expires_at).toBeUndefined()
  })
  it('uses only valid frozen plan values and does not coerce arbitrary JSON into money', () => {
    const plan = boxPlanSnapshot({
      credits: '9007199254740993',
      period_seconds: 86400,
      model_limits: { 'gpt-model': 100 },
      enabled: false,
    })
    expect(plan?.credits).toBe('9007199254740993')
    expect(plan?.model_limits).toEqual({ 'gpt-model': 100 })
    expect(boxPlanSnapshot({ credits: '1e3', period_seconds: 86400 })).toBeUndefined()
    expect(
      boxPlanSnapshot({ credits: 1, period_seconds: 86400, model_limits: { model: -1 } }),
    ).toBeUndefined()
  })
  it('prevents misleading paid base amounts and invalid contribution/quantity settings', () => {
    const paid = batch({ purpose: 'credits', price_micro: 1000000, base_credits_micro: 1000000 })
    expect(() => validateBatchDraft(paid)).not.toThrow()
    expect(() => validateBatchDraft({ ...paid, base_credits_micro: 999999 })).toThrow(
      '确定消费额度',
    )
    expect(() => validateBatchDraft({ ...paid, contribution_share_ppm: 100001 })).toThrow('10%')
    expect(() => validateBatchDraft({ ...paid, rewards: [reward({ quantity: 0 })] })).toThrow(
      '数量',
    )
    expect(() => validateBatchDraft({ ...paid, rewards: [reward(), reward()] })).toThrow('编号')
    expect(() => validateBatchDraft(batch({ price_micro: 1 }))).toThrow('免费消费回馈')
  })
})
