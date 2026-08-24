import { ArrowDown, ArrowUp, Plus, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import type { MarketplaceAutoRoutePoolItem } from '@/features/marketplace/types'

interface AutoPoolRowProps {
  item: MarketplaceAutoRoutePoolItem
  selected: boolean
  order: number
  onToggle: (checked: boolean) => void
  onMoveUp: () => void
  onMoveDown: () => void
  canMoveUp: boolean
  canMoveDown: boolean
}

export function AutoPoolRow(props: AutoPoolRowProps) {
  return (
    <div
      className={cn(
        'group transition-colors',
        props.selected
          ? 'hover:bg-muted/20 grid gap-3 px-4 py-4 lg:px-5'
          : 'border-border bg-background hover:border-primary/50 hover:bg-muted/15 rounded-lg border p-4'
      )}
    >
      <AutoPoolRowIdentity {...props} />
      <AutoPoolRowMetrics item={props.item} />
      <AutoPoolRowMeta item={props.item} />
    </div>
  )
}

function AutoPoolRowIdentity(props: AutoPoolRowProps) {
  const { t } = useTranslation()
  const item = props.item
  return (
    <div className='flex min-w-0 items-start gap-3'>
      {props.order > 0 && (
        <span className='bg-primary/10 text-primary inline-flex size-7 shrink-0 items-center justify-center rounded-md text-xs font-semibold tabular-nums'>
          {props.order}
        </span>
      )}
      <div className='min-w-0 flex-1'>
        <div className='flex min-w-0 items-center justify-between gap-2'>
          <span className='truncate font-medium'>
            {item.system_display_name}
          </span>
          <AutoPoolRowActions {...props} />
        </div>
        <div className='text-muted-foreground mt-1 text-xs'>
          <div>
            {item.source_type === 'official'
              ? t('CodeGo 官方')
              : item.source_label || t('来源待审核')}
            <span className='mx-1'>·</span>
            {item.models.length} {t('个模型')}
          </div>
          <div
            className='mt-1 max-h-14 overflow-y-auto pr-1 leading-5 break-words whitespace-normal'
            title={item.models.join(' / ')}
          >
            {item.models.length > 0
              ? item.models.join(' / ')
              : t('暂无模型信息')}
          </div>
        </div>
      </div>
    </div>
  )
}

function AutoPoolRowActions(props: AutoPoolRowProps) {
  const { t } = useTranslation()
  if (!props.selected) {
    return (
      <Button
        type='button'
        size='sm'
        variant='outline'
        className='h-8 shrink-0 gap-1.5 px-2.5'
        onClick={() => props.onToggle(true)}
      >
        <Plus className='size-3.5' />
        {t('加入')}
      </Button>
    )
  }
  return (
    <div className='flex shrink-0 gap-1'>
      <PriorityButton
        label={t('提高路由优先级')}
        disabled={!props.canMoveUp}
        onClick={props.onMoveUp}
        icon={ArrowUp}
      />
      <PriorityButton
        label={t('降低路由优先级')}
        disabled={!props.canMoveDown}
        onClick={props.onMoveDown}
        icon={ArrowDown}
      />
      <PriorityButton
        label={t('从路由池移除')}
        disabled={false}
        onClick={() => props.onToggle(false)}
        icon={X}
        danger
      />
    </div>
  )
}

function AutoPoolRowMetrics(props: { item: MarketplaceAutoRoutePoolItem }) {
  const { t } = useTranslation()
  const item = props.item
  const hasMetrics = item.metrics_available
  const statusLabel = rowStatusLabel(item, t)
  return (
    <div className='mt-4 grid grid-cols-2 gap-2 xl:grid-cols-4'>
      <HighlightMetric
        label={t('成功率')}
        value={hasMetrics ? `${item.success_rate.toFixed(1)}%` : '-'}
        progress={hasMetrics ? item.success_rate : undefined}
        tone='success'
      />
      <HighlightMetric
        label={t('倍率')}
        value={`${item.multiplier}x`}
        tone='multiplier'
      />
      <Metric
        label={t('缓存命中')}
        value={hasMetrics ? `${item.cache_hit_rate.toFixed(1)}%` : '-'}
      />
      <Metric label={t('状态')} value={statusLabel} />
    </div>
  )
}

function AutoPoolRowMeta(props: { item: MarketplaceAutoRoutePoolItem }) {
  const { t } = useTranslation()
  const item = props.item
  return (
    <div className='text-muted-foreground mt-3 flex flex-wrap gap-x-4 gap-y-1 text-[11px]'>
      <span>
        {t('延迟')}:{' '}
        {item.metrics_available ? `${Math.round(item.avg_latency_ms)} ms` : '-'}
      </span>
      <span>
        {t('保守可用率')}: {item.availability.toFixed(1)}%
      </span>
    </div>
  )
}

function rowStatusLabel(
  item: MarketplaceAutoRoutePoolItem,
  translate: (key: string) => string
) {
  if (item.latest_request_status === 'healthy') return translate('稳定')
  if (item.latest_request_status === 'unstable') return translate('波动')
  if (item.latest_request_status === 'failed') return translate('失败')
  return item.source_type === 'official'
    ? translate('官方可用')
    : translate('待观测')
}

function HighlightMetric(props: {
  label: string
  value: string
  progress?: number
  tone: 'success' | 'multiplier'
}) {
  return (
    <div
      className={cn(
        'min-w-0 rounded-md px-2.5 py-2',
        props.tone === 'success'
          ? 'bg-emerald-500/10 text-emerald-800 dark:text-emerald-300'
          : 'bg-orange-500/10 text-orange-800 dark:text-orange-300'
      )}
    >
      <div className='text-[11px] font-medium opacity-80'>{props.label}</div>
      <div className='mt-0.5 text-lg font-semibold tabular-nums'>
        {props.value}
      </div>
      {props.progress != null && (
        <div className='bg-background/70 mt-1 h-1.5 overflow-hidden rounded-full'>
          <div
            className='h-full rounded-full bg-emerald-500 transition-[width] duration-300'
            aria-hidden='true'
            style={{ width: `${Math.max(0, Math.min(100, props.progress))}%` }}
          />
        </div>
      )}
    </div>
  )
}

function PriorityButton(props: {
  label: string
  disabled: boolean
  onClick: () => void
  icon: typeof ArrowUp
  danger?: boolean
}) {
  const Icon = props.icon
  return (
    <Button
      type='button'
      size='icon-sm'
      variant='ghost'
      disabled={props.disabled}
      onClick={(event) => {
        event.preventDefault()
        props.onClick()
      }}
      title={props.label}
      aria-label={props.label}
      className={props.danger ? 'text-destructive' : undefined}
    >
      <Icon className='size-4' />
    </Button>
  )
}

function Metric(props: { label: string; value: string }) {
  return (
    <div className='flex items-center justify-between gap-4 lg:block lg:text-right'>
      <span className='text-muted-foreground text-xs lg:block'>
        {props.label}
      </span>
      <span className='mt-0.5 font-semibold tabular-nums'>{props.value}</span>
    </div>
  )
}

export function AutoPoolSkeleton() {
  return (
    <div className='border-border space-y-2 rounded-lg border p-4'>
      <Skeleton className='h-32 w-full rounded-md' />
      {Array.from({ length: 5 }).map((_, index) => (
        <Skeleton key={index} className='h-20 w-full rounded-md' />
      ))}
    </div>
  )
}
