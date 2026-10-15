import { lazy, Suspense, useState } from 'react'
import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { useMutation, useQuery } from '@tanstack/react-query'
import { ArrowUpRight, CreditCard, Gift, RefreshCw, Ticket, Wallet } from 'lucide-react'
import { api, unwrap } from '../lib/api'
import type { Schema } from '../lib/types'
import { resourceOptions, walletOptions } from '../lib/queries'
import { credits, date } from '../lib/format'
import { followPayment, minorAmount, paymentAmount, currencyDigits } from '../lib/commerce'
import { useTranslation } from '../lib/i18n'
import { frozenSubscriptionPlan } from '../lib/subscription-policy'
import { DataTable } from '../components/data-table'
import { Button, ErrorMessage, Field, Loading, PageHeader, Status, Tabs } from '../components/ui'
import { PaymentTypeField, selectedPaymentType } from '../features/commerce/payment-type-field'

const RedeemCredits = lazy(() =>
  import('../features/redeem').then((module) => ({ default: module.RedeemCredits })),
)
const AffiliateTransfer = lazy(() =>
  import('../features/affiliate-transfer').then((module) => ({
    default: module.AffiliateTransfer,
  })),
)
const SubscriptionActions = lazy(() =>
  import('../features/commerce/subscription-actions').then((module) => ({
    default: module.SubscriptionActions,
  })),
)
const SubscriptionValues = lazy(() =>
  import('../features/commerce/subscription-values').then((module) => ({
    default: module.SubscriptionValues,
  })),
)
const Refunds = lazy(() =>
  import('../features/commerce/refunds').then((module) => ({ default: module.Refunds })),
)

const paymentMethodsOptions = () =>
  resourceOptions('payment-methods', (signal) =>
    api.GET('/api/commerce/providers', { signal }).then(unwrap),
  )

function WalletTopup() {
  const { t } = useTranslation()
  const methods = useQuery(paymentMethodsOptions())
  const [provider, setProvider] = useState('')
  const [paymentType, setPaymentType] = useState('')
  const [localError, setLocalError] = useState<Error | null>(null)
  const method = methods.data?.find((item) => item.provider === provider) ?? methods.data?.[0]
  const purchase = useMutation({
    mutationFn: (body: Schema['CreateOrderInput']) =>
      api.POST('/api/commerce/orders', { body }).then(unwrap),
    onSuccess: (order) => {
      try {
        followPayment(order.payment_url)
      } catch (error) {
        setLocalError(error as Error)
      }
    },
  })
  const unit = method ? 10n ** BigInt(currencyDigits(method.currency)) : 1n
  return (
    <section className="wallet-topup" id="wallet-topup">
      <h2>{t('充值')}</h2>
      <ErrorMessage error={methods.error ?? purchase.error ?? localError} />
      {methods.isPending ? (
        <Loading />
      ) : (
        <>
          <label className="field" htmlFor="payment-provider">
            <span>{t('支付渠道')}</span>
            <select
              id="payment-provider"
              value={method?.provider ?? ''}
              onChange={(event) => setProvider(event.target.value)}
              disabled={!methods.data?.length || purchase.isPending}
            >
              {!methods.data?.length && <option value="">{t('暂无可用支付方式')}</option>}
              {methods.data?.map((item) => (
                <option key={item.provider} value={item.provider}>
                  {item.provider} · {item.currency.toUpperCase()}
                </option>
              ))}
            </select>
          </label>
          <PaymentTypeField
            id="wallet-payment-type"
            method={method}
            value={paymentType}
            onChange={setPaymentType}
            disabled={purchase.isPending}
          />
          {method && (
            <dl className="wallet-rate">
              <div>
                <dt>{t('兑换率')}</dt>
                <dd>
                  {paymentAmount(unit, method.currency)} ={' '}
                  {credits(BigInt(method.credits_per_minor) * unit)}
                </dd>
              </div>
            </dl>
          )}
          <form
            className="form-panel"
            onSubmit={(event) => {
              event.preventDefault()
              setLocalError(null)
              if (!method || purchase.isPending) return
              try {
                purchase.mutate({
                  amount_minor: minorAmount(
                    String(new FormData(event.currentTarget).get('amount')),
                    method.currency,
                    t,
                  ),
                  provider: method.provider,
                  checkout_selection: {
                    payment_method: selectedPaymentType(method, paymentType) || undefined,
                  },
                  success_url: `${window.location.origin}/orders`,
                  cancel_url: `${window.location.origin}/wallet`,
                })
              } catch (error) {
                setLocalError(error as Error)
              }
            }}
          >
            <Field name="amount" label="支付金额" required placeholder="10.00" />
            <Button
              type="submit"
              loading={purchase.isPending}
              disabled={purchase.isPending || !method}
            >
              {t('前往支付')}
              <ArrowUpRight size={16} aria-hidden />
            </Button>
          </form>
        </>
      )}
    </section>
  )
}

