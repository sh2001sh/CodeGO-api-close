/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useMemo } from 'react'
import { Link } from '@tanstack/react-router'
import { Activity, ArrowUpRight, HeartPulse } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Skeleton } from '@/components/ui/skeleton'
import { getStatusMeta } from '@/features/sidebar-group-status/presentation'
import { useSidebarGroupStatus } from '@/features/sidebar-group-status/use-sidebar-group-status'
import {
  buildOverviewModelStatus,
  type OverviewModelStatus,
  type OverviewModelStatusRow,
} from './overview-health'

function formatRequests(count: number) {
  if (!Number.isFinite(count) || count <= 0) return '—'
  return new Intl.NumberFormat('zh-CN', {
    notation: 'compact',
    maximumFractionDigits: 1,
  }).format(count)
}

function MetricCell(props: {
  icon: typeof Activity
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

function ModelRow(props: { row: OverviewModelStatusRow }) {
  const meta = getStatusMeta(props.row.status)
  return (
    <div className='overview-soft-card grid gap-3 px-3.5 py-3 sm:grid-cols-[minmax(0,1.5fr)_minmax(84px,0.7fr)_minmax(74px,0.6fr)_auto] sm:items-center sm:px-4'>
      <div className='flex min-w-0 items-center gap-2.5'>
        <span className={cn('size-2 shrink-0 rounded-full', meta.accent)} />
        <div className='min-w-0'>
          <div
            className='text-foreground truncate text-sm font-semibold'
            title={props.row.model}
          >
            {props.row.model}
          </div>
          <div className='text-muted-foreground mt-0.5 truncate text-[10px]'>
            {props.row.group}
          </div>
        </div>
      </div>
      <MetricCell
        icon={HeartPulse}
        label='成功率'
        value={
          props.row.success_rate == null
            ? '—'
            : `${props.row.success_rate.toFixed(1)}%`
        }
      />
      <MetricCell
        icon={Activity}
        label='请求数'
        value={formatRequests(props.row.request_count ?? 0)}
      />
      <span
        className={cn(
          'w-fit rounded-md px-2 py-1 text-[10px] font-medium whitespace-nowrap',
          meta.accentText,
          meta.badgeBg
        )}
      >
        {meta.label}
      </span>
    </div>
  )
}

function getHealthSummaryText(status: OverviewModelStatus) {
  if (status.activeModelCount === 0) return '暂无活跃请求样本'
  return `${status.healthyModelCount}/${status.activeModelCount} 个活跃模型稳定`
}

function HealthPanelHeader(props: { status: OverviewModelStatus }) {
  return (
    <div className='flex flex-wrap items-start justify-between gap-3'>
      <div>
        <div className='text-muted-foreground text-[11px] font-medium tracking-[0.16em] uppercase'>
          模型健康概览
        </div>
        <div className='text-foreground mt-1 text-xl font-semibold tracking-tight'>
          今天的主要状态
        </div>
        <div className='text-muted-foreground mt-1 text-xs'>
          近 6 小时最新非空 30 分钟请求桶，按请求量展示主要模型
        </div>
      </div>
      <div className='flex flex-col items-end gap-2 text-xs'>
        <div className='flex items-center gap-2'>
          <span
            className={cn(
              'size-2 rounded-full',
              props.status.activeModelCount > 0
                ? 'bg-success'
                : 'bg-muted-foreground'
            )}
          />
          <span className='text-muted-foreground'>
            {getHealthSummaryText(props.status)}
          </span>
        </div>
        <Link
          to='/group-status'
          className='text-primary inline-flex items-center gap-1 font-medium hover:underline'
        >
          查看分组状态
          <ArrowUpRight className='size-3' aria-hidden='true' />
        </Link>
      </div>
    </div>
  )
}

function HealthPanelContent(props: {
  loading: boolean
  error: boolean
  rows: OverviewModelStatusRow[]
}) {
  return (
    <>
      <div className='text-muted-foreground mt-4 hidden grid-cols-[minmax(0,1.5fr)_minmax(84px,0.7fr)_minmax(74px,0.6fr)_auto] gap-3 px-4 text-[10px] font-medium tracking-wide uppercase sm:grid'>
        <span>模型 / 分组</span>
        <span>成功率</span>
        <span>请求数</span>
        <span>状态</span>
      </div>
      <div className='mt-2 grid gap-2.5'>
        {props.loading &&
          Array.from({ length: 3 }).map((_, index) => (
            <Skeleton key={index} className='h-[68px] rounded-xl' />
          ))}
        {props.error && (
          <div className='border-destructive/20 bg-destructive/5 text-destructive rounded-xl border px-4 py-3 text-sm'>
            模型状态暂时无法加载，请稍后重试。
          </div>
        )}
        {!props.loading && !props.error && props.rows.length === 0 && (
          <div className='border-border/70 text-muted-foreground rounded-xl border border-dashed px-4 py-6 text-center text-sm'>
            暂无可展示的模型状态。
          </div>
        )}
        {!props.loading &&
          !props.error &&
          props.rows.map((row) => (
            <ModelRow key={`${row.group}-${row.model}`} row={row} />
          ))}
      </div>
    </>
  )
}

export function OverviewHealthPanel() {
  const statusQuery = useSidebarGroupStatus()
  const status = useMemo(
    () => buildOverviewModelStatus(statusQuery.data?.data ?? []),
    [statusQuery.data]
  )

  return (
    <section className='overview-glass-card p-5 sm:p-6'>
      <HealthPanelHeader status={status} />
      <HealthPanelContent
        loading={statusQuery.isLoading}
        error={statusQuery.isError}
        rows={status.rows}
      />
    </section>
  )
}
