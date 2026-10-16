import i18n from '@/i18n/config'
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { renderToStaticMarkup } from 'react-dom/server'
import type { BalanceBlindBoxOverview } from '../types'
import { BalanceBoxPurchaseWorkspace } from './balance-blind-box-purchase'
import {
  BalanceBoxPanelView,
  type BalanceBoxPanelViewProps,
} from './balance-blind-box-view'
import { BlindBoxSidebar } from './blind-box-sidebar'

await i18n.changeLanguage('en')

const balance: BalanceBlindBoxOverview = {
  enabled: true,
  price_usd: 1,
  balance_usd: 12,
  tiers: [],
  inventory_count: 0,
  purchased_today: 4,
  daily_purchase_limit: 100,
  remaining_purchase_limit: 96,
  pity_progress: 0,
  pity_threshold: 50,
  pity_guarantee_usd: 10,
  small_pity_progress: 0,
  small_pity_threshold: 10,
  small_pity_guarantee_usd: 2,
  first_draw_guarantee_usd: 1,
  first_draw_eligible: true,
  first_draw_reward_min_usd: 1,
  first_draw_reward_max_usd: 1,
  small_pity_reward_min_usd: 2,
  small_pity_reward_max_usd: 3,
  pity_reward_min_usd: 10,
  pity_reward_max_usd: 12,
}
const noop = () => {}
const base: BalanceBoxPanelViewProps = {
  balance,
  mode: 'purchase',
  count: 1,
  maxCount: 96,
  inventoryActionCount: 1,
  inventoryMaxCount: 1,
  busy: false,
  recipient: null,
  recipientId: '',
  canPurchase: true,
  canUseInventory: false,
  confirmGift: false,
  cashMethods: [{ type: 'alipay', name: 'Alipay' }],
  selectedCashMethod: { type: 'alipay', name: 'Alipay' },
  cashAmountDue: 7,
  cashPaying: false,
  onModeChange: noop,
  onCountChange: noop,
  onInventoryCountChange: noop,
  onRecipientIdChange: noop,
  onPurchase: noop,
  onOpen: noop,
  onOpenCount: noop,
  onLookup: noop,
  onGift: noop,
  onConfirmGiftChange: noop,
  onCashMethodChange: noop,
  onCashPurchase: noop,
  onOpenProps: noop,
}

test('an empty inventory exposes no enabled open action', () => {
  const html = renderToStaticMarkup(<BalanceBoxPanelView {...base} />)
  const actions =
    html.match(/<div class="blind-box-open-actions">([\s\S]*?)<\/div>/)?.[1] ??
    ''
  assert.equal((actions.match(/<button/g) ?? []).length, 2)
  assert.equal((actions.match(/\sdisabled=""/g) ?? []).length, 2)
})

test('missing purchase counters remain unknown while zero remains a valid count', () => {
  const partial = { ...balance } as Partial<BalanceBlindBoxOverview>
  delete partial.purchased_today
  delete partial.daily_purchase_limit
  const html = renderToStaticMarkup(
    <BalanceBoxPanelView
      {...base}
      balance={partial as BalanceBlindBoxOverview}
    />
  )
  assert.match(html, /— \/ —/)
  assert.doesNotMatch(html, /undefined|NaN/)
  const zero = renderToStaticMarkup(
    <BalanceBoxPanelView
      {...base}
      balance={{ ...balance, purchased_today: 0 }}
    />
  )
  assert.match(zero, /0 \/ 100/)
})

test('cash payment requires an enabled activity and an available quote', () => {
  for (const state of [
    { balance, cashAmountDue: 7, enabled: true },
    {
      balance: { ...balance, enabled: false },
      cashAmountDue: 7,
      enabled: false,
    },
    { balance, cashAmountDue: null, enabled: false },
  ]) {
    const html = renderToStaticMarkup(
      <BalanceBoxPurchaseWorkspace
        {...base}
        balance={state.balance}
        cashAmountDue={state.cashAmountDue}
      />
    )
    const buttons =
      html.match(/<button\b[^>]*class="[^"]*blind-box-pay-button[^>]*>/g) ?? []
    assert.equal(buttons.length, 2)
    assert.equal(!buttons[1].includes('disabled=""'), state.enabled)
  }
})

test('purchase amounts keep credit and cash units distinct', () => {
  const html = renderToStaticMarkup(
    <BalanceBoxPurchaseWorkspace {...base} count={5} cashAmountDue={35} />
  )
  assert.match(html, /5.00 credits/)
  assert.match(html, /Cash payment/)
  assert.doesNotMatch(html, /Renminbi|RMB channel/)
  assert.match(html, /¥35.00/)
})

test('empty props and history keep their controls without invented settlement data', () => {
  const html = renderToStaticMarkup(
    <BlindBoxSidebar
      quota={0}
      availableBoxes={0}
      pendingBoxes={0}
      records={[]}
      props={[]}
      onOpenHistory={noop}
      onOpenProps={noop}
    />
  )
  assert.equal((html.match(/<button/g) ?? []).length, 2)
  assert.doesNotMatch(html, /30D|待结算|Settlement/)
})
