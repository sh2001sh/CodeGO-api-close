import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import { useMutation, useSuspenseQuery } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import type { Schema } from '../lib/types'
import { resourceOptions, walletOptions } from '../lib/queries'
import { credits, date } from '../lib/format'
import { followPayment, minorAmount, paymentAmount, currencyDigits } from '../lib/commerce'
import { useTranslation } from '../lib/i18n'
import { frozenSubscriptionPlan, subscriptionPolicyLabel } from '../lib/subscription-policy'
import { DataTable } from '../components/data-table'
import { Button, ErrorMessage, Field, PageHeader, Status } from '../components/ui'
import { RedeemCredits } from '../features/redeem'
import { AffiliateTransfer } from '../features/affiliate-transfer'
import { SubscriptionActions } from '../features/commerce/subscription-actions'
import { Refunds } from '../features/commerce/refunds'
import { SubscriptionValues } from '../features/commerce/subscription-values'

export default function WalletPage() {
  const { t } = useTranslation()
  const wallet = useSuspenseQuery(walletOptions()).data
  const methods = useSuspenseQuery(
    resourceOptions('payment-methods', (signal) =>
      api.GET('/api/commerce/providers', { signal }).then(unwrap),
    ),
  ).data
  const [provider, setProvider] = useState('')
  const method = methods.find((item) => item.provider === provider) ?? methods[0]
  const plans = useSuspenseQuery(
    resourceOptions('plans', (signal) =>
      api.GET('/api/subscription/plans', { signal }).then((result) => unwrap(result)),
    ),
  ).data
  const subscriptions = useSuspenseQuery(
    resourceOptions('subscriptions', (signal) =>
      api.GET('/api/subscription/self', { signal }).then((result) => unwrap(result)),
    ),
  ).data
  const [localError, setLocalError] = useState<Error | null>(null)
  const purchase = useMutation({
    mutationFn: (body: Schema['CreateOrderInput']) =>
      api.POST('/api/commerce/orders', { body }).then((result) => unwrap(result)),
    onSuccess: (order) => {
      try {
        followPayment(order.payment_url)
      } catch (error) {
        setLocalError(error as Error)
      }
    },
  })
  const checkout = (body: Pick<Schema['CreateOrderInput'], 'amount_minor' | 'plan_id'>) =>
    purchase.mutate({
      ...body,
      provider: method?.provider ?? '',
      success_url: `${window.location.origin}/orders`,
      cancel_url: `${window.location.origin}/wallet`,
    })
  return (
    <div className="wallet-workspace">
      <PageHeader
        title="钱包"
        action={
          <Link className="button button-quiet" to="/orders">
            {t('订单')}
          </Link>
        }
      />
      <nav className="wallet-nav" aria-label={t('钱包分区')}>
        {[
          ['wallet-topup', '充值'],
          ['wallet-subscriptions', '我的订阅'],
          ['wallet-plans', '订阅购买与续期'],
          ['wallet-conversion', '老套餐整份转余额'],
          ['wallet-redemption', '兑换码'],
          ['wallet-refunds', '未使用额度退款'],
        ].map(([id, label]) => (
          <a key={id} href={`#${id}`}>
            {t(label)}
          </a>
        ))}
      </nav>
      <dl className="balance-ledger">
        <div>
          <dt>{t('钱包余额')}</dt>
          <dd>{credits(wallet.balance_micro_credits)}</dd>
        </div>
      </dl>
      <AffiliateTransfer />
      <section className="section wallet-topup" id="wallet-topup">
        <h2>{t('充值')}</h2>
        <label className="field" htmlFor="payment-provider">
          <span>{t('支付方式')}</span>
          <select
            id="payment-provider"
            value={method?.provider ?? ''}
            onChange={(event) => setProvider(event.target.value)}
            disabled={!methods.length}
          >
            {!methods.length && <option value="">{t('暂无可用支付方式')}</option>}
            {methods.map((item) => (
              <option key={item.provider} value={item.provider}>
                {item.provider} · {item.currency.toUpperCase()}
              </option>
            ))}
          </select>
        </label>
        {method && (
          <p className="muted section">
            {t('每')}{' '}
            {paymentAmount(10n ** BigInt(currencyDigits(method.currency)), method.currency)}{' '}
            {t('可获得')}{' '}
            {credits(
              BigInt(method.credits_per_minor) * 10n ** BigInt(currencyDigits(method.currency)),
            )}
            。
          </p>
        )}
        <form
          className="form-panel"
          onSubmit={(event) => {
            event.preventDefault()
            setLocalError(null)
            try {
              checkout({
                amount_minor: minorAmount(
                  String(new FormData(event.currentTarget).get('amount')),
                  method?.currency,
                  t,
                ),
              })
            } catch (error) {
              setLocalError(error as Error)
            }
          }}
        >
          <Field name="amount" label="支付金额" required placeholder="10.00" />
          <Button type="submit" disabled={purchase.isPending || !method}>
            {t('前往支付')}
          </Button>
        </form>
        <ErrorMessage error={purchase.error ?? localError} />
      </section>
      <div id="wallet-plans">
        <SubscriptionActions
          plans={plans ?? []}
          subscriptions={subscriptions ?? []}
          methods={methods}
        />
      </div>
      <div id="wallet-conversion">
        <SubscriptionValues
          plans={plans ?? []}
          subscriptions={subscriptions ?? []}
          methods={methods}
        />
      </div>
      <div id="wallet-redemption">
        <RedeemCredits />
      </div>
      <section className="section" id="wallet-subscriptions">
        <h2>{t('我的订阅')}</h2>
        <DataTable
          rows={subscriptions ?? []}
          rowKey={(row) => row.id}
          columns={[
            {
              label: '套餐',
              render: (row) => frozenSubscriptionPlan(row, plans ?? [])?.name ?? row.plan_id,
            },
            { label: '余额', render: (row) => credits(row.balance), numeric: true },
            {
              label: '规则',
              render: (row) =>
                row.converted_at ? t('已转余额 · 不能刷新') : t(subscriptionPolicyLabel(row)),
            },
            { label: '状态', render: (row) => <Status value={row.state} /> },
            { label: '到期时间', render: (row) => date(row.expires_at) },
            { label: '保留福利截止', render: (row) => date(row.benefits_until) },
          ]}
        />
      </section>
      <div id="wallet-refunds">
        <Refunds />
      </div>
    </div>
  )
}
