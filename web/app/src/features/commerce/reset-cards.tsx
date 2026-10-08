import { useTranslation } from '../../lib/i18n'
import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { APIError, api, unwrap } from '../../lib/api'
import type { Schema } from '../../lib/types'
import { credits, date } from '../../lib/format'
import { Button, ErrorMessage, Loading } from '../../components/ui'
import { DataTable } from '../../components/data-table'
import { errorFrom, positiveID } from './amounts'

export function ResetCards() {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [error, setError] = useState<Error | null>(null)
  const [accepted, setAccepted] = useState(false)
  const [requestID, setRequestID] = useState('')
  const [now, setNow] = useState(Date.now())
  const [activation, setActivation] = useState<{
    card: Schema['BoundSubscriptionCard']
    requestID: string
  } | null>(null)
  const offers = useQuery({
    queryKey: ['reset-card-offers'],
    queryFn: ({ signal }) =>
      api.GET('/api/subscription/self/reset-cards/rules', { signal }).then(unwrap),
  })
  const cards = useQuery({
    queryKey: ['reset-cards'],
    queryFn: ({ signal }) => api.GET('/api/subscription/self/reset-cards', { signal }).then(unwrap),
  })
  const quote = useMutation({
    mutationFn: (body: Schema['ResetCardQuoteInput']) =>
      api.POST('/api/subscription/self/reset-cards/quote', { body }).then(unwrap),
    onSuccess: () => {
      setAccepted(false)
      setRequestID(crypto.randomUUID())
    },
  })
  const exchange = useMutation({
    mutationFn: () =>
      api
        .POST('/api/subscription/self/reset-cards/confirm', {
          body: { quote_id: quote.data!.quote_id, request_id: requestID, accepted_terms: accepted },
        })
        .then(unwrap),
    onSuccess: () => {
      setAccepted(false)
      quote.reset()
      void client.invalidateQueries({ queryKey: ['reset-cards'] })
      void client.invalidateQueries({ queryKey: ['reset-opportunity'] })
      void client.invalidateQueries({ queryKey: ['referral-rewards'] })
    },
  })
  const activate = useMutation({
    mutationFn: (input: NonNullable<typeof activation>) =>
      api
        .POST('/api/subscription/self/reset-cards/{id}/activate', {
          params: { path: { id: input.card.id } },
          body: { request_id: input.requestID },
        })
        .then(unwrap),
    onSuccess: () => {
      setActivation(null)
      void client.invalidateQueries({ queryKey: ['reset-cards'] })
      void client.invalidateQueries({ queryKey: ['subscriptions'] })
      void client.invalidateQueries({ queryKey: ['wallet'] })
    },
  })
  useEffect(() => {
    if (!quote.data) return
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [quote.data])
  const expired = !quote.data || new Date(quote.data.expires_at).getTime() <= now
  const changed = exchange.error instanceof APIError && exchange.error.status === 409
  return (
    <section className="section">
      <h2>{t('刷新次数自愿换套餐卡')}</h2>
      <p className="muted">
        {t(
          '已有次数可继续按原规则使用，也可自愿换本人专用套餐卡。未激活不开始倒计时，可分批兑换、逐张激活；激活后90天使用，固定额度、不能刷新、不能提现或转赠。',
        )}
      </p>
      <ErrorMessage
        error={
          error ?? offers.error ?? cards.error ?? quote.error ?? exchange.error ?? activate.error
        }
      />
      {offers.isPending && <Loading />}
      {!!offers.data?.length && !quote.data && (
        <form
          className="form-panel"
          onSubmit={(event) => {
            event.preventDefault()
            setError(null)
            try {
              const form = new FormData(event.currentTarget)
              const value = String(form.get('card-quantity') ?? '')
              if (!/^\d+$/.test(value) || Number(value) < 1 || Number(value) > 100)
                throw new Error('每次兑换须为1至100次的整数')
              quote.reset()
              exchange.reset()
              setAccepted(false)
              setRequestID('')
              quote.mutate({
                rule_id: positiveID(String(form.get('card-offer'))),
                quantity: Number(value),
              })
            } catch (cause) {
              setError(errorFrom(cause))
            }
          }}
        >
          <label className="field" htmlFor="card-offer">
            <span>{t('可兑换套餐卡')}</span>
            <select id="card-offer" name="card-offer" required>
              {offers.data.map((offer) => (
                <option key={String(offer.id)} value={String(offer.id)}>
                  {offer.name} {t('· 每张')} {credits(offer.credits)} {t('· 激活后')}{' '}
                  {offer.duration_days} {t('天')}
                </option>
              ))}
            </select>
          </label>
          <label className="field" htmlFor="card-quantity">
            <span>{t('本次消耗刷新次数')}</span>
            <input
              id="card-quantity"
              name="card-quantity"
              type="number"
              min={1}
              max={100}
              step={1}
              required
              defaultValue="1"
            />
          </label>
          <Button type="submit" disabled={quote.isPending}>
            {quote.isPending ? t('核算中…') : t('预览次数换卡')}
          </Button>
        </form>
      )}
      {offers.data?.length === 0 && (
        <p className="empty-state">{t('当前没有适用且已核定的换卡档位，已有刷新次数仍保留。')}</p>
      )}
      {quote.data && (
        <div className="form-panel">
          <p className="full-width">
            {t('本次消耗')} {quote.data.quantity} {t('次，获得')} {quote.data.quantity} {t('张“')}
            {quote.data.plan.name}
            {t('”，每张')} {credits(quote.data.plan.credits)}
            {t('，激活后')} {quote.data.plan.duration_value} {t('天。当前')}{' '}
            {String(quote.data.available_count)} {t('次，确认后剩余')}{' '}
            {String(BigInt(quote.data.available_count) - BigInt(quote.data.quantity))}{' '}
            {t('次；报价有效至')} {date(quote.data.expires_at)}。
          </p>
          <p className="muted full-width">
            {t(
              '卡内额度按余额同口径扣费；适用账号权限和卡内冻结的模型消费上限，不附送新邀请奖励或刷新次数。',
            )}
          </p>
          <label className="checkbox-field full-width">
            <input
              type="checkbox"
              checked={accepted}
              disabled={exchange.isPending || changed || expired}
              onChange={(event) => setAccepted(event.target.checked)}
            />
            {t(
              '我同意消耗上述次数，换取本人专用的固定额度套餐卡。这些已兑换次数不再用于原刷新；卡不能刷新、转赠或提现，未兑换次数继续保留。',
            )}
          </label>
          {(changed || expired) && (
            <p role="status" className="full-width">
              {t('次数、预算或报价已变化，请重新预览；本次未完成换卡。')}
            </p>
          )}
          <Button
            disabled={!accepted || expired || changed || exchange.isPending}
            onClick={() => exchange.mutate()}
          >
            {exchange.isPending
              ? t('换卡中…')
              : exchange.isError
                ? t('重试同一次换卡')
                : t('确认次数换卡')}
          </Button>
          <Button
            variant="quiet"
            disabled={exchange.isPending}
            onClick={() => {
              quote.reset()
              exchange.reset()
              setAccepted(false)
              setRequestID('')
            }}
          >
            {changed || expired ? t('重新预览换卡') : t('取消')}
          </Button>
        </div>
      )}
      {exchange.isSuccess && (
        <p role="status">
          {t('已换取')} {exchange.data.quantity} {t('张套餐卡，剩余')}{' '}
          {String(exchange.data.remaining_count)} {t('次。卡未激活，不开始倒计时。')}
        </p>
      )}
      <h3>{t('我的套餐卡')}</h3>
      {cards.isPending && <Loading />}
      <DataTable
        rows={cards.data ?? []}
        rowKey={(row) => row.id}
        empty="暂无套餐卡。"
        columns={[
          { label: '卡编号 / 套餐', render: (row) => `${row.id} · ${row.plan.name}` },
          { label: '额度', render: (row) => credits(row.plan.credits) },
          {
            label: '状态',
            render: (row) => (row.state === 'ready' ? t('待激活 · 不倒计时') : t('已激活')),
          },
          { label: '激活时间', render: (row) => date(row.activated_at) },
          {
            label: '订阅',
            render: (row) =>
              row.subscription_id == null ? t('待激活') : String(row.subscription_id),
          },
          {
            label: '操作',
            render: (row) => (
              <Button
                variant="quiet"
                disabled={row.state !== 'ready' || !!activation}
                onClick={() => {
                  activate.reset()
                  setActivation({ card: row, requestID: crypto.randomUUID() })
                }}
              >
                {t('激活此卡')}
              </Button>
            ),
          },
        ]}
      />
      {activation && (
        <div className="form-panel">
          <p className="full-width">
            {t('确认激活“')}
            {activation.card.plan.name}
            {t('”？从现在起')} {activation.card.plan.duration_value}{' '}
            {t('天到期，生成独立固定额度套餐，不能刷新；不延长其它套餐。')}
          </p>
          <Button disabled={activate.isPending} onClick={() => activate.mutate(activation)}>
            {activate.isPending
              ? t('激活中…')
              : activate.isError
                ? t('重试激活同一张卡')
                : t('确认激活套餐卡')}
          </Button>
          <Button variant="quiet" disabled={activate.isPending} onClick={() => setActivation(null)}>
            {t('取消')}
          </Button>
        </div>
      )}
      {activate.isSuccess && (
        <p role="status">
          {t('套餐卡已激活，订阅编号')} {String(activate.data.subscription_id)}
          {t('。详情可在钱包查看。')}
        </p>
      )}
    </section>
  )
}
