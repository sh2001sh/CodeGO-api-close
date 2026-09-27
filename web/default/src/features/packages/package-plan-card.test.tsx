import i18n from '@/i18n/config'
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { renderToStaticMarkup } from 'react-dom/server'
import type { GroupBuyItem } from '@/features/group-buy/types'
import { subscriptionPlanSchema } from '@/features/subscriptions/types'
import { PackagePlanCard } from './package-plan-card'

await i18n.changeLanguage('en')
const plan = subscriptionPlanSchema.parse({
  id: 1,
  title: 'Standard月卡',
  price_amount: 89,
  currency: 'CNY',
  duration_unit: 'month',
  duration_value: 1,
  quota_reset_period: 'never',
  enabled: true,
  sort_order: 0,
  max_purchase_per_user: 0,
  total_amount: 325000000,
  group_buy_enabled: true,
  group_buy_bonus_2: 50,
  group_buy_bonus_3: 90,
  group_buy_bonus_5: 115,
})
const room: GroupBuyItem = {
  id: 1,
  plan_id: 1,
  plan_name: plan.title,
  plan_price: 89,
  currency: 'CNY',
  base_quota_usd: 650,
  current_count: 4,
  target_count: 5,
  bonus_at_2: 50,
  bonus_at_3: 90,
  bonus_at_5: 115,
  expires_at: Math.floor(Date.now() / 1000) + 3600,
  initiator_id: 1,
  status: 'pending',
}
function render(overrides: Partial<GroupBuyItem>) {
  return renderToStaticMarkup(
    <PackagePlanCard
      record={{ plan, action: 'subscribe' }}
      purchaseCount={0}
      collectiveRoom={{ ...room, ...overrides }}
      onPurchase={() => {}}
    />
  )
}
test('an active round shows deadline, countdown, tier totals, and one purchase entry', () => {
  const html = render({})
  assert.match(html, /Deadline:/)
  assert.match(html, /Time left:/)
  assert.match(html, /Current tier/)
  assert.match(html, /\$740/)
  assert.equal(
    (html.match(/Purchase and participate in the current period/g) || [])
      .length,
    1
  )
  assert.doesNotMatch(html, /Subscribe now/)
})
test('an expired round never offers joining or a negative countdown', () => {
  const html = render({ expires_at: 1 })
  assert.match(html, /Awaiting settlement/)
  assert.match(html, /Subscribe now/)
  assert.doesNotMatch(
    html,
    /Time left:|Purchase and participate in the current period/
  )
})
test('a full round cannot accept another participant', () => {
  assert.doesNotMatch(
    render({ current_count: 5 }),
    /Purchase and participate in the current period/
  )
})
test('already joined users are told additional purchases do not rejoin', () => {
  const html = render({ joined: true })
  assert.match(html, /Additional purchases do not join this round again/)
  assert.doesNotMatch(html, /Purchase and participate in the current period/)
})
test('unopened rounds explain the 48 hour window instead of inventing a date', () => {
  const html = render({ id: 0, current_count: 0, expires_at: 0 })
  assert.match(html, /Closes 48 hours after the first paid participant joins/)
  assert.doesNotMatch(html, /Deadline:|Time left:/)
})
