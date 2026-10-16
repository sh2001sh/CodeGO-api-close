import i18n from '@/i18n/config'
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { renderToStaticMarkup } from 'react-dom/server'
import { subscriptionPlanSchema } from '@/features/subscriptions/types'
import { CreditPackPlans } from './credit-pack-plans'
import { RechargeFormCard } from './recharge-form-card'
import { WalletAccountOverview } from './wallet-account-overview'

await i18n.changeLanguage('en')

const plan = subscriptionPlanSchema.parse({
  id: 1,
  title: 'Permanent 64',
  price_amount: 64,
  currency: 'CNY',
  duration_unit: 'month',
  duration_value: 1,
  quota_reset_period: 'never',
  enabled: true,
  sort_order: 0,
  max_purchase_per_user: 0,
  total_amount: 33_600_000,
  plan_type: 'credit_pack',
})

test('wallet quota stays unknown when the account and subscription requests fail', () => {
  const html = renderToStaticMarkup(
    <WalletAccountOverview
      user={null}
      onOpenBillingHistory={() => {}}
      onOpenTransferHistory={() => {}}
    />
  )
  assert.equal((html.match(/<dd>--<\/dd>/g) || []).length, 5)
  assert.doesNotMatch(html, /0 credits/)
})

test('permanent packs keep total quota and bonus distinct and exclude retired plans', () => {
  const html = renderToStaticMarkup(
    <CreditPackPlans
      plans={[
        {
          plan: {
            ...plan,
            id: 2,
            title: 'Retired monthly',
            plan_type: 'monthly',
          },
        },
        { plan },
      ]}
      topupInfo={null}
      onRefresh={async () => {}}
    />
  )
  assert.match(html, /67.2 credits/)
  assert.match(html, /\+3.2 credits/)
  assert.doesNotMatch(html, /Retired monthly|待核验/)
})

test('an empty permanent catalogue does not create a purchase control', () => {
  assert.equal(
    renderToStaticMarkup(
      <CreditPackPlans plans={[]} topupInfo={null} onRefresh={async () => {}} />
    ),
    ''
  )
})

test('invalid recharge quantities cannot open payment while a valid amount can', () => {
  for (const amount of [-1, 0, 1.5, 10]) {
    const html = renderToStaticMarkup(
      <RechargeFormCard
        topupInfo={{
          enable_online_topup: true,
          enable_stripe_topup: false,
          pay_methods: [{ type: 'alipay', name: 'Pay' }],
          min_topup: 1,
          stripe_min_topup: 1,
          amount_options: [],
          discount: {},
        }}
        presetAmounts={[]}
        selectedPreset={null}
        onSelectPreset={() => {}}
        topupAmount={amount}
        onTopupAmountChange={() => {}}
        paymentAmount={10}
        calculating={false}
        onPaymentMethodSelect={() => {}}
        paymentLoading={null}
        redemptionCode=''
        onRedemptionCodeChange={() => {}}
        onRedeem={() => {}}
        redeeming={false}
      />
    )
    const button = html.match(
      /<button\b[^>]*class="[^"]*commerce-payment-method[^>]*>/
    )?.[0]
    assert.ok(button)
    assert.equal(
      /\sdisabled(?:=|[ >])/.test(button),
      amount !== 10,
      `amount=${amount}`
    )
  }
})
