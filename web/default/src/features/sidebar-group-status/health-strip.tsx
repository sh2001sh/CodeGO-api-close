import { useMemo } from 'react'
import { cn } from '@/lib/utils'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { buildHealthSegments } from './presentation'
import type {
  SidebarGroupModelStatusItem,
  SidebarGroupStatusBucket,
} from './types'

const SEGMENT_CLASS = {
  healthy: 'bg-success',
  unstable: 'bg-warning',
  failed: 'bg-destructive',
  unknown: 'bg-muted',
} as const

const TIME_FORMATTER = new Intl.DateTimeFormat('zh-CN', {
  hour: '2-digit',
  minute: '2-digit',
  hour12: false,
})

export function HealthStrip(props: { item: SidebarGroupModelStatusItem }) {
  const segments = useMemo(() => buildHealthSegments(props.item), [props.item])
  const total = segments.length || 1
  const bucketSeconds =
    props.item.bucket_seconds ??
    inferBucketSeconds(
      props.item.series_window ?? props.item.sample_window,
      total
    )

  return (
    <div className='space-y-2'>
      <div className='flex w-full gap-1'>
        {segments.map(({ bucket, tone }, index) => (
          <Tooltip key={`${props.item.model}-${bucket.ts}-${index}`}>
            <TooltipTrigger
              render={
                <button
                  type='button'
                  aria-label={buildBucketLabel(bucket, bucketSeconds)}
                  style={{ flex: '1 1 0%' }}
                  className={cn(
                    'focus-visible:ring-ring h-6 min-w-0 rounded transition-all hover:scale-110 hover:shadow-md focus-visible:ring-2 focus-visible:ring-offset-1 focus-visible:outline-none',
                    SEGMENT_CLASS[tone]
                  )}
                />
              }
            />
            <TooltipContent side='top' className='max-w-none'>
              <div className='space-y-0.5'>
                <div className='font-medium'>
                  {formatBucketRange(bucket.ts, bucketSeconds)}
                </div>
                <div className='text-background/80 text-xs'>
                  {bucket.request_count > 0 && bucket.success_rate != null
                    ? `成功率 ${bucket.success_rate.toFixed(1)}%`
                    : '该时间段暂无请求样本'}
                </div>
              </div>
            </TooltipContent>
          </Tooltip>
        ))}
      </div>

      <div className='text-muted-foreground flex items-center gap-x-3 text-[10px]'>
        <LegendSwatch className={SEGMENT_CLASS.healthy} label='稳定' />
        <LegendSwatch className={SEGMENT_CLASS.unstable} label='波动' />
        <LegendSwatch className={SEGMENT_CLASS.failed} label='异常' />
        <LegendSwatch className={SEGMENT_CLASS.unknown} label='无样本' />
      </div>
    </div>
  )
}

function LegendSwatch(props: { className: string; label: string }) {
  return (
    <div className='flex items-center gap-1.5'>
      <span className={cn('h-2.5 w-2.5 rounded-full', props.className)} />
      <span>{props.label}</span>
    </div>
  )
}

function formatBucketRange(ts: number, bucketSeconds: number) {
  const start = new Date(ts * 1000)
  const end = new Date((ts + bucketSeconds) * 1000)
  return `${formatTime(start)} - ${formatTime(end)}`
}

function formatTime(date: Date) {
  return TIME_FORMATTER.format(date)
}

function buildBucketLabel(
  bucket: SidebarGroupStatusBucket,
  bucketSeconds: number
) {
  const range = formatBucketRange(bucket.ts, bucketSeconds)
  if (bucket.request_count <= 0 || bucket.success_rate == null) {
    return `${range}，暂无请求样本`
  }
  return `${range}，成功率 ${bucket.success_rate.toFixed(1)}%`
}

function inferBucketSeconds(sampleWindowHours: number, segmentCount: number) {
  const totalSeconds = Math.max(1, Math.round(sampleWindowHours * 3600))
  return Math.max(60, Math.round(totalSeconds / Math.max(segmentCount, 1)))
}
