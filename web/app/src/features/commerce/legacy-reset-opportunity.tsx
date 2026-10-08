import { useTranslation } from '../../lib/i18n'
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import { resourceOptions } from '../../lib/queries'
import { credits } from '../../lib/format'
import { Button, ErrorMessage, Loading } from '../../components/ui'

export function LegacyResetOpportunity() {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [confirming, setConfirming] = useState(false)
  const summary = useQuery(
    resourceOptions('reset-opportunity', (signal) =>
      api.GET('/api/subscription/self/reset-opportunity', { signal }).then(unwrap),
    ),
  )
  const reset = useMutation({
    mutationFn: () =>
      api.POST('/api/subscription/self/reset-opportunity/use', { body: {} }).then(unwrap),
    onSuccess: () => {
      setConfirming(false)
      void client.invalidateQueries({ queryKey: ['reset-opportunity'] })
      void client.invalidateQueries({ queryKey: ['referral-rewards'] })
      void client.invalidateQueries({ queryKey: ['subscriptions'] })
      void client.invalidateQueries({ queryKey: ['wallet'] })
    },
  })
  const available =
    summary.data && BigInt(summary.data.available_count) > 0n && !summary.data.used_this_month
  return (
    <section className="section">
      <h2>{t('已有刷新次数')}</h2>
      <p className="muted">
        {t(
          '已有次数继续按原规则使用，北京时间每月最多一次，仅适用于有效且符合原资格的老套餐。新版套餐和已转余额的套餐不能刷新。',
        )}
      </p>
      <ErrorMessage error={summary.error ?? reset.error} />
      {summary.isPending && <Loading />}
      {summary.data && (
        <>
          <dl className="metrics">
            <div>
              <dt>{t('可用次数')}</dt>
              <dd>{String(summary.data.available_count)}</dd>
            </div>
            <div>
              <dt>{t('已使用')}</dt>
              <dd>{String(summary.data.used_total)}</dd>
            </div>
            <div>
              <dt>{t('当前月份')}</dt>
              <dd>{summary.data.current_month}</dd>
            </div>
            <div>
              <dt>{t('本月状态')}</dt>
              <dd>{summary.data.used_this_month ? t('已使用') : t('未使用')}</dd>
            </div>
          </dl>
          {!confirming && (
            <Button
              disabled={!available}
              onClick={() => {
                reset.reset()
                setConfirming(true)
              }}
            >
              {t('使用一次重置机会')}
            </Button>
          )}
        </>
      )}
      {confirming && (
        <div className="form-panel">
          <p className="full-width">
            {t(
              '确认消耗一次已有刷新次数？服务器将选择符合原规则的老套餐。每月限制、套餐到期或没有可恢复额度时会拒绝，不会提示成功。',
            )}
          </p>
          <Button disabled={reset.isPending} onClick={() => reset.mutate()}>
            {reset.isPending ? t('处理中…') : t('确认使用重置机会')}
          </Button>
          <Button variant="quiet" disabled={reset.isPending} onClick={() => setConfirming(false)}>
            {t('取消')}
          </Button>
        </div>
      )}
      {reset.isSuccess && (
        <p role="status">
          {t('已重置，恢复')} {credits(reset.data.cleared_used_amount)}
          {t('，剩余')} {String(reset.data.reset_opportunity.available_count)} {t('次。')}
        </p>
      )}
    </section>
  )
}
