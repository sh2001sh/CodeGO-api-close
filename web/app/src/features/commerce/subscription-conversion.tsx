import { useTranslation } from '../../lib/i18n'
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import type { Schema } from '../../lib/types'
import { resourceOptions } from '../../lib/queries'
import { credits, date } from '../../lib/format'
import { frozenSubscriptionPlan } from '../../lib/subscription-policy'
import { DataTable } from '../../components/data-table'
import { Button, ErrorMessage, Loading } from '../../components/ui'
import { errorFrom } from './amounts'

export const subscriptionConversionsOptions = () =>
  resourceOptions('subscription-conversions', (signal) =>
    api
      .GET('/api/subscription/self/claude-conversions', {
        signal,
      })
      .then(unwrap),
  )

export function SubscriptionConversion(props: {
  plans: Schema['Plan'][]
  subscriptions: Schema['Subscription'][]
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const history = useQuery(subscriptionConversionsOptions())
  const [error, setError] = useState<Error | null>(null)
  const [draft, setDraft] = useState<Schema['SubscriptionConversionInput'] | null>(null)
  const convert = useMutation({
    mutationFn: (body: Schema['SubscriptionConversionInput']) =>
      api.POST('/api/subscription/self/claude-conversions', { body }).then(unwrap),
    onSuccess: () => {
      setDraft(null)
      void client.invalidateQueries({ queryKey: ['subscription-conversions'] })
      void client.invalidateQueries({ queryKey: ['subscriptions'] })
      void client.invalidateQueries({ queryKey: ['wallet'] })
    },
  })
  return (
    <section className="section">
      <h2>{t('月卡额度转换')}</h2>
      <p className="muted">
        {t(
          '将月卡部分剩余额度转换为钱包通用额度。比例按月卡总额度计算，实际可转换量和到账量由服务器核算；转换会减少月卡额度，耗尽时月卡结束。',
        )}
      </p>
      <ErrorMessage error={error ?? convert.error ?? history.error} />
      {!props.subscriptions.length && <p className="empty-state">{t('暂无可转换的有效月卡。')}</p>}
      {!!props.subscriptions.length && !draft && (
        <form
          className="form-panel"
          onSubmit={(event) => {
            event.preventDefault()
            setError(null)
            try {
              const form = new FormData(event.currentTarget)
              const subscription = props.subscriptions.find(
                (sub) => String(sub.id) === String(form.get('conversion-subscription')),
              )
              if (!subscription) throw new Error('请选择有效月卡')
              const value = String(form.get('conversion-percent') ?? '')
              if (!/^\d+$/.test(value) || Number(value) < 1 || Number(value) > 100)
                throw new Error('转换比例须为 1 至 100 的整数')
              convert.reset()
              setDraft({
                subscription_id: subscription.id,
                conversion_percent: Number(value),
                request_id: crypto.randomUUID(),
              })
            } catch (cause) {
              setError(errorFrom(cause))
            }
          }}
        >
          <label className="field" htmlFor="conversion-subscription">
            <span>{t('需要转换的月卡')}</span>
            <select id="conversion-subscription" name="conversion-subscription" required>
              {props.subscriptions.map((sub) => (
                <option key={String(sub.id)} value={String(sub.id)}>
                  {frozenSubscriptionPlan(sub, props.plans)?.name} · {sub.id} {t('· 余额')}{' '}
                  {credits(sub.balance)} · {date(sub.expires_at)}
                </option>
              ))}
            </select>
          </label>
          <label className="field" htmlFor="conversion-percent">
            <span>{t('转换比例 %')}</span>
            <input
              id="conversion-percent"
              name="conversion-percent"
              type="number"
              min={1}
              max={100}
              step={1}
              required
              defaultValue="1"
            />
          </label>
          <Button type="submit">{t('核对额度转换')}</Button>
        </form>
      )}
      {draft && (
        <div className="form-panel">
          <p className="full-width">
            {t('确认转换订阅')} {draft.subscription_id} {t('总额度的')} {draft.conversion_percent}
            {t(
              '%？可用余额不足、已用重置机会或仍有消费结算时，服务器会拒绝或要求稍后重试。到账量不等同于所减少的月卡额度。',
            )}
          </p>
          <Button
            variant="danger"
            disabled={convert.isPending}
            onClick={() => convert.mutate(draft)}
          >
            {convert.isPending
              ? t('转换中…')
              : convert.isError
                ? t('重试同一次转换')
                : t('确认转换额度')}
          </Button>
          {!convert.isError && (
            <Button variant="quiet" disabled={convert.isPending} onClick={() => setDraft(null)}>
              {t('取消')}
            </Button>
          )}
        </div>
      )}
      {convert.data && (
        <p role="status">
          {t('转换完成：月卡减少')} {credits(convert.data.source_credits)}
          {t('，钱包到账')} {credits(convert.data.target_credits)}。
        </p>
      )}
      <div className="page-header">
        <h3>{t('最近转换记录')}</h3>
        <Button
          variant="quiet"
          disabled={history.isFetching}
          onClick={() => void history.refetch()}
        >
          {t('刷新记录')}
        </Button>
      </div>
      {history.isPending && <Loading />}
      <DataTable
        rows={history.data ?? []}
        rowKey={(row) => row.request_id}
        empty="暂无转换记录。"
        columns={[
          { label: '订阅', render: (row) => String(row.subscription_id) },
          { label: '比例', render: (row) => `${row.conversion_percent}%` },
          { label: '月卡减少', render: (row) => credits(row.source_credits), numeric: true },
          { label: '钱包到账', render: (row) => credits(row.target_credits), numeric: true },
          { label: '请求编号', render: (row) => row.request_id },
        ]}
      />
    </section>
  )
}
