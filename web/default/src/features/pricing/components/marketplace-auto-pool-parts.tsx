import {
  ArrowDown,
  ArrowUp,
  ListFilter,
  Plus,
  RotateCcw,
  Search,
  X,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import type { MarketplaceAutoRoutePoolItem } from '@/features/marketplace/types'

export type AutoPoolSort =
  | 'route'
  | 'multiplier'
  | 'success'
  | 'cache'
  | 'latency'

export function AutoPoolFilters(props: {
  search: string
  onSearchChange: (value: string) => void
  sourceFilter: string
  onSourceFilterChange: (value: string) => void
  sourceOptions: string[]
  modelFilter: string
  onModelFilterChange: (value: string) => void
  modelOptions: string[]
  sortBy: AutoPoolSort
  onSortChange: (value: AutoPoolSort) => void
  onReset: () => void
}) {
  const { t } = useTranslation()
  const hasFilters =
    props.search.trim() !== '' ||
    props.sourceFilter !== 'all' ||
    props.modelFilter !== 'all' ||
    props.sortBy !== 'route'
  return (
    <div className='space-y-3'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div className='flex items-center gap-2'>
          <ListFilter className='text-primary size-4' aria-hidden='true' />
          <span className='text-sm font-medium'>{t('筛选与排序')}</span>
        </div>
        {hasFilters && (
          <Button
            type='button'
            variant='ghost'
            size='sm'
            className='text-muted-foreground h-8 gap-1.5 px-2 text-xs'
            onClick={props.onReset}
          >
            <RotateCcw className='size-3.5' />
            {t('清除筛选')}
          </Button>
        )}
      </div>
      <div className='grid gap-2 sm:grid-cols-2 xl:grid-cols-[minmax(220px,1.4fr)_minmax(150px,0.7fr)_minmax(180px,0.8fr)_minmax(180px,0.8fr)]'>
        <LabeledControl label={t('搜索')}>
          <div className='relative'>
            <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2' />
            <Input
              value={props.search}
              onChange={(event) => props.onSearchChange(event.target.value)}
              placeholder={t('分组、来源或模型')}
              aria-label={t('搜索分组、来源或模型')}
              className='h-10 pl-9'
            />
          </div>
        </LabeledControl>
        <LabeledControl label={t('来源')}>
          <PoolSelect
            value={props.sourceFilter}
            onChange={props.onSourceFilterChange}
            placeholder={t('选择来源')}
            allLabel={t('全部来源')}
            options={props.sourceOptions}
          />
        </LabeledControl>
        <LabeledControl label={t('模型')}>
          <PoolSelect
            value={props.modelFilter}
            onChange={props.onModelFilterChange}
            placeholder={t('选择模型')}
            allLabel={t('全部模型')}
            options={props.modelOptions}
          />
        </LabeledControl>
        <LabeledControl label={t('排序方式')}>
          <Select
            value={props.sortBy}
            onValueChange={(value) =>
              props.onSortChange((value || 'route') as AutoPoolSort)
            }
          >
            <SelectTrigger className='h-10 w-full'>
              <SelectValue placeholder={t('选择排序方式')} />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value='route'>{t('推荐路由顺序')}</SelectItem>
              <SelectItem value='success'>{t('成功率：高到低')}</SelectItem>
              <SelectItem value='multiplier'>{t('倍率：低到高')}</SelectItem>
              <SelectItem value='cache'>{t('缓存命中率：高到低')}</SelectItem>
              <SelectItem value='latency'>{t('延迟：低到高')}</SelectItem>
            </SelectContent>
          </Select>
        </LabeledControl>
      </div>
    </div>
  )
}

function LabeledControl(props: { label: string; children: React.ReactNode }) {
  return (
    <label className='min-w-0 space-y-1'>
      <span className='text-muted-foreground block text-[11px] font-medium'>
        {props.label}
      </span>
      {props.children}
    </label>
  )
}

function PoolSelect(props: {
  value: string
  onChange: (value: string) => void
  placeholder: string
  allLabel: string
  options: string[]
}) {
  return (
    <Select
      value={props.value}
      onValueChange={(value) => props.onChange(value || 'all')}
    >
      <SelectTrigger className='h-10 w-full'>
        <SelectValue placeholder={props.placeholder} />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value='all'>{props.allLabel}</SelectItem>
        {props.options.map((option) => (
          <SelectItem key={option} value={option}>
            {option}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

export function AutoPoolRow(props: {
  item: MarketplaceAutoRoutePoolItem
  selected: boolean
  order: number
  onToggle: (checked: boolean) => void
  onMoveUp: () => void
  onMoveDown: () => void
  canMoveUp: boolean
  canMoveDown: boolean
}) {
  const { t } = useTranslation()
  const item = props.item
  const hasMetrics = item.metrics_available
  const statusLabel =
    item.latest_request_status === 'healthy'
      ? t('稳定')
      : item.latest_request_status === 'unstable'
        ? t('波动')
        : item.latest_request_status === 'failed'
          ? t('失败')
          : item.source_type === 'official'
            ? t('官方可用')
            : t('待观测')
  return (
    <div
      className={cn(
        'group transition-colors',
        props.selected
          ? 'hover:bg-muted/20 grid gap-3 px-4 py-4 lg:px-5'
          : 'border-border bg-background hover:border-primary/50 hover:bg-muted/15 rounded-lg border p-4'
      )}
    >
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
            {props.selected && (
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
            )}
            {!props.selected && (
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
            )}
          </div>
          <div className='text-muted-foreground mt-1 truncate text-xs'>
            {item.source_type === 'official'
              ? t('官方分组')
              : item.source_label || t('来源待审核')}{' '}
            · {item.models.slice(0, 3).join(' / ')}
          </div>
        </div>
      </div>
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
      <div className='text-muted-foreground mt-3 flex flex-wrap gap-x-4 gap-y-1 text-[11px]'>
        <span>
          {t('延迟')}:{' '}
          {hasMetrics ? `${Math.round(item.avg_latency_ms)} ms` : '-'}
        </span>
        <span>
          {t('保守可用率')}: {item.availability.toFixed(1)}%
        </span>
      </div>
    </div>
  )
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
