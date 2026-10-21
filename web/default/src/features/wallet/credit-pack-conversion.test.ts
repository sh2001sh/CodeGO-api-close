import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { UserSubscription } from '../subscriptions/types'
import { estimateCreditPackConversion } from './credit-pack-conversion'

const pack: UserSubscription = {
  id: 1,
  user_id: 1,
  plan_id: 9,
  amount_total: 33_600_000,
  amount_used: 0,
  credit_pack_granted_quota: 33_600_000,
  credit_pack_paid_quota: 32_000_000,
  start_time: 0,
  end_time: 0,
  status: 'active',
  source: 'credit_pack',
}

test('new package preview selects a percentage of the current remainder', () => {
  assert.deepEqual(estimateCreditPackConversion(pack, 50), {
    sourceQuota: 16_800_000,
    walletQuota: 16_000_000,
    remainingQuota: 16_800_000,
  })
  assert.deepEqual(
    estimateCreditPackConversion({ ...pack, amount_used: 16_800_000 }, 50),
    {
      sourceQuota: 8_400_000,
      walletQuota: 8_000_000,
      remainingQuota: 8_400_000,
    }
  )
  assert.deepEqual(estimateCreditPackConversion(pack, 100), {
    sourceQuota: 33_600_000,
    walletQuota: 32_000_000,
    remainingQuota: 0,
  })
})

test('preview rounds both debited and credited quota down exactly', () => {
  const source = (BigInt(33_600_000 - 123) * 37n) / 100n
  const result = estimateCreditPackConversion({ ...pack, amount_used: 123 }, 37)
  assert.equal(result.sourceQuota, Number(source))
  assert.equal(result.walletQuota, Number((source * 32_000_000n) / 33_600_000n))
  assert.equal(
    estimateCreditPackConversion(
      { ...pack, amount_used: pack.amount_total - 1 },
      100
    ).walletQuota,
    0
  )
})

test('invalid percentages and unsafe or inconsistent snapshots cannot show a credit', () => {
  for (const percent of [0, -1, 101, 12.5, NaN, Infinity]) {
    assert.equal(estimateCreditPackConversion(pack, percent).walletQuota, 0)
  }
  for (const bad of [
    { ...pack, credit_pack_paid_quota: 0 },
    { ...pack, credit_pack_paid_quota: pack.amount_total + 1 },
    { ...pack, amount_total: Number.MAX_SAFE_INTEGER + 1 },
  ])
    assert.equal(estimateCreditPackConversion(bad, 100).walletQuota, 0)
})
