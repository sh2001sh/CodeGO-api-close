/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU Affero
General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Activity, Gauge, HeartPulse, RadioTower, Timer } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Skeleton } from '@/components/ui/skeleton'
import { getUserGroupOverview, getUptimeStatus } from '@/features/dashboard/api'
import type { UserGroupOverviewItem } from '@/features/dashboard/types'
import {
  formatLatency,
  formatThroughput,
  formatUptimePct,
} from '@/features/performance-metrics/lib/format'

const PERFORMANCE_WINDOW_HOURS = 24

function resolveGroupStatus(successRate: number | null) {
  if (successRate == null)
    return { label: '观测中', className: 'text-muted-foreground bg-muted' }
  if (successRate >= 99.9)
    return { label: '运行正常', className: 'text-success bg-success/10' }
  if (successRate >= 99)
    return { label: '轻微波动', className: 'text-warning bg-warning/10' }
  return { label: '需要关注', className: 'text-destructive bg-destructive/10' }
}

function formatRequests(count: number) {
  if (!Number.isFinite(count) || count <= 0) return '—'
  return new Intl.NumberFormat('zh-CN', {
    notation: 'compact',
    maximumFractionDigits: 1,
  }).format(count)
}

function MetricCell(props: {
  icon: typeof Timer
  label: string
  value: string
}) {
  const Icon = props.icon
  return (
    <div className='flex min-w-0 items-center gap-2'>
      <Icon
        className='text-muted-foreground size-3.5 shrink-0'
        aria-hidden='true'
      />
      <div className='min-w-0'>
        <div className='text-muted-foreground text-[10px] leading-4'>
          {props.label}
        </div>
        <div className='text-foreground truncate text-sm font-semibold tabular-nums'>
          {props.value}
        </div>
      </div>
    </div>
  )
}

function GroupRow({ item }: { item: UserGroupOverviewItem }) {
  const status = resolveGroupStatus(item.success_rate)
  return (
    <div className='overview-soft-card grid gap-3 px-3.5 py-3 sm:grid-cols-[minmax(130px,1.2fr)_repeat(5,minmax(0,1fr))] sm:items-center sm:px-4'>
      <div className='flex min-w-0 items-center justify-between gap-3 sm:block'>
        <div className='flex min-w-0 items-center gap-2'>
          <span
            className={cn(
              'size-2 shrink-0 rounded-full',
              item.success_rate == null
                ? 'bg-muted-foreground/50'
                : item.success_rate >= 99
                  ? 'bg-success'
                  : 'bg-destructive'
            )}
          />
          <span
            className='text-foreground truncate text-sm font-semibold'
            title={item.group}
          >
            {item.group}
          </span>
        </div>
        <span
          className={cn(
            'rounded-md px-2 py-1 text-[10px] font-medium whitespace-nowrap sm:mt-1.5 sm:inline-block',
            status.className
          )}
        >
          {status.label}
        </span>
      </div>
      <MetricCell
        icon={HeartPulse}
        label='成功率'
        value={
          item.success_rate == null ? '—' : `${item.success_rate.toFixed(2)}%`
        }
      />
      <MetricCell
        icon={Timer}
        label='平均延迟'
        value={formatLatency(item.avg_latency_ms ?? NaN)}
      />
      <MetricCell
        icon={Gauge}
        label='平均吞吐'
        value={formatThroughput(item.avg_tps ?? NaN)}
      />
      <MetricCell
        icon={Activity}
        label='请求数'
        value={formatRequests(item.request_count)}
      />
      <MetricCell
        icon={RadioTower}
        label='活跃模型'
        value={
          item.active_model_count > 0 ? `${item.active_model_count} 个` : '—'
        }
      />
    </div>
  )
}

export function OverviewHealthPanel() {
  const overviewQuery = useQuery({
    queryKey: ['dashboard', 'group-overview', PERFORMANCE_WINDOW_HOURS],
    queryFn: () => getUserGroupOverview(PERFORMANCE_WINDOW_HOURS),
    staleTime: 60 * 1000,
    retry: false,
  })
  const uptimeQuery = useQuery({
    queryKey: ['dashboard', 'uptime-status', 'overview'],
    queryFn: getUptimeStatus,
    staleTime: 60 * 1000,
  })
  const groups = useMemo(
    () => overviewQuery.data?.data ?? [],
    [overviewQuery.data]
  )
  const uptimeMonitors =
    uptimeQuery.data?.data?.flatMap((group) => group.monitors ?? []) ?? []
  const uptimeAverage =
    uptimeMonitors.length > 0
      ? uptimeMonitors.reduce(
          (sum, monitor) => sum + Number(monitor.uptime ?? 0),
          0
        ) / uptimeMonitors.length
      : NaN

  return (
    <section className='overview-glass-card p-5 sm:p-6'>
      <div className='flex items-start justify-between gap-3'>
        <div>
          <div className='text-muted-foreground text-[11px] font-medium tracking-[0.16em] uppercase'>
            分组健康概览
          </div>
          <div className='text-foreground mt-1 text-xl font-semibold tracking-tight'>
            今天的主要状态
          </div>
          <div className='text-muted-foreground mt-1 text-xs'>
            按分组展示最近 24 小时的请求表现
          </div>
        </div>
        <div className='flex items-center gap-2 text-xs'>
          <span className='bg-success size-2 rounded-full' />
          <span className='text-muted-foreground'>
            {Number.isFinite(uptimeAverage)
              ? `可用率 ${formatUptimePct(uptimeAverage * 100)}`
              : '可用率 —'}
          </span>
        </div>
      </div>

      <div className='text-muted-foreground mt-4 hidden grid-cols-[minmax(130px,1.2fr)_repeat(5,minmax(0,1fr))] gap-3 px-4 text-[10px] font-medium tracking-wide uppercase sm:grid'>
        <span>分组</span>
        <span>成功率</span>
        <span>平均延迟</span>
        <span>平均吞吐</span>
        <span>请求数</span>
        <span>活跃模型</span>
      </div>
      <div className='mt-2 grid gap-2.5'>
        {overviewQuery.isLoading &&
          Array.from({ length: 2 }).map((_, index) => (
            <Skeleton key={index} className='h-[76px] rounded-xl' />
          ))}
        {overviewQuery.isError && (
          <div className='border-destructive/20 bg-destructive/5 text-destructive rounded-xl border px-4 py-3 text-sm'>
            分组状态暂时无法加载，请稍后重试。
          </div>
        )}
        {!overviewQuery.isLoading &&
          !overviewQuery.isError &&
          groups.length === 0 && (
            <div className='border-border/70 text-muted-foreground rounded-xl border border-dashed px-4 py-6 text-center text-sm'>
              暂无可展示的分组状态。
            </div>
          )}
        {!overviewQuery.isLoading &&
          !overviewQuery.isError &&
          groups.map((item) => <GroupRow key={item.group} item={item} />)}
      </div>
    </section>
  )
}
