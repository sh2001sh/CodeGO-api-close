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

export function GroupMetrics({ group }: { group: MarketplaceGroup }) {
  const { t } = useTranslation()
  return (
    <div className='border-border mt-4 grid border-y md:grid-cols-2 xl:grid-cols-4'>
      <MetricSection icon={Gauge} title={t('调用质量')}>
        <RateLine
          label={t('成功率')}
          value={formatPercent(group.success_rate)}
          rate={group.success_rate}
        />
        <RecentRequestStatus group={group} />
      </MetricSection>
      <MetricSection
        icon={Timer}
        title={t('响应性能')}
        className='md:border-r-0 xl:border-r'
      >
        <ValueLine
          label={t('首字延迟')}
          value={formatDuration(group.avg_ttft_ms)}
          title={t('TTFT：从请求发出到收到首个输出内容的平均时间')}
        />
        <ValueLine
          label={t('总延迟')}
          value={formatDuration(group.avg_latency_ms)}
        />
        <ValueLine
          label={t('输出速度')}
          value={group.avg_tps ? `${group.avg_tps.toFixed(1)} TPS` : '--'}
        />
      </MetricSection>
      <MetricSection
        icon={CircleDollarSign}
        title={t('费用方式')}
        className='md:border-b-0'
      >
        <ValueLine
          label={t('余额倍率')}
          value={`${formatMultiplier(group.multiplier)}x`}
          strong
        />
        <ValueLine
          label={t('额度类型')}
          value={
            group.credit_pool_policy === 'universal'
              ? t('仅通用额度')
              : group.credit_pool_policy
          }
        />
      </MetricSection>
      <MetricSection
        icon={Activity}
        title={t('使用情况')}
        className='md:border-r-0 md:border-b-0'
      >
        <ValueLine
          label={t('请求数')}
          value={formatNumber(group.request_count)}
          strong
        />
        <ValueLine
          label={t('独立用户')}
          value={formatNumber(group.independent_consumers)}
        />
        <ValueLine
          label={t('缓存命中')}
          value={formatPercent(group.cache_hit_rate)}
        />
      </MetricSection>
    </div>
  )
}

function MetricSection(props: {
  icon: typeof Gauge
  title: string
  children: ReactNode
  className?: string
}) {
  const Icon = props.icon
  return (
    <section
      className={cn(
        'border-border min-w-0 border-b px-3 py-3.5 md:border-r xl:border-b-0',
        props.className
      )}
    >
      <div className='text-muted-foreground mb-2.5 flex items-center gap-1.5 text-xs font-medium'>
        <Icon className='text-primary size-3.5' />
        {props.title}
      </div>
      <div className='space-y-2'>{props.children}</div>
    </section>
  )
}

function RateLine(props: { label: string; value: string; rate: number }) {
  const rate = Math.max(0, Math.min(100, props.rate))
  return (
    <div>
      <ValueLine label={props.label} value={props.value} strong />
      <div className='bg-muted mt-1.5 h-1.5 overflow-hidden rounded-full'>
        <div
          className={cn('h-full rounded-full', rateTone(rate))}
          style={{ width: `${rate}%` }}
        />
      </div>
    </div>
  )
}

function RecentRequestStatus({ group }: { group: MarketplaceGroup }) {
  const { t } = useTranslation()
  const points = group.recent_request_series?.slice(-10) ?? []
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
    <div className='flex items-center gap-3'>
      <span
        className='text-muted-foreground shrink-0 text-[11px]'
        title={threshold}
      >
        {labels[group.latest_request_status]}
      </span>
      <div
        className='flex min-w-16 flex-1 gap-0.5'
        aria-label={`${t('近期请求状态')}。${threshold}`}
      >
        {(points.length ? points : Array.from({ length: 6 }, () => null)).map(
          (point, index) => (
            <span
              key={point ? `${point.ts}-${point.request_count}` : index}
              title={
                point
                  ? `${formatPercent(point.success_rate)} · ${formatNumber(point.request_count)} ${t('次请求')}`
                  : undefined
              }
              className={cn(
                'h-2.5 min-w-1 flex-1 rounded-sm',
                point ? rateTone(point.success_rate) : 'bg-muted'
              )}
            />
          )
        )}
      </div>
    </div>
  )
}

function rateTone(rate: number) {
  if (rate >= 90) return 'bg-success'
  if (rate >= 85) return 'bg-warning'
  return 'bg-destructive'
}

function ValueLine(props: {
  label: string
  value: string
  strong?: boolean
  title?: string
}) {
  return (
    <div className='flex items-start justify-between gap-3 text-xs'>
      <span className='text-muted-foreground' title={props.title}>
        {props.label}
      </span>
      <span
        className={cn(
          'text-right tabular-nums',
          props.strong ? 'text-foreground font-semibold' : 'font-medium'
        )}
      >
        {props.value}
      </span>
    </div>
  )
}
