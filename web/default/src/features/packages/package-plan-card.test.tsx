import i18n from '@/i18n/config'
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { renderToStaticMarkup } from 'react-dom/server'
import type { GroupBuyItem } from '@/features/group-buy/types'
import { subscriptionPlanSchema } from '@/features/subscriptions/types'
import { CurrentPackagePanel, PackageHistory, PlanZone } from './components'
import { CreditPackRules } from './credit-pack-rules'
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
  assert.match(html, /740 credits/)
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

test('a disabled plan exposes the actual purchase restriction', () => {
  const html = renderToStaticMarkup(
    <PackagePlanCard
      record={{
        plan,
        action: 'disabled',
        disabled_reason: 'Snapshot unavailable',
      }}
      purchaseCount={0}
      onPurchase={() => {}}
    />
  )
  assert.match(html, /role="status"[^>]*>Snapshot unavailable/)
  assert.match(html, /disabled=""/)
})

test('historical entitlements retain identity and zero-expiry permanence', () => {
  const html = renderToStaticMarkup(
    <PackageHistory
      loading={false}
      plans={[{ plan }]}
      subscriptions={[
        {
          plan,
          subscription: {
            id: 73,
            user_id: 1,
            plan_id: plan.id,
            status: 'cancelled',
            source: 'original-order',
            start_time: 1,
            end_time: 0,
            amount_total: 10000000,
            amount_used: 500000,
          },
        },
      ]}
    />
  )
  assert.match(html, /#73/)
  assert.match(html, /original-order/)
  assert.ok(html.includes(i18n.t('永久有效')))
  assert.doesNotMatch(html, /1970/)
})

const creditPack = subscriptionPlanSchema.parse({
  ...plan,
  title: '64元永久额度包',
  plan_type: 'credit_pack',
  currency: 'CNY',
  price_amount: 64,
  total_amount: 33_600_000,
  duration_unit: 'permanent',
  fuel_enabled: true,
  fuel_min_quota: 500_000,
  fuel_quota_step: 500_000,
})

test('permanent packs offer a purchase even when old renewal and promotion flags remain', () => {
  const html = renderToStaticMarkup(
    <PackagePlanCard
      record={{ plan: creditPack, action: 'renew' }}
      purchaseCount={1}
      collectiveRoom={room}
      onPurchase={() => {}}
      currentSubscription={{
        subscription: {
          id: 74,
          user_id: 1,
          plan_id: creditPack.id,
          status: 'active',
          start_time: 1,
          end_time: 0,
          amount_total: 33_600_000,
          amount_used: 500_000,
        },
      }}
    />
  )
  assert.ok(html.includes(i18n.t('购买')))
  assert.ok(html.includes(i18n.t('到账额度')))
  assert.match(html, /67.2 credits/)
  assert.match(html, /3.2 credits/)
  assert.doesNotMatch(
    html,
    /Renew|Add quota|Collective benefit|Deadline:|Time left:|套餐首购/
  )
})

test('owned packs retain their snapshot and show only the actual bonus without sales actions', () => {
  const html = renderToStaticMarkup(
    <CurrentPackagePanel
      loading={false}
      plans={[]}
      subscriptions={[
        {
          plan: creditPack,
          subscription: {
            id: 74,
            user_id: 1,
            plan_id: creditPack.id,
            status: 'active',
            start_time: 1,
            end_time: 0,
            amount_total: 33_600_000,
            amount_used: 500_000,
            credit_pack_paid_quota: 32_000_000,
            credit_pack_granted_quota: 33_600_000,
          },
        },
      ]}
    />
  )
  assert.match(html, /64元永久额度包/)
  assert.match(html, /3.2 credits/)
  assert.ok(html.includes(i18n.t('永久有效')))
  assert.doesNotMatch(html, /<button|Renew|Add quota|1970/)
})

test('new rules explain permanent separate funding and distinguish legacy entitlements', () => {
  const html = renderToStaticMarkup(<CreditPackRules />)
  for (const rule of [
    /never expire/,
    /wallet pricing/,
    /tracked separately/,
    /Legacy plans are no longer sold/,
  ])
    assert.match(html, rule)
  assert.doesNotMatch(html, /30%|0.1x|first.purchase|10×|Monthly plan rules/)
})

test('an empty catalog gives an explicit empty state and no purchase action', () => {
  const html = renderToStaticMarkup(
    <PlanZone
      title={i18n.t('永久额度包')}
      plans={[]}
      loading={false}
      purchaseCountMap={new Map()}
      subscriptions={[]}
      onPurchase={() => {}}
    />
  )
  assert.match(html, /No plans are currently available/)
  assert.doesNotMatch(html, /<button/)
})