function WalletSubscriptions(props: { section: string }) {
  const { t } = useTranslation()
  const plans = useQuery(
    resourceOptions('plans', (signal) =>
      api.GET('/api/subscription/plans', { signal }).then(unwrap),
    ),
  )
  const subscriptions = useQuery(
    resourceOptions('subscriptions', (signal) =>
      api.GET('/api/subscription/self', { signal }).then(unwrap),
    ),
  )
  const methods = useQuery({
    ...paymentMethodsOptions(),
    enabled: props.section !== 'subscriptions',
  })
  const error = plans.error ?? subscriptions.error ?? methods.error
  if (error) return <ErrorMessage error={error} />
  if (
    plans.isPending ||
    subscriptions.isPending ||
    (props.section !== 'subscriptions' && methods.isPending)
  )
    return <Loading />
  const values = {
    plans: plans.data ?? [],
    subscriptions: subscriptions.data ?? [],
    methods: methods.data ?? [],
  }
  if (props.section === 'plans') return <SubscriptionActions {...values} />
  if (props.section === 'conversion') return <SubscriptionValues {...values} />
  return (
    <section id="wallet-subscriptions">
      <h2>{t('我的订阅')}</h2>
      <DataTable
        rows={values.subscriptions}
        pageSize={10}
        rowKey={(row) => row.id}
        columns={[
          {
            label: '套餐',
            render: (row) => frozenSubscriptionPlan(row, values.plans)?.name ?? row.plan_id,
          },
          { label: '余额', render: (row) => credits(row.balance), numeric: true },
          { label: '状态', render: (row) => <Status value={row.state} /> },
          { label: '到期时间', render: (row) => date(row.expires_at) },
          { label: '保留福利截止', render: (row) => date(row.benefits_until), hideOnMobile: true },
        ]}
      />
    </section>
  )
}

export default function WalletPage() {
  const { t } = useTranslation()
  const navigate = useNavigate({ from: '/wallet' })
  const search = useSearch({ from: '/_authenticated/wallet' })
  const section = search.section ?? 'topup'
  const wallet = useQuery(walletOptions())
  return (
    <div className="wallet-workspace">
      <PageHeader
        title="钱包"
        action={
          <Link className="button button-quiet" to="/orders">
            {t('订单')}
            <ArrowUpRight size={16} aria-hidden />
          </Link>
        }
      />
      <section className="wallet-balance" aria-label={t('钱包余额')}>
        <Wallet size={22} strokeWidth={1.7} aria-hidden />
        <dl>
          <dt>{t('钱包余额')}</dt>
          <dd>
            {wallet.data ? (
              <>
                <span>{credits(wallet.data.balance_micro_credits).replace(/ credits$/, '')}</span>{' '}
                <span className="wallet-credit-unit">credits</span>
              </>
            ) : (
              '—'
            )}
          </dd>
        </dl>
        <Button variant="quiet" disabled={wallet.isFetching} onClick={() => void wallet.refetch()}>
          <RefreshCw size={16} aria-hidden />
          {t('刷新')}
        </Button>
      </section>
      <ErrorMessage error={wallet.error} />
      <Tabs
        value={section}
        label="钱包分区"
        onValueChange={(value) => void navigate({ search: { section: value }, replace: true })}
        items={[
          {
            value: 'topup',
            label: '充值',
            icon: <ArrowUpRight size={16} aria-hidden />,
            content: <WalletTopup />,
          },
          {
            value: 'subscriptions',
            label: '我的订阅',
            icon: <CreditCard size={16} aria-hidden />,
            content: (
              <Suspense fallback={<Loading />}>
                <WalletSubscriptions section="subscriptions" />
              </Suspense>
            ),
          },
          {
            value: 'plans',
            label: '购买套餐',
            icon: <CreditCard size={16} aria-hidden />,
            content: (
              <Suspense fallback={<Loading />}>
                <WalletSubscriptions section="plans" />
              </Suspense>
            ),
          },
          {
            value: 'conversion',
            label: '额度转换',
            icon: <RefreshCw size={16} aria-hidden />,
            content: (
              <Suspense fallback={<Loading />}>
                <WalletSubscriptions section="conversion" />
              </Suspense>
            ),
          },
          {
            value: 'redemption',
            label: '兑换',
            icon: <Gift size={16} aria-hidden />,
            content: (
              <Suspense fallback={<Loading />}>
                <RedeemCredits />
                <AffiliateTransfer />
              </Suspense>
            ),
          },
          {
            value: 'refunds',
            label: '退款',
            icon: <Ticket size={16} aria-hidden />,
            content: (
              <Suspense fallback={<Loading />}>
                <Refunds />
              </Suspense>
            ),
          },
        ]}
      />
    </div>
  )
}
