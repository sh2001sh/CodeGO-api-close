import { ArrowRight, RefreshCcw } from 'lucide-react'
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
    <section className='codego-panel flex flex-wrap items-center justify-between gap-x-8 gap-y-3 px-4 py-3 sm:px-5'>
      <div className='divide-border/60 flex min-w-0 divide-x'>
        {metrics.map(({ label, value }) => (
          <div key={label} className='min-w-0 px-6 first:pl-0 last:pr-0'>
            <div className='codego-stat-label'>{label}</div>
            <div className='text-foreground mt-1 text-xl leading-none font-semibold tabular-nums'>
              {value}
            </div>
          </div>
        ))}
      </div>
      <div className='flex items-center gap-1.5'>
        <RefreshCcw
          className={cn(
            'text-muted-foreground size-3.5',
            props.updating && 'animate-spin'
          )}
        />
        <button
          type='button'
          className='text-muted-foreground hover:text-primary inline-flex items-center gap-1 font-mono text-[10px] uppercase transition-colors'
          onClick={props.onBrowse}
        >
          {t('看市场')}
          <ArrowRight aria-hidden='true' className='size-3' />
        </button>
        <span className='bg-border h-3 w-px' />
        <button
          type='button'
          className='text-muted-foreground hover:text-primary inline-flex items-center gap-1 font-mono text-[10px] uppercase transition-colors'
          onClick={props.onInsights}
        >
          {t('看路由池')}
          <ArrowRight aria-hidden='true' className='size-3' />
        </button>
      </div>
    </section>
  )
}
