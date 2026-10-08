import type { Schema } from '../../lib/types'
import { credits, date } from '../../lib/format'
import { useTranslation } from '../../lib/i18n'
import { DataTable } from '../../components/data-table'
import { Button, Status, confirmAction } from '../../components/ui'
import { BoxPlanSpecification } from './plan-specification'
import { boxPlanSnapshot } from './batch-presentation'

export function RetainedBoxProps(props: {
  items: readonly (Schema['MarketplaceProp'] & { plan_snapshot?: unknown })[]
  pending: boolean
  onAction: (input: { id: Schema['MarketplaceProp']['id']; action: 'pause' | 'use' }) => void
  onConvert: (id: Schema['MarketplaceProp']['id']) => void
}) {
  const { t } = useTranslation()
  return (
    <section className="section box-retained-props" aria-labelledby="box-props-heading">
      <h2 id="box-props-heading">{t('我的道具')}</h2>
      <p className="muted">{t('已有倍率卡、折扣券和套餐道具继续按原权益使用。')}</p>
      <DataTable
        caption="我的道具"
        rows={[...props.items]}
        rowKey={(row) => String(row.id)}
        columns={[
          { label: '名称', render: (row) => row.title },
          { label: '状态', render: (row) => <Status value={row.status} /> },
          {
            label: '权益',
            render: (row) => {
              if (row.kind === 'subscription' && row.plan_snapshot)
                return <BoxPlanSpecification snapshot={boxPlanSnapshot(row.plan_snapshot)} />
              if (row.kind === 'multiplier')
                return `${Number(row.multiplier_ppm) / 1_000_000}× · ${(Number(row.remaining_seconds) / 3600).toFixed(1)} ${t('小时')}`
              if (row.kind === 'topup_discount' || row.kind === 'subscription_discount')
                return `${Number(row.discount_rate_ppm) / 10000}%${BigInt(row.max_discount_micro) > 0n ? ` · ${t('折扣上限')} ${credits(row.max_discount_micro)}` : ''}`
              return row.kind === 'extra_draw' ? t('额外抽取一次') : t('赠送订阅套餐')
            },
          },
          { label: '到期时间', render: (row) => date(row.expires_at) },
          {
            label: '操作',
            render: (row) => {
              if (row.kind === 'topup_discount' || row.kind === 'subscription_discount')
                return (
                  <div className="box-prop-actions">
                    <span>
                      {row.kind === 'topup_discount'
                        ? t('充值时自动使用')
                        : t('购买套餐时自动使用')}
                    </span>
                    {row.status === 'available' && row.prop_type !== 'topup_discount_90' && (
                      <Button
                        variant="quiet"
                        disabled={props.pending}
                        onClick={() => {
                          void confirmAction({ title: '将此道具转换为九折充值卡？' }).then((ok) => {
                            if (ok) props.onConvert(row.id)
                          })
                        }}
                      >
                        {t('转换为九折充值卡')}
                      </Button>
                    )}
                  </div>
                )
              if (row.kind === 'extra_draw') return <span>{t('开启盲盒时自动使用')}</span>
              const canUse = row.status === 'available' || row.status === 'paused'
              const canPause = row.kind === 'multiplier' && row.status === 'active'
              return (
                <Button
                  variant="quiet"
                  disabled={props.pending || (!canUse && !canPause)}
                  onClick={() => props.onAction({ id: row.id, action: canPause ? 'pause' : 'use' })}
                >
                  {t(canPause ? '暂停' : row.plan_snapshot ? '激活套餐' : '使用')}
                </Button>
              )
            },
          },
        ]}
      />
    </section>
  )
}
