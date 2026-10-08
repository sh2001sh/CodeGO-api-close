import { useTranslation } from '../../lib/i18n'
import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../../lib/api'
import type { Schema } from '../../lib/types'
import { Button, ErrorMessage, Field } from '../../components/ui'
import { decimalCredits, errorFrom, nonnegativeMicroCredits } from './amounts'
import { subscriptionPolicy } from '../../lib/subscription-policy'

function localDate(value: string) {
  const timestamp = new Date(value)
  return new Date(timestamp.getTime() - timestamp.getTimezoneOffset() * 60000)
    .toISOString()
    .slice(0, 16)
}

export function SubscriptionEdit(props: {
  subscription: Schema['Subscription']
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [error, setError] = useState<Error | null>(null)
  const [draft, setDraft] = useState<Schema['EditSubscriptionInput'] | null>(null)
  const client = useQueryClient()
  const sub = props.subscription
  const fixed = subscriptionPolicy(sub) === 'standard_v2'
  const edit = useMutation({
    mutationFn: (body: Schema['EditSubscriptionInput']) =>
      api.PUT('/api/subscription/admin/user_subscriptions/{id}', {
        params: { path: { id: sub.id } },
        body,
      }),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ['admin-user-subscriptions'] })
      props.onClose()
    },
  })
  return (
    <section className="section">
      <h3>
        {t('编辑订阅')} {String(sub.id)}
      </h3>
      <p className="muted">
        {t('修改额度和状态后，服务器会重新计算可用余额；存在未结算消费时会要求稍后重试。')}
      </p>
      <ErrorMessage error={error ?? edit.error} />
      {!draft && (
        <form
          className="form-panel"
          onSubmit={(event) => {
            event.preventDefault()
            setError(null)
            try {
              const form = new FormData(event.currentTarget)
              const text = (name: string) => String(form.get(name) ?? '')
              const start = new Date(text('subscription-start'))
              const end = new Date(text('subscription-expiry'))
              if (
                !Number.isFinite(start.getTime()) ||
                !Number.isFinite(end.getTime()) ||
                end <= start
              )
                throw new Error('到期时间必须晚于开始时间')
              const total = nonnegativeMicroCredits(text('subscription-total'))
              const used = nonnegativeMicroCredits(text('subscription-used'))
              const period = fixed ? 0n : nonnegativeMicroCredits(text('subscription-period'))
              const periodUsed = fixed
                ? 0n
                : nonnegativeMicroCredits(text('subscription-period-used'))
              if (total === 0n && period === 0n) throw new Error('总额度和周期额度不能同时为零')
              if ((total > 0n && used > total) || (period > 0n && periodUsed > period))
                throw new Error('已使用额度不能超过对应额度')
              setDraft({
                starts_at: start.toISOString(),
                expires_at: end.toISOString(),
                state: text('subscription-state'),
                total_credits: total,
                used_credits: used,
                period_credits: period,
                period_used: periodUsed,
                request_id: crypto.randomUUID(),
              })
            } catch (cause) {
              setError(errorFrom(cause))
            }
          }}
        >
          <Field
            name="subscription-start"
            label="开始时间"
            type="datetime-local"
            required
            defaultValue={localDate(sub.starts_at)}
          />
          <Field
            name="subscription-expiry"
            label="到期时间"
            type="datetime-local"
            required
            defaultValue={localDate(sub.expires_at)}
          />
          <label className="field" htmlFor="subscription-state">
            <span>{t('状态')}</span>
            <select
              name="subscription-state"
              id="subscription-state"
              defaultValue={
                ['active', 'expired', 'canceled'].includes(sub.state) ? sub.state : 'canceled'
              }
            >
              <option value="active">{t('有效')}</option>
              <option value="expired">{t('已到期')}</option>
              <option value="canceled">{t('已取消')}</option>
            </select>
          </label>
          <Field
            name="subscription-total"
            label="总额度 credits（0 为仅周期）"
            required
            defaultValue={decimalCredits(sub.total_credits)}
          />
          <Field
            name="subscription-used"
            label="已使用 credits"
            required
            defaultValue={decimalCredits(sub.used_credits)}
          />
          {!fixed && (
            <>
              <Field
                name="subscription-period"
                label="周期额度 credits（0 为不限制）"
                required
                defaultValue={decimalCredits(sub.period_credits)}
              />
              <Field
                name="subscription-period-used"
                label="本周期已使用 credits"
                required
                defaultValue={decimalCredits(sub.period_used)}
              />
            </>
          )}
          <div className="row-actions">
            <Button type="submit">{t('核对修改')}</Button>
            <Button type="button" variant="quiet" onClick={props.onClose}>
              {t('取消')}
            </Button>
          </div>
        </form>
      )}
      {draft && (
        <div className="form-panel">
          <p className="full-width">
            {t('确认保存订阅')} {String(sub.id)}{' '}
            {t('的修改？失败重试将保持同一份修改内容和请求编号。')}
          </p>
          <Button disabled={edit.isPending} onClick={() => edit.mutate(draft)}>
            {edit.isPending ? t('保存中…') : edit.isError ? t('重试同一份修改') : t('确认保存')}
          </Button>
          {!edit.isError && (
            <Button variant="quiet" disabled={edit.isPending} onClick={() => setDraft(null)}>
              {t('返回修改')}
            </Button>
          )}
        </div>
      )}
    </section>
  )
}
