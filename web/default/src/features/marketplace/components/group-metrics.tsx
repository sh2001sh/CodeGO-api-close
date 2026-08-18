import type { ReactNode } from 'react'
import { Activity, CircleDollarSign, Gauge, Timer } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import {
  formatDuration,
  formatMultiplier,
  formatNumber,
  formatPercent,
} from '../lib/format'
import type { MarketplaceGroup } from '../types'
import { RecentRequestStrip } from './recent-request-strip'

export function GroupMetrics({ group }: { group: MarketplaceGroup }) {
  const { t } = useTranslation()
  return (
    <div className='border-border mt-4 grid grid-cols-2 border-y sm:grid-cols-5'>
      <PrimaryMetric
        icon={Gauge}
        label={t('成功率')}
        value={formatPercent(group.success_rate)}
      >
        <RecentRequestStatus group={group} />
      </PrimaryMetric>
      <PrimaryMetric
        icon={Timer}
        label={t('首字延迟')}
        value={formatDuration(group.avg_ttft_ms)}
        title={t('TTFT：从请求发出到收到首个输出内容的平均时间')}
      />
      <PrimaryMetric
        icon={CircleDollarSign}
        label={t('额度倍率')}
        value={`${formatMultiplier(group.multiplier)}x`}
      />
      <PrimaryMetric
        icon={Activity}
        label={t('缓存命中率')}
        value={
          group.request_count > 0 ? `${group.cache_hit_rate.toFixed(1)}%` : '--'
        }
        title={t('近 24 小时请求中的缓存读取命中比例')}
      />
      <PrimaryMetric
        icon={Activity}
        label={t('当前 / 最大并发')}
        value={`${formatNumber(group.current_concurrency)} / ${formatConcurrencyLimit(group.max_concurrency, t('不限'))}`}
      >
        <div className='text-muted-foreground mt-1 text-[11px] tabular-nums'>
          {t('单用户上限')}:{' '}
          {formatConcurrencyLimit(group.user_max_concurrency, t('不限'))}
        </div>
      </PrimaryMetric>
    </div>
  )
}

function formatConcurrencyLimit(limit: number | undefined, unlimited: string) {
  return !limit ? unlimited : formatNumber(limit)
}

function PrimaryMetric(props: {
  icon: typeof Gauge
  label: string
  value: string
  title?: string
  children?: ReactNode
}) {
  const Icon = props.icon
  return (
    <div className='border-border min-w-0 border-r border-b px-3 py-3.5 even:border-r-0 sm:border-b-0 sm:last:border-r-0 sm:even:border-r'>
      <div className='text-muted-foreground flex items-center gap-1.5 text-xs'>
        <Icon className='size-3.5' />
        <span title={props.title}>{props.label}</span>
      </div>
      <div className='mt-1 text-base font-semibold tabular-nums'>
        {props.value}
      </div>
      {props.children}
    </div>
  )
}

function RecentRequestStatus({ group }: { group: MarketplaceGroup }) {
  const { t } = useTranslation()
  const labels = {
    healthy: t('近 6 小时稳定'),
    unstable: t('近 6 小时波动'),
    failed: t('近 6 小时异常'),
    unknown: t('暂无近期请求'),
  }
  const threshold = t(
    '最近非空时间段成功率：90%（含）以上稳定，85%（含）至 90%（不含）波动，低于 85% 异常'
  )
  return (
    <div className='mt-1 min-w-0'>
      <div className='flex min-w-0 items-center gap-2'>
        <span
          className='text-muted-foreground truncate text-[11px]'
          title={threshold}
        >
          {labels[group.latest_request_status]}
        </span>
        <span
          className={cn(
            'size-1.5 shrink-0 rounded-full',
            requestStatusTone(group.latest_request_status)
          )}
          aria-label={`${t('近期请求状态')}。${threshold}`}
        />
      </div>
      <RecentRequestStrip group={group} />
    </div>
  )
}

function requestStatusTone(status: MarketplaceGroup['latest_request_status']) {
  switch (status) {
    case 'healthy':
      return 'bg-success'
    case 'unstable':
      return 'bg-warning'
    case 'failed':
      return 'bg-destructive'
    default:
      return 'bg-muted-foreground'
  }
}
