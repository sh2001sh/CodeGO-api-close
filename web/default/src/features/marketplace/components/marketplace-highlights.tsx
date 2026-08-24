import { Award, Gauge, Rabbit, Scale } from 'lucide-react'
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
        ? formatDuration(highlights.fastest.attempt_ttft_p50_ms)
        : '--',
      hint: t('最终上游尝试首字 P50'),
    },
  ]

  return (
    <section
      className='border-border flex items-center gap-3 overflow-x-auto border-b px-4 py-2.5 sm:px-5'
      aria-label={t('全部筛选结果亮点')}
    >
      <span className='text-muted-foreground flex shrink-0 items-center gap-1.5 text-xs'>
        <Award className='text-primary size-3.5' />
        {t('当前亮点')}
      </span>
      <div className='bg-border h-4 w-px shrink-0' />
      <div className='flex min-w-0 items-center gap-2'>
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
    <div
      className='bg-muted/50 flex h-8 max-w-72 shrink-0 items-center gap-2 rounded-md px-2.5 text-xs'
      title={`${props.label} · ${props.hint}`}
    >
      <Icon className='text-primary size-3.5 shrink-0' />
      <span className='text-muted-foreground shrink-0'>
        {props.label.replace('全部结果', '')}
      </span>
      <span className='max-w-28 truncate font-medium'>
        {props.group?.system_display_name || t('等待样本')}
      </span>
      <strong className='shrink-0 tabular-nums'>{props.value}</strong>
    </div>
  )
}
