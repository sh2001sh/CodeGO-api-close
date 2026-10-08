import { useTranslation } from '../../lib/i18n'
import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { APIError, api, unwrap } from '../../lib/api'
import type { Schema } from '../../lib/types'
import { credits, date } from '../../lib/format'
import {
  frozenSubscriptionPlan,
  subscriptionPolicy,
  validSubscription,
} from '../../lib/subscription-policy'
import { Button, ErrorMessage } from '../../components/ui'

export function WholeWalletConversion(props: {
  subscriptions: Schema['Subscription'][]
  plans: Schema['Plan'][]
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [selected, setSelected] = useState('')
  const [accepted, setAccepted] = useState(false)
  const [requestID, setRequestID] = useState('')
  const [now, setNow] = useState(Date.now())
  const legacy = props.subscriptions.filter((sub) => subscriptionPolicy(sub) === 'legacy')
  const subscription = legacy.find((sub) => String(sub.id) === selected)
  const refresh = () => {
    void client.invalidateQueries({ queryKey: ['subscriptions'] })
    void client.invalidateQueries({ queryKey: ['wallet'] })
    void client.invalidateQueries({ queryKey: ['reset-opportunity'] })
  }
  const quote = useMutation({
    mutationFn: (id: Schema['Subscription']['id']) =>
      api
        .POST('/api/subscription/self/wallet-conversion/quote', { body: { subscription_id: id } })
        .then(unwrap),
    onSuccess: () => {
      setAccepted(false)
      setRequestID(crypto.randomUUID())
    },
  })
  const confirm = useMutation({
    mutationFn: () =>
      api
        .POST('/api/subscription/self/wallet-conversion/confirm', {
          body: {
            quote_id: quote.data!.quote_id,
            request_id: requestID,
            accepted_terms: accepted,
          },
        })
        .then(unwrap),
    onSuccess: refresh,
  })
  const submittedPending = confirm.data?.state === 'pending' || confirm.data?.state === 'processing'
  const status = useQuery({
    queryKey: ['whole-conversion', requestID],
    enabled: !!requestID && submittedPending,
    queryFn: ({ signal }) =>
      api
        .GET('/api/subscription/self/wallet-conversion/{request_id}', {
          params: { path: { request_id: requestID } },
          signal,
        })
        .then(unwrap),
    refetchInterval: (query) =>
      ['completed', 'failed', 'rejected'].includes(query.state.data?.state ?? '') ? false : 1500,
  })
  const result = status.data ?? confirm.data
  const pending = result?.state === 'pending' || result?.state === 'processing'
  useEffect(() => {
    if (result?.state === 'completed') refresh()
  }, [result?.state])
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [])
  const expired =
    !quote.data ||
    new Date(quote.data.expires_at).getTime() <= now ||
    new Date(quote.data.subscription_expires_at).getTime() <= now
  const requote = confirm.error instanceof APIError && confirm.error.status === 409
  const completed = result?.state === 'completed'
  const getQuote = () => {
    if (!subscription || !validSubscription(subscription, Date.now())) return
    quote.reset()
    confirm.reset()
    setAccepted(false)
    setRequestID('')
    quote.mutate(subscription.id)
  }
  return (
    <section className="section">
      <h2>{t('老套餐整份转余额')}</h2>
      <p className="muted">
        {t(
          '可以继续按原规则使用，也可按已公布比例转换全部剩余权益。套餐到期后不能转换；不额外按剩余天数折算，不附加新套餐赠额。',
        )}
      </p>
      <ErrorMessage error={quote.error ?? confirm.error ?? status.error} />
      <label className="field" htmlFor="whole-conversion-subscription">
        <span>{t('需要转余额的老套餐')}</span>
        <select
          id="whole-conversion-subscription"
          value={selected}
          disabled={confirm.isPending || pending}
          onChange={(event) => {
            setSelected(event.target.value)
            quote.reset()
            confirm.reset()
            setAccepted(false)
            setRequestID('')
          }}
        >
          <option value="">{t('请选择老套餐')}</option>
          {legacy.map((sub) => (
            <option
              key={String(sub.id)}
              value={String(sub.id)}
              disabled={!validSubscription(sub, now)}
            >
              {frozenSubscriptionPlan(sub, props.plans)?.name ?? t('订阅 ') + String(sub.id)} ·{' '}
              {credits(sub.balance)} ·{' '}
              {validSubscription(sub, now) ? date(sub.expires_at) : t('已到期或不可用')}
            </option>
          ))}
        </select>
      </label>
      <Button
        variant="quiet"
        disabled={
          !subscription ||
          !validSubscription(subscription, now) ||
          quote.isPending ||
          confirm.isPending ||
          pending
        }
        onClick={getQuote}
      >
        {quote.isPending
          ? t('核算中…')
          : requote || (expired && quote.data)
            ? t('重新获取转换报价')
            : t('预览整份转余额')}
      </Button>
      {!legacy.length && (
        <p className="empty-state">{t('暂无老套餐；新版固定额度套餐不使用此转换入口。')}</p>
      )}
      {quote.data && !completed && (
        <div className="section">
          {quote.data.state === 'needs_review' ? (
            <p role="status">
              {t('需要核对：')}
              {quote.data.review_reason
                ? t(quote.data.review_reason)
                : t('历史权益或转换比例尚未核定')}
              {t('。未扣除套餐权益。')}
            </p>
          ) : (
            <>
              <dl className="metrics">
                <div>
                  <dt>{t('原总额度')}</dt>
                  <dd>{credits(quote.data.source_total)}</dd>
                </div>
                <div>
                  <dt>{t('本次全部剩余')}</dt>
                  <dd>
                    {credits(
                      BigInt(quote.data.source_credits) + BigInt(quote.data.future_credits ?? 0),
                    )}
                  </dd>
                </div>
                {BigInt(quote.data.future_credits ?? 0) > 0n && (
                  <div>
                    <dt>{t('当前可用老额度')}</dt>
                    <dd>{credits(quote.data.source_credits)}</dd>
                  </div>
                )}
                {BigInt(quote.data.future_credits ?? 0) > 0n && (
                  <div>
                    <dt>{t('未发放的周期承诺')}</dt>
                    <dd>{credits(quote.data.future_credits)}</dd>
                  </div>
                )}
                <div>
                  <dt>{t('钱包到账')}</dt>
                  <dd>{credits(quote.data.target_credits)}</dd>
                </div>
                <div>
                  <dt>{t('原付费来源')}</dt>
                  <dd>{credits(quote.data.paid_credits)}</dd>
                </div>
                <div>
                  <dt>{t('赠送及刷新来源')}</dt>
                  <dd>{credits(quote.data.reward_credits)}</dd>
                </div>
                <div>
                  <dt>{t('原套餐到期')}</dt>
                  <dd>{date(quote.data.subscription_expires_at)}</dd>
                </div>
              </dl>
              {!!quote.data.segments?.length && (
                <>
                  <h3>{t('权益分段')}</h3>
                  {quote.data.segments.map((part, index) => (
                    <p key={index}>
                      {part.name} · {t('整包老额度 → 钱包')} {credits(part.source_total)} →{' '}
                      {credits(part.wallet_credits)} · {t('当前可用老额度')}{' '}
                      {credits(part.current_credits)} · {t('未发放的周期承诺')}{' '}
                      {credits(part.future_credits)} · {t('钱包到账')}{' '}
                      {credits(part.target_credits)} · {t('原付费来源')}{' '}
                      {credits(part.paid_credits)}
                    </p>
                  ))}
                  <p className="muted">
                    {t('未来周期承诺已计入本次到账，转换后不会再次自动发放。')}
                  </p>
                </>
              )}
              <p className="muted">
                {t('本次比例：')}
                {credits(
                  BigInt(quote.data.source_credits) + BigInt(quote.data.future_credits ?? 0),
                )}{' '}
                {t('老额度 →')} {credits(quote.data.target_credits)} {t('钱包额度。估值依据：')}
                {quote.data.basis_key} {t('· 比例版本')} {String(quote.data.rule_revision)}{' '}
                {t('· 报价有效至')} {date(quote.data.expires_at)}。
              </p>
              <label className="checkbox-field">
                <input
                  type="checkbox"
                  checked={accepted}
                  disabled={confirm.isPending || pending || requote || expired}
                  onChange={(event) => setAccepted(event.target.checked)}
                />
                {t(
                  '我确认将整份剩余权益转入本人余额。原套餐停止消费且永久不能刷新或周期恢复；余额不受原套餐到期限制，不代表现金退款，赠送及刷新来源不能提现或转赠。已有其它套餐和未兑换刷新次数不变，原福利仅保留至原到期日。转换完成后不能自行撤销。',
                )}
              </label>
              {expired && (
                <p role="status">{t('报价或套餐已到期，不能确认；原套餐已到期时无法重新报价。')}</p>
              )}
              <Button
                variant="danger"
                disabled={
                  !accepted ||
                  expired ||
                  requote ||
                  confirm.isPending ||
                  pending ||
                  quote.data.state !== 'quoted'
                }
                onClick={() => confirm.mutate()}
              >
                {confirm.isPending
                  ? t('提交中…')
                  : pending
                    ? t('等待在途消费完成…')
                    : confirm.isError
                      ? t('重试同一次整份转换')
                      : t('确认整份转余额')}
              </Button>
            </>
          )}
        </div>
      )}
      {pending && !completed && (
        <p role="status">
          {t('转换处理中，正在等待原套餐消费结算和余额同步；请保留此页面，结果会自动更新。')}
        </p>
      )}
      {completed && (
        <p role="status">
          {t('整份转换完成：钱包到账')} {credits(result.target_credits)}
          {t('。原套餐不能再使用或刷新，未兑换刷新次数保留。')}
        </p>
      )}
      {result?.failure_reason && (
        <p role="alert">
          {t('转换未完成：')}
          {t(result.failure_reason)}
          {t('。请核对状态后重新报价。')}
        </p>
      )}
    </section>
  )
}
