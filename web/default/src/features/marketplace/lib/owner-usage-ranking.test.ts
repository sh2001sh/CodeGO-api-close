import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { MarketplaceOwnerUsageItem } from '../types'
import { rankOwnerUsers } from './owner-usage-ranking'

const user = (
  id: string,
  count: number,
  amount: number,
  channel = 'a'
): MarketplaceOwnerUsageItem => ({
  user_id: id,
  external_user_id: id,
  channel_id: channel,
  channel_name: channel,
  group_id: channel,
  request_count: count,
  success_count: count,
  failed_count: 0,
  success_rate: 1,
  total_tokens: 0,
  total_consumer_amount: amount * 10,
  total_settlement_gross_amount: amount,
  last_request_at: '',
})

test('request and amount rankings differ and exclude other channels', () => {
  const items = [
    user('1', 10, 50),
    user('2', 2, 100),
    user('3', 999, 9999, 'b'),
  ]
  assert.deepEqual(
    rankOwnerUsers(items, 'a', 'requests').map((u) => u.user_id),
    ['1', '2']
  )
  assert.deepEqual(
    rankOwnerUsers(items, 'a', 'amount').map((u) => u.user_id),
    ['2', '1']
  )
  assert.equal(items[0].user_id, '1')
  assert.deepEqual(rankOwnerUsers(items, 'missing', 'amount'), [])
})

test('amount ranking uses wallet equivalents, with stable ties', () => {
  const wallet = user('10', 2, 60)
  wallet.total_consumer_amount = 60
  const subscription = user('2', 2, 60)
  assert.deepEqual(
    rankOwnerUsers([wallet, subscription], 'a', 'amount').map((u) => u.user_id),
    ['2', '10']
  )
  assert.deepEqual(
    rankOwnerUsers([subscription, wallet], 'a', 'amount').map((u) => u.user_id),
    ['2', '10']
  )
})
