import { useTranslation } from '../../lib/i18n'
import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { api, unwrap } from '../../lib/api'
import type { Schema } from '../../lib/types'
import { credits, date, toMicroCredits } from '../../lib/format'
import { followPayment, paymentAmount } from '../../lib/commerce'
import { frozenSubscriptionPlan } from '../../lib/subscription-policy'
import { Button, ErrorMessage, Field, Status } from '../../components/ui'
import { decimalCredits, errorFrom, steppedMicroCredits } from './amounts'

type QuotedFuel = { body: Schema['SubscriptionFuelInput']; quote: Schema['FuelQuote'] }

export function SubscriptionFuel(props: {
  plans: Schema['Plan'][]
  subscriptions: Schema['Subscription'][]
  methods: Schema['PaymentMethod'][]
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const eligible = props.subscriptions.filter((sub) => {
    const plan = frozenSubscriptionPlan(sub, props.plans)
    return !plan || plan.fuel_enabled
  })
  const [target, setTarget] = useState('')
  const [provider, setProvider] = useState('')
  const [error, setError] = useState<Error | null>(null)
  const [quoted, setQuoted] = useState<QuotedFuel | null>(null)
  const subscription = eligible.find((sub) => String(sub.id) === target) ?? eligible[0]
  const plan = subscription ? frozenSubscriptionPlan(subscription, props.plans) : undefined
  const method = props.methods.find((item) => item.provider === provider) ?? props.methods[0]
  const quote = useMutation({
    mutationFn: async (body: Schema['SubscriptionFuelInput']) => ({
      body,
      quote: await api.POST('/api/subscription/fuel/quote', { body }).then(unwrap),
    }),
    onSuccess: setQuoted,
  })
  const purchase = useMutation({
    mutationFn: (body: Schema['SubscriptionFuelInput']) =>
      api.POST('/api/subscription/fuel/purchase', { body }).then(unwrap),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ['orders'] })
      void client.invalidateQueries({ queryKey: ['subscriptions'] })
    },
  })
  return (
    <section className="section">
      <h2>{t('订阅燃料')}</h2>
      <p className="muted">
        {t('为支持燃料的月卡增加总额度，到期时间保持不变，原有周期上限仍适用。')}
      </p>
      <ErrorMessage error={error ?? quote.error ?? purchase.error} />
      {!eligible.length && <p className="empty-state">{t('暂无支持购买燃料的有效月卡。')}</p>}
      {subscription && !quoted && (
        <form
          className="form-panel"
          key={String(subscription.id)}
          onSubmit={(event) => {
            event.preventDefault()
            setError(null)
            try {
              if (!method) throw new Error('暂无可用支付方式')
              const input = String(new FormData(event.currentTarget).get('fuel-credits') ?? '')
              const amount = plan
                ? steppedMicroCredits(input, plan.fuel_min_credits, plan.fuel_credit_step)
                : BigInt(toMicroCredits(input))
              purchase.reset()
              quote.mutate({
                subscription_id: subscription.id,
                credits: amount,
                provider: method.provider,
                success_url: `${window.location.origin}/orders`,
                cancel_url: `${window.location.origin}/wallet`,
              })
            } catch (cause) {
              setError(errorFrom(cause))
            }
          }}
        >
          <label className="field" htmlFor="fuel-target">
            <span>{t('月卡订阅')}</span>
            <select
              id="fuel-target"
              value={String(subscription.id)}
              disabled={quote.isPending}
              onChange={(event) => {
                setTarget(event.target.value)
                quote.reset()
              }}
            >
              {eligible.map((sub) => (
                <option key={String(sub.id)} value={String(sub.id)}>
                  {frozenSubscriptionPlan(sub, props.plans)?.name ?? t('已下架或内部套餐')} ·{' '}
                  {sub.id} · {date(sub.expires_at)}
                </option>
              ))}
            </select>
          </label>
          <label className="field" htmlFor="fuel-provider">
            <span>{t('燃料支付方式')}</span>
            <select
              id="fuel-provider"
              value={method?.provider ?? ''}
              disabled={quote.isPending}
              onChange={(event) => setProvider(event.target.value)}
            >
              {!props.methods.length && <option value="">{t('暂无支付方式')}</option>}
              {props.methods.map((item) => (
                <option key={item.provider} value={item.provider}>
                  {item.provider} · {item.currency.toUpperCase()}
                </option>
              ))}
            </select>
          </label>
          <Field
            name="fuel-credits"
            label="购买燃料 credits"
            required
            defaultValue={plan ? decimalCredits(plan.fuel_min_credits) : undefined}
          />
          <p className="muted full-width">
            {plan
              ? t('最低 ') +
                String(credits(plan.fuel_min_credits)) +
                t('，步长 ') +
                String(credits(plan.fuel_credit_step)) +
                '。'
              : t('套餐配置未公开，服务器将核对燃料资格及最低额度与步长。')}
            {t('最终价格由服务器报价。')}
          </p>
          <Button type="submit" disabled={quote.isPending || !method}>
            {quote.isPending ? t('报价中…') : t('获取燃料报价')}
          </Button>
        </form>
      )}
      {quoted && !purchase.data && (
        <div className="form-panel">
          <p className="full-width">
            {t('订阅')} {quoted.quote.subscription_id} {t('增加')} {credits(quoted.quote.credits)}
            {t('，应付')} {paymentAmount(quoted.quote.amount_minor, quoted.quote.currency)}
            {t('；额度到期')} {date(quoted.quote.expires_at)}。
          </p>
          <p className="muted full-width">
            {t('最低')} {credits(quoted.quote.min_credits)}
            {t('，步长')} {credits(quoted.quote.credit_step)}。
          </p>
          <Button
            disabled={purchase.isPending || purchase.isError}
            onClick={() => purchase.mutate(quoted.body)}
          >
            {purchase.isPending ? t('创建燃料订单中…') : t('确认创建燃料订单')}
          </Button>
          {!purchase.isError && (
            <Button variant="quiet" disabled={purchase.isPending} onClick={() => setQuoted(null)}>
              {t('返回修改')}
            </Button>
          )}
          {purchase.isError && (
            <>
              <p className="full-width">
                {t('订单提交未成功确认。请先检查订单记录，避免重复购买；确认后可重新报价。')}
              </p>
              <Link to="/orders" className="button button-quiet">
                {t('查看订单')}
              </Link>
              <Button
                variant="quiet"
                onClick={() => {
                  purchase.reset()
                  setQuoted(null)
                }}
              >
                {t('重新报价')}
              </Button>
            </>
          )}
        </div>
      )}
      {purchase.data && (
        <div className="section" role="status">
          <p>
            {t('订单')} {purchase.data.trade_no} {t('· 应付')}{' '}
            {paymentAmount(purchase.data.amount_minor, purchase.data.currency)} ·{' '}
            <Status value={purchase.data.state} />
          </p>
          <Button
            disabled={!purchase.data.payment_url || purchase.data.state !== 'created'}
            onClick={() => {
              try {
                followPayment(purchase.data.payment_url)
              } catch (cause) {
                setError(errorFrom(cause))
              }
            }}
          >
            {t('前往支付')}
          </Button>
          <Button
            variant="quiet"
            onClick={() => {
              purchase.reset()
              quote.reset()
              setQuoted(null)
            }}
          >
            {t('关闭燃料订单信息')}
          </Button>
        </div>
      )}
    </section>
  )
}
