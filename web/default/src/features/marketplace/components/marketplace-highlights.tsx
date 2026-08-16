import { Gauge, Rabbit, Scale } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatDuration, formatMultiplier } from '../lib/format'
import type {
  MarketplaceGroupHighlight,
  MarketplaceGroupHighlights,
} from '../types'

export function MarketplaceHighlights(props: {
  highlights?: MarketplaceGroupHighlights
}) {
  const { t } = useTranslation()
  const highlights = props.highlights
  if (!highlights?.best && !highlights?.cheapest && !highlights?.fastest) {
    return null
  }

  const items = [
    {
      icon: Gauge,
      label: t('全部结果综合最佳'),
      group: highlights.best,
      value: highlights.best ? highlights.best.score.toFixed(1) : '--',
      hint: t('质量评分'),
    },
    {
      icon: Scale,
      label: t('全部结果最低倍率'),
      group: highlights.cheapest,
      value: highlights.cheapest
        ? `${formatMultiplier(highlights.cheapest.multiplier)}x`
        : '--',
      hint: t('通用额度倍率'),
    },
    {
      icon: Rabbit,
      label: t('全部结果首字最快'),
      group: highlights.fastest,
      value: highlights.fastest
        ? formatDuration(highlights.fastest.avg_ttft_ms)
        : '--',
      hint: t('首字延迟 TTFT'),
    },
  ]

  return (
    <section
      className='border-border bg-primary/[0.025] border-b'
      aria-label={t('全部筛选结果亮点')}
    >
      <div className='text-muted-foreground border-border border-b px-4 py-2 text-xs sm:px-5'>
        {t('以下统计基于全部筛选结果，不受当前分页影响。')}
      </div>
      <div className='grid lg:grid-cols-3'>
        {items.map(({ icon: Icon, label, group, value, hint }) => (
          <HighlightItem
            key={label}
            icon={Icon}
            label={label}
            group={group}
            value={value}
            hint={hint}
          />
        ))}
      </div>
    </section>
  )
}

function HighlightItem(props: {
  icon: typeof Gauge
  label: string
  group?: MarketplaceGroupHighlight | null
  value: string
  hint: string
}) {
  const { t } = useTranslation()
  const Icon = props.icon
  return (
    <div className='border-border flex min-h-24 items-center gap-3 border-b px-4 py-4 last:border-r-0 lg:border-r lg:border-b-0'>
      <div className='bg-background text-primary flex size-9 shrink-0 items-center justify-center rounded-md border'>
        <Icon className='size-4' />
      </div>
      <div className='min-w-0 flex-1'>
        <div className='text-muted-foreground text-xs'>{props.label}</div>
        <div className='mt-1 flex items-baseline justify-between gap-3'>
          <span className='truncate text-sm font-medium'>
            {props.group?.system_display_name || t('等待有效样本')}
          </span>
          <span className='shrink-0 font-semibold tabular-nums'>
            {props.value}
          </span>
        </div>
        <div className='text-muted-foreground mt-0.5 text-[11px]'>
          {props.hint}
        </div>
      </div>
    </div>
  )
}
