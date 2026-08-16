import { Fragment } from 'react'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { TableCell, TableRow } from '@/components/ui/table'
import {
  formatDuration,
  formatMultiplier,
  formatNumber,
  formatPercent,
} from '../lib/format'
import type { MarketplaceGroup } from '../types'
import { GroupDetails } from './group-details'
import { ModelConsistencyBadge } from './model-verification'
import { MarketplaceStatusBadge } from './status-badge'

export function DesktopGroupRow(props: {
  group: MarketplaceGroup
  open: boolean
  onToggle: () => void
}) {
  const { t } = useTranslation()
  const group = props.group
  return (
    <Fragment>
      <TableRow aria-expanded={props.open} className='group/row'>
        <TableCell className='text-center'>
          <RankBadge group={group} />
        </TableCell>
        <TableCell>
          <GroupIdentity group={group} />
        </TableCell>
        <TableCell>
          <QualityCell group={group} />
        </TableCell>
        <TableCell>
          <ResponseCell group={group} />
        </TableCell>
        <TableCell className='text-right'>
          <div className='text-lg font-semibold tabular-nums'>
            {formatMultiplier(group.multiplier)}x
          </div>
          <div className='text-muted-foreground mt-0.5 text-[11px]'>
            {t('通用额度')}
          </div>
        </TableCell>
        <TableCell className='text-right'>
          <UsageCell group={group} />
        </TableCell>
        <TableCell>
          <Button
            variant='ghost'
            size='icon-sm'
            onClick={props.onToggle}
            aria-label={props.open ? t('收起详情') : t('展开详情')}
          >
            {props.open ? <ChevronDown /> : <ChevronRight />}
          </Button>
        </TableCell>
      </TableRow>
      {props.open && (
        <TableRow className='hover:bg-transparent'>
          <TableCell colSpan={7} className='bg-muted/15 p-0'>
            <GroupDetails group={group} />
          </TableCell>
        </TableRow>
      )}
    </Fragment>
  )
}

export function MobileGroupRow(props: {
  group: MarketplaceGroup
  open: boolean
  onToggle: () => void
}) {
  const { t } = useTranslation()
  const group = props.group
  return (
    <div className='px-4 py-4'>
      <button
        type='button'
        className='focus-visible:ring-ring w-full rounded-md text-left focus-visible:ring-2 focus-visible:outline-none'
        onClick={props.onToggle}
        aria-expanded={props.open}
      >
        <div className='flex items-start gap-3'>
          <RankBadge group={group} />
          <div className='min-w-0 flex-1'>
            <div className='flex items-start justify-between gap-3'>
              <div className='min-w-0'>
                <div className='truncate font-medium'>
                  {group.system_display_name}
                </div>
                <div className='text-muted-foreground mt-0.5 truncate text-xs'>
                  {group.provider_type}
                </div>
              </div>
              <div className='shrink-0 text-right'>
                <div className='text-lg font-semibold tabular-nums'>
                  {formatMultiplier(group.multiplier)}x
                </div>
                <div className='text-muted-foreground text-[11px]'>
                  {t('通用额度')}
                </div>
              </div>
            </div>
            <div className='mt-2 flex flex-wrap items-center gap-1.5'>
              <MarketplaceStatusBadge status={group.lifecycle_status} />
              <ModelConsistencyBadge status={group.model_consistency_status} />
              <ModelPreview models={group.models} compact />
            </div>
          </div>
        </div>
        <div className='bg-muted/25 mt-3 grid grid-cols-2 gap-y-2 rounded-lg px-3 py-2.5 text-xs sm:grid-cols-4'>
          <Metric
            label={t('成功率')}
            value={formatPercent(group.success_rate)}
          />
          <Metric label='TTFT' value={formatDuration(group.avg_ttft_ms)} />
          <Metric
            label={t('缓存命中率')}
            value={formatPercent(group.cache_hit_rate)}
          />
          <Metric label={t('请求')} value={formatNumber(group.request_count)} />
        </div>
        <div className='mt-3'>
          <RecentRequestStatus group={group} />
        </div>
      </button>
      {props.open && (
        <div className='pt-4'>
          <GroupDetails group={group} />
        </div>
      )}
    </div>
  )
}

function GroupIdentity(props: { group: MarketplaceGroup }) {
  const group = props.group
  return (
    <div>
      <div className='flex flex-wrap items-center gap-2'>
        <span className='font-medium'>{group.system_display_name}</span>
        <MarketplaceStatusBadge status={group.lifecycle_status} />
        <ModelConsistencyBadge status={group.model_consistency_status} />
      </div>
      <div className='text-muted-foreground mt-1 text-xs'>
        {group.provider_type}
      </div>
      <div className='mt-2'>
        <ModelPreview models={group.models} />
      </div>
    </div>
  )
}

