import { useTranslation } from '../../lib/i18n'
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import type { Schema } from '../../lib/types'
import { resourceOptions } from '../../lib/queries'
import { credits, date } from '../../lib/format'
import { followPayment, paymentAmount } from '../../lib/commerce'
import {
  frozenSubscriptionPlan,
  packageComparison,
  subscriptionPolicy,
  subscriptionPolicyLabel,
} from '../../lib/subscription-policy'
import { Button, ErrorMessage, Field, Loading, Status } from '../../components/ui'
import { errorFrom, positiveID } from './amounts'
import { ReferralOrderTerms } from './referral-order-terms'

export const subscriptionPreferenceOptions = () =>
  resourceOptions('subscription-preference', (signal) =>
    api.GET('/api/subscription/self/preference', { signal }).then(unwrap),
  )

type Purchase = { action: 'purchase' | 'renew' | 'upgrade'; body: Schema['PackagePurchaseInput'] }

export function SubscriptionActions(props: {
  plans: Schema['Plan'][]
  subscriptions: Schema['Subscription'][]
  methods: Schema['PaymentMethod'][]
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const preference = useQuery(subscriptionPreferenceOptions())
  const [provider, setProvider] = useState('')
  const [target, setTarget] = useState('')
  const [purchase, setPurchase] = useState<Purchase | null>(null)
  const [error, setError] = useState<Error | null>(null)
  const [checkout, setCheckout] = useState<Schema['Order'] | null>(null)
  const method = props.methods.find((item) => item.provider === provider) ?? props.methods[0]
  const active = props.subscriptions.filter(
    (sub) => sub.state === 'active' && new Date(sub.expires_at).getTime() > Date.now(),
  )
  const current = active.find((sub) => String(sub.id) === target)
  const buy = useMutation({
    mutationFn: ({ action, body }: Purchase) =>
      api
        .POST(
          action === 'renew'
            ? '/api/packages/renew'
            : action === 'upgrade'
              ? '/api/packages/upgrade'
              : '/api/packages/purchase',
          { body },
        )
        .then(unwrap),
    onSuccess: (order) => {
      setCheckout(order)
      void client.invalidateQueries({ queryKey: ['orders'] })
      void client.invalidateQueries({ queryKey: ['subscriptions'] })
      void client.invalidateQueries({ queryKey: ['wallet'] })
    },
  })
  const savePreference = useMutation({
    mutationFn: (body: Schema['SubscriptionPreferenceInput']) =>
      api.PUT('/api/subscription/self/preference', { body }).then(unwrap),
    onSuccess: () => client.invalidateQueries({ queryKey: ['subscription-preference'] }),
  })
  const prepare = (plan: Schema['Plan'], action: Purchase['action']) => {
    setError(null)
    if (!method) {
      setError(new Error('暂无可用支付方式'))
      return
    }
    if (plan.currency.toLowerCase() !== method.currency.toLowerCase()) {
      setError(new Error('请选择与套餐相同币种的支付方式'))
      return
    }
    if (action !== 'purchase' && !current) {
      setError(new Error('请先选择需要续期或升级的订阅'))
      return
    }
    if (
      action !== 'purchase' &&
      (subscriptionPolicy(plan) === 'standard_v2' ||
        (current && subscriptionPolicy(current) !== subscriptionPolicy(plan)))
    ) {
      setError(new Error('新版套餐独立购买，不能与原规则套餐合并续期或升级'))
      return
    }
    buy.reset()
    setCheckout(null)
    setPurchase({
      action,
      body: {
        plan_id: plan.id,
        provider: method.provider,
        target_subscription_id: action === 'purchase' ? undefined : current?.id,
        request_id: crypto.randomUUID(),
        success_url: `${window.location.origin}/orders`,
        cancel_url: `${window.location.origin}/wallet`,
      },
    })
  }
  return (
    <section className="section">
      <h2>{t('订阅购买与续期')}</h2>
      <ErrorMessage error={error ?? buy.error ?? savePreference.error ?? preference.error} />
      <div className="form-panel">
        <label className="field" htmlFor="subscription-provider">
          <span>{t('支付方式')}</span>
          <select
            id="subscription-provider"
            value={method?.provider ?? ''}
            disabled={!!purchase}
            onChange={(event) => setProvider(event.target.value)}
          >
            {!props.methods.length && <option value="">{t('暂无支付方式')}</option>}
            {props.methods.map((item) => (
              <option value={item.provider} key={item.provider}>
                {item.provider} · {item.currency.toUpperCase()}
              </option>
            ))}
          </select>
        </label>
        <label className="field" htmlFor="subscription-target">
          <span>{t('需要续期或升级的订阅')}</span>
          <select
            id="subscription-target"
            value={target}
            disabled={!!purchase}
            onChange={(event) => setTarget(event.target.value)}
          >
            <option value="">{t('选择现有订阅')}</option>
            {active.map((sub) => (
              <option key={String(sub.id)} value={String(sub.id)}>
                {frozenSubscriptionPlan(sub, props.plans)?.name ?? t('订阅 ') + String(sub.id)} ·{' '}
                {date(sub.expires_at)}
              </option>
            ))}
          </select>
        </label>
      </div>
      <p className="muted">
        {t('续期及升级价格由服务器按已使用额度核算；提交后会生成订单，确认订单金额后再前往支付。')}
      </p>
      <div className="catalog-grid">
        {props.plans
          .filter((plan) => plan.enabled && !plan.internal_only)
          .map((plan) => (
            <article className="product" key={String(plan.id)}>
              <h3>{plan.name}</h3>
              <p className="muted">{t(subscriptionPolicyLabel(plan))}</p>
              <p>
                {credits(plan.credits)} · {paymentAmount(plan.price_minor, plan.currency)}
              </p>
              {subscriptionPolicy(plan) === 'standard_v2' && (
                <p>
                  {t('与余额同口径扣费，不额外乘十倍；额度一次到账，不支持刷新。')}
                  {(() => {
                    const comparison = packageComparison(plan, method)
                    return comparison ? (
                      <>
                        {t('按当前支付方式兑换率，同金额充值可得')} {credits(comparison.wallet)}
                        {t('，此套餐')}
                        {comparison.extra >= 0n ? t('多得') : t('少得')}{' '}
                        {credits(comparison.extra >= 0n ? comparison.extra : -comparison.extra)}（
                        {comparison.percent}）。
                      </>
                    ) : (
                      t('选择同币种支付方式后可比较实际充值额度。')
                    )
                  })()}
                </p>
              )}
              <dl>
                <div>
                  <dt>{t('有效期')}</dt>
                  <dd>
                    {plan.duration_unit === 'custom'
                      ? String(plan.custom_seconds) + t(' 秒')
                      : String(plan.duration_value) +
                        ' ' +
                        t(
                          { day: '天', hour: '小时', month: '自然月', year: '自然年' }[
                            plan.duration_unit
                          ] ?? plan.duration_unit,
                        )}
                  </dd>
                </div>
                <div>
                  <dt>{t('周期额度')}</dt>
                  <dd>
                    {BigInt(plan.period_credits) > 0n
                      ? credits(plan.period_credits)
                      : t('不限制周期额度')}
                  </dd>
                </div>
                <div>
                  <dt>{t('额度重置')}</dt>
                  <dd>
                    {plan.reset_period === 'custom'
                      ? t('每 ') + String(plan.reset_custom_seconds) + t(' 秒')
                      : t(
                          { never: '不重置', daily: '每天', weekly: '每周', monthly: '每月' }[
                            plan.reset_period
                          ] ?? plan.reset_period,
                        )}
                  </dd>
                </div>
                <div>
                  <dt>{t('购买限制')}</dt>
                  <dd>
                    {plan.max_purchase_per_user > 0
                      ? t('每人最多 ') + String(plan.max_purchase_per_user) + t(' 次')
                      : t('不限购')}
                  </dd>
                </div>
              </dl>
              <div className="row-actions">
                <Button
                  disabled={
                    !!purchase ||
                    !method ||
                    method.currency.toLowerCase() !== plan.currency.toLowerCase()
                  }
                  onClick={() => prepare(plan, 'purchase')}
                >
                  {t('购买')}
                </Button>
                {current &&
                  subscriptionPolicy(plan) === 'legacy' &&
                  subscriptionPolicy(current) === 'legacy' &&
                  String(current.plan_id) === String(plan.id) && (
                    <Button
                      variant="quiet"
                      disabled={
                        !!purchase ||
                        !method ||
                        method.currency.toLowerCase() !== plan.currency.toLowerCase()
                      }
                      onClick={() => prepare(plan, 'renew')}
                    >
                      {t('续期')}
                    </Button>
                  )}
                {current &&
                  subscriptionPolicy(plan) === 'legacy' &&
                  subscriptionPolicy(current) === 'legacy' &&
                  String(current.plan_id) !== String(plan.id) && (
                    <Button
                      variant="quiet"
                      disabled={
                        !!purchase ||
                        !method ||
                        method.currency.toLowerCase() !== plan.currency.toLowerCase()
                      }
                      onClick={() => prepare(plan, 'upgrade')}
                    >
                      {t('升级到此套餐')}
                    </Button>
                  )}
              </div>
            </article>
          ))}
      </div>
      {!props.plans.some((plan) => plan.enabled && !plan.internal_only) && (
        <p className="empty-state">{t('暂无可购买套餐。')}</p>
      )}
      {purchase && !checkout && (
        <div className="form-panel">
          <p className="full-width">
            {t('确认')}
            {purchase.action === 'renew'
              ? t('续期')
              : purchase.action === 'upgrade'
                ? t('升级')
                : t('购买')}
            “{props.plans.find((plan) => String(plan.id) === String(purchase.body.plan_id))?.name}
            ”？
            {subscriptionPolicy(
              props.plans.find((plan) => String(plan.id) === String(purchase.body.plan_id)) ?? {},
            ) === 'standard_v2' && t('这是一份独立的固定额度套餐，不支持刷新，也不延长其它套餐。')}
          </p>
          <Button disabled={buy.isPending} onClick={() => buy.mutate(purchase)}>
            {buy.isPending ? t('创建订单中…') : buy.isError ? t('重试同一笔订单') : t('创建订单')}
          </Button>
          {!buy.isError && (
            <Button variant="quiet" disabled={buy.isPending} onClick={() => setPurchase(null)}>
              {t('取消')}
            </Button>
          )}
        </div>
      )}
      {checkout && (
        <div className="section" role="status">
          <ReferralOrderTerms order={checkout} />
          <p>
            {t('订单')} {checkout.trade_no} {t('· 应付')}{' '}
            {paymentAmount(checkout.amount_minor, checkout.currency)} ·{' '}
            <Status value={checkout.state} />
          </p>
          <Button
            disabled={!checkout.payment_url || checkout.state !== 'created'}
            onClick={() => {
              try {
                followPayment(checkout.payment_url)
              } catch (cause) {
                setError(errorFrom(cause))
              }
            }}
          >
            {t('前往支付')}
          </Button>
          <p className="muted">{t('订单可在订单页继续支付或取消。')}</p>
          <Button
            variant="quiet"
            onClick={() => {
              setCheckout(null)
              setPurchase(null)
            }}
          >
            {t('关闭订单信息')}
          </Button>
        </div>
      )}
      <h2>{t('扣费偏好')}</h2>
      {preference.isPending && <Loading />}
      {preference.data && (
        <form
          key={`${preference.data.billing_preference}:${preference.data.subscription_order_ids?.join(',')}`}
          className="form-panel"
          onSubmit={(event) => {
            event.preventDefault()
            setError(null)
            try {
              const form = new FormData(event.currentTarget)
              const ids = String(form.get('subscription-priority') ?? '')
                .split(/[\s,，]+/)
                .filter(Boolean)
                .map(positiveID)
              if (new Set(ids).size !== ids.length || ids.length > 1000)
                throw new Error('订阅顺序不能重复，最多 1000 项')
              if (ids.some((id) => !active.some((sub) => String(sub.id) === id)))
                throw new Error('扣费顺序只能包含当前有效订阅')
              savePreference.mutate({
                billing_preference: String(form.get('billing-preference')),
                subscription_order_ids: ids,
              })
            } catch (cause) {
              setError(errorFrom(cause))
            }
          }}
        >
          <label className="field" htmlFor="billing-preference">
            <span>{t('扣费顺序')}</span>
            <select
              id="billing-preference"
              name="billing-preference"
              defaultValue={preference.data.billing_preference}
            >
              <option value="subscription_first">{t('优先订阅，再用钱包')}</option>
              <option value="wallet_first">{t('优先钱包，再用订阅')}</option>
              <option value="subscription_only">{t('仅使用订阅')}</option>
              <option value="wallet_only">{t('仅使用钱包')}</option>
            </select>
          </label>
          <Field
            name="subscription-priority"
            label="订阅优先顺序（编号，以逗号分隔）"
            defaultValue={
              preference.data.subscription_order_ids
                ?.filter((id) => active.some((sub) => String(sub.id) === String(id)))
                .join(',') ?? ''
            }
            placeholder="留空使用默认顺序"
          />
          <p className="muted full-width">
            {t('当前有效订阅：')}
            {active
              .map(
                (sub) => `${sub.id}（${frozenSubscriptionPlan(sub, props.plans)?.name ?? '订阅'}）`,
              )
              .join('、') || t('暂无')}
          </p>
          <Button type="submit" disabled={savePreference.isPending}>
            {savePreference.isPending ? t('保存中…') : t('保存扣费偏好')}
          </Button>
          {savePreference.isSuccess && <p role="status">{t('扣费偏好已保存。')}</p>}
        </form>
      )}
    </section>
  )
}
