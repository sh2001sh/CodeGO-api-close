import { ArrowRight, Info, RefreshCcw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { formatMultiplier } from '../lib/format'

export function MarketplaceOverview(props: {
  total: number
  ranked: number
  multiplier?: number
  updating: boolean
  onBrowse?: () => void
  onInsights?: () => void
}) {
  const { t } = useTranslation()
  const metrics = [
    {
      label: t('可见分组'),
      value: String(props.total),
    },
    {
      label: t('正式排名'),
      value: String(props.ranked),
    },
    {
      label: t('当前最低倍率'),
      value:
        props.multiplier == null
          ? '--'
          : `${formatMultiplier(props.multiplier)}x`,
    },
  ]

  return (
    <section className='codego-marketplace-overview border-border bg-card flex min-h-14 flex-col justify-between gap-3 rounded-lg border px-3 py-2.5 sm:px-4'>
      <div className='divide-border/60 grid min-w-0 grid-cols-3 divide-x sm:flex sm:divide-x'>
        {metrics.map(({ label, value }) => (
          <div
            key={label}
            className='min-w-0 px-3 first:pl-0 sm:min-w-28 sm:first:pl-0'
          >
            <div className='text-muted-foreground text-[11px]'>{label}</div>
            <div className='app-numeric truncate text-sm font-semibold'>
              {value}
            </div>
          </div>
        ))}
      </div>
      <div className='flex flex-wrap items-center justify-end gap-2 text-[11px] sm:text-xs'>
        <button
          type='button'
          className='codego-marketplace-overview-link'
          onClick={props.onBrowse}
        >
          {t('看市场')}
          <ArrowRight className='size-3' />
        </button>
        <button
          type='button'
          className='codego-marketplace-overview-link'
          onClick={props.onInsights}
        >
          {t('看走势')}
          <ArrowRight className='size-3' />
        </button>
        <span className='bg-border mx-1 h-3 w-px' />
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