function ModelPreview(props: { models: string[]; compact?: boolean }) {
  const visible = props.models.slice(0, props.compact ? 1 : 2)
  const remaining = props.models.length - visible.length
  return (
    <div className='flex flex-wrap gap-1'>
      {visible.map((model) => (
        <Badge key={model} variant='secondary' className='max-w-40 truncate'>
          {model}
        </Badge>
      ))}
      {remaining > 0 && <Badge variant='outline'>+{remaining}</Badge>}
    </div>
  )
}

function QualityCell(props: { group: MarketplaceGroup }) {
  const { t } = useTranslation()
  const group = props.group
  const rate = Math.max(0, Math.min(100, group.success_rate))
  return (
    <div className='space-y-2.5'>
      <div className='flex items-center justify-between gap-3 text-xs'>
        <span className='text-muted-foreground'>{t('成功率')}</span>
        <span className='font-medium tabular-nums'>{formatPercent(rate)}</span>
      </div>
      <div className='bg-muted mt-2 h-1.5 overflow-hidden rounded-full'>
        <div
          className='h-full rounded-full bg-emerald-500'
          style={{ width: `${rate}%` }}
        />
      </div>
      <RecentRequestStatus group={group} />
      <div className='flex items-center justify-between gap-3 text-[11px]'>
        <span className='text-muted-foreground'>{t('缓存命中率')}</span>
        <span className='font-medium tabular-nums'>
          {formatPercent(group.cache_hit_rate)}
        </span>
      </div>
    </div>
  )
}

function RecentRequestStatus(props: { group: MarketplaceGroup }) {
  const { t } = useTranslation()
  const points = props.group.recent_request_series?.slice(-10) ?? []
  const statusLabels = {
    healthy: t('近期稳定'),
    unstable: t('近期波动'),
    failed: t('近期异常'),
    unknown: t('暂无近期请求'),
  }

  return (
    <div className='flex items-center justify-between gap-3'>
      <span className='text-muted-foreground shrink-0 text-[11px]'>
        {statusLabels[props.group.latest_request_status]}
      </span>
      <div
        className='flex min-w-16 flex-1 justify-end gap-0.5'
        aria-label={t('近期请求状态')}
      >
        {points.length === 0
          ? Array.from({ length: 6 }).map((_, index) => (
              <span
                key={index}
                className='bg-muted h-2.5 min-w-1 flex-1 rounded-sm'
              />
            ))
          : points.map((point) => (
              <span
                key={`${point.ts}-${point.request_count}`}
                title={`${formatPercent(point.success_rate)} · ${formatNumber(point.request_count)} ${t('次请求')}`}
                className={cn(
                  'h-2.5 min-w-1 flex-1 rounded-sm',
                  point.success_rate >= 99 && 'bg-emerald-500',
                  point.success_rate >= 85 &&
                    point.success_rate < 99 &&
                    'bg-amber-500',
                  point.success_rate < 85 && 'bg-rose-500'
                )}
              />
            ))}
      </div>
    </div>
  )
}

function ResponseCell(props: { group: MarketplaceGroup }) {
  const { t } = useTranslation()
  return (
    <div className='space-y-1.5 text-xs'>
      <ValueLine label='TTFT' value={formatDuration(props.group.avg_ttft_ms)} />
      <ValueLine
        label={t('总延迟')}
        value={formatDuration(props.group.avg_latency_ms)}
      />
    </div>
  )
}

function UsageCell(props: { group: MarketplaceGroup }) {
  const { t } = useTranslation()
  return (
    <div className='space-y-1 text-xs'>
      <div className='font-medium tabular-nums'>
        {formatNumber(props.group.request_count)} {t('次请求')}
      </div>
      <div className='text-muted-foreground tabular-nums'>
        {props.group.avg_tps ? `${props.group.avg_tps.toFixed(1)} TPS` : '--'}
      </div>
    </div>
  )
}

function RankBadge(props: { group: MarketplaceGroup }) {
  const rank = props.group.observing ? 0 : props.group.rank
  return (
    <span
      className={cn(
        'inline-flex size-9 shrink-0 items-center justify-center rounded-lg text-sm font-semibold tabular-nums',
        rank === 1 && 'bg-amber-500/15 text-amber-700 dark:text-amber-300',
        rank === 2 && 'bg-slate-500/15 text-slate-700 dark:text-slate-300',
        rank === 3 && 'bg-orange-500/15 text-orange-700 dark:text-orange-300',
        (rank === 0 || rank > 3) && 'bg-muted text-muted-foreground'
      )}
    >
      {rank || '·'}
    </span>
  )
}

function ValueLine(props: { label: string; value: string }) {
  return (
    <div className='flex justify-between gap-3'>
      <span className='text-muted-foreground'>{props.label}</span>
      <span className='font-medium tabular-nums'>{props.value}</span>
    </div>
  )
}

function Metric(props: { label: string; value: string }) {
  return (
    <div className='min-w-0 text-center'>
      <div className='text-muted-foreground'>{props.label}</div>
      <div className='mt-0.5 truncate font-medium tabular-nums'>
        {props.value}
      </div>
    </div>
  )
}
