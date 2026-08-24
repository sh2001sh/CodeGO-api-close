/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { cn } from '@/lib/utils'
import { Card, CardContent } from '@/components/ui/card'
import { HealthStrip } from './health-strip'
import {
  formatRequestCount,
  formatSampleWindowLabel,
  getStatusMeta,
} from './presentation'
import type { SidebarGroupModelStatusItem } from './types'

export function GroupStatusMonitorCard(props: {
  item: SidebarGroupModelStatusItem
}) {
  const meta = getStatusMeta(props.item.status)
  const seriesWindowLabel = formatSampleWindowLabel(
    props.item.series_window ?? props.item.sample_window
  )
  const sampleWindowLabel = formatSampleWindowLabel(props.item.sample_window)

  return (
    <Card
      size='sm'
      className='group bg-card hover:border-primary/30 overflow-hidden border py-0 transition-colors'
    >
      <CardContent className='px-4 py-3.5'>
        <div className='space-y-3'>
          {/* Header: Model name and status badge */}
          <div className='flex items-start justify-between gap-2'>
            <div className='flex min-w-0 flex-1 items-center gap-2'>
              <div className={cn('size-1.5 shrink-0 rounded-full', meta.dot)} />
              <h4
                className='text-foreground min-w-0 flex-1 truncate font-mono text-[13px] font-semibold'
                title={props.item.model}
              >
                {props.item.model}
              </h4>
            </div>
            <div
              className={cn(
                'shrink-0 rounded-[4px] px-2 py-0.5 text-[10px] font-bold tracking-wide uppercase',
                meta.accentText,
                meta.badgeBg
              )}
            >
              {meta.label}
            </div>
          </div>

          <div className='divide-border/60 grid grid-cols-3 divide-x py-0.5'>
            <Metric label='成功率' value={props.item.success_rate} />
            <Metric label='缓存命中率' value={props.item.cache_hit_rate} />
            <RequestMetric
              label={`${sampleWindowLabel}请求`}
              value={props.item.request_count}
            />
          </div>

          {/* Time range label */}
          <div className='text-muted-foreground text-[11px]'>
            {seriesWindowLabel}
          </div>

          {/* Health strip */}
          <div className='pt-1'>
            <HealthStrip item={props.item} />
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

function Metric(props: { label: string; value?: number | null }) {
  return (
    <div className='px-3 first:pl-0'>
      <div className='text-muted-foreground text-[11px]'>{props.label}</div>
      <div className='app-numeric mt-0.5 text-base font-semibold'>
        {props.value == null ? '--' : `${props.value.toFixed(1)}%`}
      </div>
    </div>
  )
}

function RequestMetric(props: { label: string; value?: number }) {
  return (
    <div className='px-2 first:pl-0'>
      <div
        className='text-muted-foreground truncate text-[11px]'
        title={props.label}
      >
        {props.label}
      </div>
      <div className='app-numeric mt-0.5 text-base font-semibold'>
        {formatRequestCount(props.value)}
      </div>
    </div>
  )
}
