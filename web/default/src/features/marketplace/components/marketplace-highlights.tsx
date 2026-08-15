import { Gauge, Rabbit, Scale } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatDuration, formatMultiplier } from '../lib/format'
import type { MarketplaceGroup } from '../types'

export function MarketplaceHighlights(props: { groups: MarketplaceGroup[] }) {
  const { t } = useTranslation()
  if (props.groups.length === 0) return null

  const ranked = props.groups.filter((group) => !group.observing)
  const best = [...ranked].sort((a, b) => b.score - a.score)[0]
  const cheapest = [...props.groups].sort(
    (a, b) => a.multiplier - b.multiplier
  )[0]
  const fastest = [...props.groups]
    .filter((group) => group.avg_ttft_ms > 0)
    .sort((a, b) => a.avg_ttft_ms - b.avg_ttft_ms)[0]

  const items = [
    {
      icon: Gauge,
      label: t('综合表现最佳'),
      group: best,
      value: best ? best.score.toFixed(1) : '--',
      hint: t('质量评分'),
    },
    {
      icon: Scale,
      label: t('当前最低倍率'),
      group: cheapest,
      value: cheapest ? `${formatMultiplier(cheapest.multiplier)}x` : '--',
      hint: t('消费倍率'),
    },
    {
      icon: Rabbit,
      label: t('首字响应最快'),
      group: fastest,
      value: fastest ? formatDuration(fastest.avg_ttft_ms) : '--',
      hint: 'TTFT',
    },
  ]

  return (
    <div className='border-border bg-muted/20 grid border-b lg:grid-cols-3'>
      {items.map(({ icon: Icon, label, group, value, hint }) => (
        <div
          key={label}
          className='border-border flex min-h-24 items-center gap-3 border-b px-4 py-4 last:border-r-0 lg:border-r lg:border-b-0'
        >
          <div className='bg-background text-primary flex size-9 shrink-0 items-center justify-center rounded-lg border'>
            <Icon className='size-4' />
          </div>
          <div className='min-w-0 flex-1'>
            <div className='text-muted-foreground text-xs'>{label}</div>
            <div className='mt-1 flex items-baseline justify-between gap-3'>
              <span className='truncate text-sm font-medium'>
                {group?.system_display_name || t('等待有效样本')}
              </span>
              <span className='shrink-0 font-semibold tabular-nums'>
                {value}
              </span>
            </div>
            <div className='text-muted-foreground mt-0.5 text-[11px]'>
              {hint}
            </div>
          </div>
        </div>
      ))}
    </div>
  )
}
