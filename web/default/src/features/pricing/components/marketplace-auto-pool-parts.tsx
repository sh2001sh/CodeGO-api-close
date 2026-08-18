import { ArrowDown, ArrowUp, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
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
}) {
  const { t } = useTranslation()
  return (
    <div className='flex flex-col gap-2 xl:flex-row xl:items-center'>
      <div className='relative w-full xl:max-w-sm'>
        <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2' />
        <Input
          value={props.search}
          onChange={(event) => props.onSearchChange(event.target.value)}
          placeholder={t('搜索分组、来源或模型')}
          className='pl-9'
        />
      </div>
      <PoolSelect
        value={props.sourceFilter}
        onChange={props.onSourceFilterChange}
        placeholder={t('按来源筛选')}
        allLabel={t('全部来源')}
        options={props.sourceOptions}
        className='xl:w-44'
      />
      <PoolSelect
        value={props.modelFilter}
        onChange={props.onModelFilterChange}
        placeholder={t('按模型筛选')}
        allLabel={t('全部模型')}
        options={props.modelOptions}
        className='xl:w-52'
      />
      <Select
        value={props.sortBy}
        onValueChange={(value) =>
          props.onSortChange((value || 'route') as AutoPoolSort)
        }
      >
        <SelectTrigger className='w-full xl:w-44'>
          <SelectValue placeholder={t('排序')} />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value='route'>{t('推荐路由顺序')}</SelectItem>
          <SelectItem value='multiplier'>{t('倍率从低到高')}</SelectItem>
          <SelectItem value='success'>{t('成功率从高到低')}</SelectItem>
          <SelectItem value='cache'>{t('缓存命中率从高到低')}</SelectItem>
          <SelectItem value='latency'>{t('延迟从低到高')}</SelectItem>
        </SelectContent>
      </Select>
    </div>
  )
}

function PoolSelect(props: {
  value: string
  onChange: (value: string) => void
  placeholder: string
  allLabel: string
  options: string[]
  className: string
}) {
  return (
    <Select
      value={props.value}
      onValueChange={(value) => props.onChange(value || 'all')}
    >
      <SelectTrigger className={`w-full ${props.className}`}>
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
    <label className='hover:bg-muted/20 grid cursor-pointer gap-3 px-4 py-4 transition-colors lg:grid-cols-[28px_minmax(220px,1.4fr)_90px_100px_100px_100px_100px_100px] lg:items-center lg:px-5'>
      <Checkbox checked={props.selected} onCheckedChange={props.onToggle} />
      <div className='min-w-0'>
        <div className='flex items-center gap-2'>
          {props.order > 0 && (
            <span className='bg-primary/10 text-primary inline-flex size-6 items-center justify-center rounded-md text-xs font-semibold tabular-nums'>
              {props.order}
            </span>
          )}
          <span className='truncate font-medium'>
            {item.system_display_name}
          </span>
        </div>
        <div className='text-muted-foreground mt-1 truncate text-xs'>
          {item.source_type === 'official'
            ? t('官方分组')
            : item.source_label || t('来源待审核')}{' '}
          · {item.models.slice(0, 3).join(' / ')}
        </div>
      </div>
      <Metric label={t('状态')} value={statusLabel} />
      <Metric
        label={t('成功率')}
        value={hasMetrics ? `${item.success_rate.toFixed(1)}%` : '-'}
      />
      <Metric
        label={t('缓存命中')}
        value={hasMetrics ? `${item.cache_hit_rate.toFixed(1)}%` : '-'}
      />
      <Metric
        label={t('延迟')}
        value={hasMetrics ? `${Math.round(item.avg_latency_ms)} ms` : '-'}
      />
      <Metric label={t('倍率')} value={`${item.multiplier}x`} />
      <Metric
        label={t('保守可用率')}
        value={`${item.availability.toFixed(1)}%`}
      />
      {props.selected && (
        <div className='flex justify-end gap-1 lg:col-span-7 lg:col-start-2'>
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
        </div>
      )}
    </label>
  )
}

function PriorityButton(props: {
  label: string
  disabled: boolean
  onClick: () => void
  icon: typeof ArrowUp
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
