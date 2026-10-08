import { useTranslation } from '../../lib/i18n'
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import type { Schema } from '../../lib/types'
import { credits, date } from '../../lib/format'
import { decimalCredits, errorFrom, nonnegativeMicroCredits } from './amounts'
import { Button, ErrorMessage, Field, Loading } from '../../components/ui'

function percentPPM(value: string, maximum: number): bigint {
  if (!/^\d+(\.\d{1,4})?$/.test(value)) throw new Error('奖励比例最多支持四位小数')
  const amount = nonnegativeMicroCredits(value) / 100n
  if (amount > BigInt(maximum)) throw new Error('奖励比例超出利润保护上限')
  return amount
}

export function ReferralPolicyAdmin() {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [error, setError] = useState<Error | null>(null)
  const [draft, setDraft] = useState<Schema['ReferralPolicy'] | null>(null)
  const policy = useQuery({
    queryKey: ['referral-policy'],
    queryFn: ({ signal }) =>
      api.GET('/api/subscription/admin/referral-policy', { signal }).then(unwrap),
    retry: false,
  })
  const save = useMutation({
    mutationFn: (body: Schema['ReferralPolicy']) =>
      api.PUT('/api/subscription/admin/referral-policy', { body }).then(unwrap),
    onSuccess: () => {
      setDraft(null)
      void client.invalidateQueries({ queryKey: ['referral-policy'] })
    },
  })
  return (
    <section className="section">
      <h2>{t('新邀请消费奖励配置')}</h2>
      <p className="muted">
        {t(
          '活动默认关闭。启用前核定奖励上限、全额履约预算和可归属费用比例；首次生效时间冻结后，停止为新订单发刷新次数，停用消费奖励不会重新开启邀请刷新。既有订单承诺仍保留。',
        )}
      </p>
      <ErrorMessage error={error ?? policy.error ?? save.error} />
      {policy.isPending && <Loading />}
      {policy.data && (
        <>
          <dl className="metrics">
            <div>
              <dt>{t('活动状态')}</dt>
              <dd>{policy.data.enabled ? t('启用') : t('停用')}</dd>
            </div>
            <div>
              <dt>{t('首次生效时间')}</dt>
              <dd>{policy.data.effective_at ? date(policy.data.effective_at) : t('未生效')}</dd>
            </div>
            <div>
              <dt>{t('已预留奖励')}</dt>
              <dd>{credits(policy.data.reserved_credits)}</dd>
            </div>
            <div>
              <dt>{t('已支出奖励')}</dt>
              <dd>{credits(policy.data.spent_credits)}</dd>
            </div>
          </dl>
          {!draft && (
            <form
              key={String(policy.data.revision)}
              className="form-panel"
              onSubmit={(event) => {
                event.preventDefault()
                setError(null)
                save.reset()
                try {
                  const form = new FormData(event.currentTarget)
                  const text = (name: string) => String(form.get(name) ?? '').trim()
                  const window = Number(text('referral-window'))
                  const delay = Number(text('referral-delay'))
                  if (
                    !Number.isInteger(window) ||
                    window < 1 ||
                    window > 30 ||
                    !Number.isInteger(delay) ||
                    delay < 7 ||
                    delay > 90
                  )
                    throw new Error('消费窗口须为1至30天，结算延迟须为7至90天')
                  const ancillary = text('referral-ancillary')
                  setDraft({
                    ...policy.data!,
                    enabled: form.has('referral-enabled'),
                    reward_ppm: percentPPM(text('referral-percent'), 10000),
                    profit_share_ppm: percentPPM(text('referral-profit-percent'), 200000),
                    ancillary_cost_ppm: ancillary ? percentPPM(ancillary, 1000000) : null,
                    window_days: window,
                    delay_days: delay,
                    max_reward_credits: nonnegativeMicroCredits(text('referral-cap')),
                    total_budget_credits: nonnegativeMicroCredits(text('referral-budget')),
                    effective_at: policy.data!.effective_at,
                  })
                } catch (cause) {
                  setError(errorFrom(cause))
                }
              }}
            >
              <Field
                name="referral-percent"
                label="合格消费奖励 %（最高1%）"
                required
                defaultValue={String(Number(policy.data.reward_ppm) / 10000)}
              />
              <Field
                name="referral-profit-percent"
                label="正贡献利润奖励上限 %（最高20%）"
                required
                defaultValue={String(Number(policy.data.profit_share_ppm) / 10000)}
              />
              <Field
                name="referral-ancillary"
                label="可归属手续费与活动费用 %（未知留空）"
                defaultValue={
                  policy.data.ancillary_cost_ppm == null
                    ? ''
                    : String(Number(policy.data.ancillary_cost_ppm) / 10000)
                }
              />
              <Field
                name="referral-window"
                label="付款后消费窗口天数"
                type="number"
                min={1}
                required
                defaultValue={policy.data.window_days}
              />
              <Field
                name="referral-delay"
                label="消费后结算延迟天数"
                type="number"
                min={7}
                required
                defaultValue={policy.data.delay_days}
              />
              <Field
                name="referral-cap"
                label="每个首购奖励上限 credits"
                required
                defaultValue={decimalCredits(policy.data.max_reward_credits)}
              />
              <Field
                name="referral-budget"
                label="奖励全额履约总预算 credits"
                required
                defaultValue={decimalCredits(policy.data.total_budget_credits)}
              />
              <label className="checkbox-field">
                <input
                  type="checkbox"
                  name="referral-enabled"
                  defaultChecked={policy.data.enabled}
                />
                {t('启用新邀请消费奖励')}
              </label>
              <Button type="submit">{t('核对邀请活动配置')}</Button>
            </form>
          )}
        </>
      )}
      {draft && (
        <div className="form-panel">
          <p className="full-width">
            {t('确认')}
            {draft.enabled ? t('启用') : t('停用')}
            {t('消费奖励？合格收入比例')} {Number(draft.reward_ppm) / 10000}
            {t('%，最多使用正贡献利润的')} {Number(draft.profit_share_ppm) / 10000}
            {t('%，单笔上限')} {credits(draft.max_reward_credits)}
            {t('，总预算')} {credits(draft.total_budget_credits)}。
            {draft.enabled &&
              !draft.effective_at &&
              t('本次首次启用后，新订单不再赠邀请刷新；这一生效时间不能撤销。')}
            {t('既有承诺不随此次编辑改变。')}
          </p>
          <Button disabled={save.isPending} onClick={() => save.mutate(draft)}>
            {save.isPending ? t('保存中…') : t('确认保存邀请配置')}
          </Button>
          <Button variant="quiet" disabled={save.isPending} onClick={() => setDraft(null)}>
            {t('取消')}
          </Button>
        </div>
      )}
      {save.isSuccess && <p role="status">{t('邀请活动配置已保存。')}</p>}
    </section>
  )
}
