import { BadgeDollarSign, Gauge, Info, RefreshCcw, Store } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { formatMultiplier } from '../lib/format'

export function MarketplaceOverview(props: {
  total: number
  ranked: number
  multiplier?: number
  updating: boolean
}) {
  const { t } = useTranslation()
  const metrics = [
    {
      icon: Store,
      label: t('可见分组'),
      value: String(props.total),
    },
    {
      icon: Gauge,
      label: t('正式排名'),
      value: String(props.ranked),
    },
    {
      icon: BadgeDollarSign,
      label: t('当前最低倍率'),
      value:
        props.multiplier == null
          ? '--'
          : `${formatMultiplier(props.multiplier)}x`,
    },
  ]

  return (
    <section className='border-border bg-card flex min-h-14 flex-col justify-between gap-2 rounded-lg border px-3 py-2.5 sm:flex-row sm:items-center sm:px-4'>
      <div className='grid min-w-0 grid-cols-3 gap-3 sm:flex sm:flex-wrap sm:items-center sm:gap-x-5 sm:gap-y-2'>
        {metrics.map(({ icon: Icon, label, value }) => (
          <div
            key={label}
            className='flex min-w-0 items-center gap-2 sm:min-w-28 sm:gap-2.5'
          >
            <Icon className='text-primary size-4 shrink-0' />
            <div className='min-w-0'>
              <div className='text-muted-foreground text-[11px]'>{label}</div>
              <div className='truncate text-sm font-semibold tabular-nums'>
                {value}
              </div>
            </div>
          </div>
        ))}
      </div>
      <div className='text-muted-foreground flex items-center justify-end gap-2 text-[11px] sm:text-xs'>
        <RefreshCcw
          className={cn('size-3.5', props.updating && 'animate-spin')}
        />
        <span>{props.updating ? t('更新中') : t('30 秒自动刷新')}</span>
        <span className='bg-border h-3 w-px' />
        <span
          className='flex items-center gap-1'
          title={t('第三方渠道由用户独立提供，请根据检测结果自行判断。')}
        >
          <Info className='size-3.5' />
          {t('第三方渠道需自行判断')}
        </span>
      </div>
    </section>
  )
}
