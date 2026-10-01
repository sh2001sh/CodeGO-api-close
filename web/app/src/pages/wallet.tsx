import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import { useMutation, useSuspenseQuery } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import type { Schema } from '../lib/types'
import { resourceOptions, walletOptions } from '../lib/queries'
import { credits, date } from '../lib/format'
import { followPayment, minorAmount, paymentAmount, currencyDigits } from '../lib/commerce'
import { useTranslation } from '../lib/i18n'
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
    <>
      <PageHeader
        title="钱包"
        action={
          <Link className="button button-quiet" to="/orders">
            {t('订单')}
          </Link>
        }
      />
      <dl className="balance-ledger">
        <div>
          <dt>{t('钱包余额')}</dt>
          <dd>{credits(wallet.balance_micro_credits)}</dd>
        </div>
      </dl>
      <AffiliateTransfer />
      <section className="section">
        <h2>{t('充值')}</h2>
        <label className="field" htmlFor="payment-provider">
          <span>支付方式</span>
          <select
            id="payment-provider"
            value={method?.provider ?? ''}
            onChange={(event) => setProvider(event.target.value)}
            disabled={!methods.length}
          >
            {!methods.length && <option value="">暂无可用支付方式</option>}
            {methods.map((item) => (
              <option key={item.provider} value={item.provider}>
                {item.provider} · {item.currency.toUpperCase()}
              </option>
            ))}
          </select>
        </label>
        {method && (
          <p className="muted section">
            每 {paymentAmount(10n ** BigInt(currencyDigits(method.currency)), method.currency)}{' '}
            可获得{' '}
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
      <SubscriptionActions
        plans={plans ?? []}
        subscriptions={subscriptions ?? []}
        methods={methods}
      />
      <SubscriptionValues
        plans={plans ?? []}
        subscriptions={subscriptions ?? []}
        methods={methods}
      />
      <RedeemCredits />
      <section className="section">
        <h2>{t('我的订阅')}</h2>
        <DataTable
          rows={subscriptions ?? []}
          rowKey={(row) => row.id}
          columns={[
            {
              label: '套餐',
              render: (row) =>
                plans?.find((plan) => String(plan.id) === String(row.plan_id))?.name ?? row.plan_id,
            },
            { label: '余额', render: (row) => credits(row.balance), numeric: true },
            { label: '状态', render: (row) => <Status value={row.state} /> },
            { label: '到期时间', render: (row) => date(row.expires_at) },
          ]}
        />
      </section>
      <Refunds />
    </>
  )
}
