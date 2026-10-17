import i18n from '@/i18n/config'
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { renderToStaticMarkup } from 'react-dom/server'
import type { MarketplaceOwnerIncomeBreakdown } from '@/features/marketplace/types'
import {
  OwnerBillingSource,
  OwnerIncomeBreakdown,
} from './owner-income-breakdown'

await i18n.changeLanguage('en')

const snapshot: MarketplaceOwnerIncomeBreakdown = {
  algorithm_version: 'marketplace_fee_v2',
  snapshot_status: 'recorded',
  billing_source: 'credit_pack',
  subscription_id: 313,
  subscription_plan_id: 64,
  subscription_plan_title: 'Developer 64',
  subscription_type: 'credit_pack',
  settlement_gross_amount: 50_000_000,
  base_commission_amount: 2_500_000,
  base_commission_rate_bps: 500,
  package_bonus_rate: 0.03,
  channel_share_bps: 6000,
  package_surcharge_rate: 0.018,
  package_surcharge_amount: 900_000,
  referral_rebate_amount: 1_500_000,
  owner_net_amount: 45_100_000,
  formula: '50000000 - 2500000 - 900000 - 1500000 = 45100000',
}

function rowsOf(detail?: MarketplaceOwnerIncomeBreakdown | null) {
  const html = renderToStaticMarkup(<OwnerIncomeBreakdown breakdown={detail} />)
  const rows = Object.fromEntries(
    Array.from(
      html.matchAll(/<dt[^>]*>(.*?)<\/dt><dd[^>]*>([\s\S]*?)<\/dd>/g),
      ([, label, value]) => [label, value.replace(/<[^>]*>/g, '')]
    )
  )
  return { html, rows }
}

test('uses the frozen three percent bonus to show 6.8 percent total fees', () => {
  const { rows, html } = rowsOf(snapshot)
  assert.equal(rows['Base commission rate'], '5%')
  assert.equal(rows['Plan bonus rate'], '3%')
  assert.equal(rows['Channel share'], '60%')
  assert.equal(rows['Bonus surcharge rate'], '1.8%')
  assert.equal(rows['Total fee rate'], '6.8%')
  assert.equal(rows['Plan instance ID'], '313')
  assert.equal(rows['Plan ID'], '64')
  assert.equal(rows['Plan name'], 'Developer 64')
  assert.match(
    html,
    /100 credits − 5 credits − 1\.8 credits − 3 credits = 90\.2 credits/
  )
})

test('keeps historical financial amounts without inventing a current fee rate', () => {
  const { rows, html } = rowsOf({
    ...snapshot,
    snapshot_status: 'historical_partial',
    algorithm_version: null,
    subscription_plan_title: null,
    subscription_plan_id: null,
    subscription_id: null,
    base_commission_amount: 1_000_000,
    base_commission_rate_bps: null,
    package_bonus_rate: null,
    channel_share_bps: null,
    package_surcharge_rate: null,
    package_surcharge_amount: null,
    referral_rebate_amount: null,
  })
  assert.equal(rows['Base commission'], '2 credits')
  for (const label of [
    'Base commission rate',
    'Plan bonus rate',
    'Channel share',
    'Bonus surcharge rate',
    'Bonus surcharge',
    'Total fee rate',
    'Referral rebates',
    'Plan instance ID',
    'Plan ID',
    'Plan name',
  ])
    assert.equal(rows[label], 'Unknown', label)
  assert.doesNotMatch(html, /5%|6\.8%/)
})

test('a missing settlement snapshot shows unknown rather than fabricated zero fees', () => {
  const { rows } = rowsOf(null)
  assert.equal(rows['Payment source'], 'Unknown')
  assert.equal(rows['Settlement gross'], 'Unknown')
  assert.equal(rows['Base commission'], 'Unknown')
  assert.equal(rows['Bonus surcharge'], 'Unknown')
  assert.equal(rows['Referral rebates'], 'Unknown')
  assert.equal(rows['Net income'], 'Unknown')
})

test('a source snapshot without settlement preserves identity and leaves financial values unknown', () => {
  const { rows, html } = rowsOf({
    ...snapshot,
    settlement_gross_amount: null,
    base_commission_amount: null,
    base_commission_rate_bps: null,
    package_surcharge_amount: null,
    package_surcharge_rate: null,
    referral_rebate_amount: null,
    owner_net_amount: null,
    formula: null,
  })
  assert.equal(rows['Plan name'], 'Developer 64')
  assert.equal(rows['Plan instance ID'], '313')
  assert.equal(rows['Settlement gross'], 'Unknown')
  assert.equal(rows['Net income'], 'Unknown')
  assert.equal(rows['Base commission rate'], 'Unknown')
  assert.equal(rows['Total fee rate'], 'Unknown')
  assert.doesNotMatch(html, /5%|6\.8%/)
})

test('preserves known zero amounts and wallet identity', () => {
  const wallet = {
    ...snapshot,
    billing_source: 'wallet',
    subscription_id: null,
    subscription_plan_id: null,
    subscription_plan_title: null,
    settlement_gross_amount: 1,
    base_commission_amount: 0,
    package_surcharge_amount: 0,
    referral_rebate_amount: 0,
    owner_net_amount: 1,
  }
  const { rows, html } = rowsOf(wallet)
  assert.equal(rows['Payment source'], 'Wallet')
  assert.equal(rows['Base commission'], '0 credits')
  assert.equal(rows['Bonus surcharge'], '0 credits')
  assert.equal(rows['Referral rebates'], '0 credits')
  assert.equal(rows['Net income'], '0.000002 credits')
  assert.doesNotMatch(html, /Plan instance ID|Plan ID/)
})

test('compact source keeps the actual plan instance and escapes its title', () => {
  const html = renderToStaticMarkup(
    <OwnerBillingSource
      breakdown={{
        ...snapshot,
        subscription_plan_title: '<script>plan</script>',
      }}
    />
  )
  assert.match(html, /#313/)
  assert.match(html, /&lt;script&gt;plan&lt;\/script&gt;/)
  assert.doesNotMatch(html, /<script>/)
})
