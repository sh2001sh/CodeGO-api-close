import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'
import type { SidebarGroupModelStatusItem, SidebarGroupStatusBucket } from './types'
import { buildHealthSegments } from './presentation'

const SEGMENT_CLASS = {
  healthy: 'bg-success',
  slow: 'bg-warning',
  critical: 'bg-destructive',
  unknown: 'bg-muted',
} as const

export function HealthStrip(props: { item: SidebarGroupModelStatusItem }) {
  const segments = buildHealthSegments(props.item)
  const total = segments.length || 1
  const bucketSeconds =
    props.item.bucket_seconds ??
    inferBucketSeconds(props.item.series_window ?? props.item.sample_window, total)

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
                    'h-6 min-w-0 rounded transition-all hover:scale-110 hover:shadow-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1',
                    SEGMENT_CLASS[tone]
                  )}
                />
              }
            />
            <TooltipContent side='top' className='max-w-none'>
              <div className='space-y-0.5'>
                <div className='font-medium'>{formatBucketRange(bucket.ts, bucketSeconds)}</div>
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

      <div className='flex items-center gap-x-3 text-[10px] text-muted-foreground'>
        <LegendSwatch className={SEGMENT_CLASS.healthy} label='顺畅' />
        <LegendSwatch className={SEGMENT_CLASS.slow} label='缓慢' />
        <LegendSwatch className={SEGMENT_CLASS.critical} label='故障' />
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
  return new Intl.DateTimeFormat('zh-CN', {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }).format(date)
}

function buildBucketLabel(bucket: SidebarGroupStatusBucket, bucketSeconds: number) {
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
