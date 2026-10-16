import { useMemo } from 'react'
import i18n from '@/i18n/config'
import { getIntlLocale } from '@/i18n/format'
import { useTranslation } from 'react-i18next'
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

const timeFormatter = () =>
  new Intl.DateTimeFormat(getIntlLocale(), {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })

export function HealthStrip(props: { item: SidebarGroupModelStatusItem }) {
  useTranslation()
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
      <div className='status-health-strip flex w-full gap-1'>
        {segments.map(({ bucket, tone }, index) => (
          <Tooltip key={`${props.item.model}-${bucket.ts}-${index}`}>
            <TooltipTrigger
              render={
                <button
                  type='button'
                  aria-label={buildBucketLabel(bucket, bucketSeconds)}
                  data-tone={tone}
                  style={{ flex: '1 1 0%' }}
                  className={cn(
                    'status-health-segment focus-visible:ring-ring min-h-11 min-w-[6px] rounded-sm transition-colors hover:brightness-90 focus-visible:ring-2 focus-visible:ring-offset-1 focus-visible:outline-none',
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
                <div className='text-popover-foreground text-xs'>
                  {bucket.request_count > 0 && bucket.success_rate != null
                    ? i18n.t('成功率 {{param0}}%', {
                        param0: bucket.success_rate.toFixed(1),
                      })
                    : i18n.t('该时间段暂无请求样本')}
                </div>
              </div>
            </TooltipContent>
          </Tooltip>
        ))}
      </div>

      <div className='text-muted-foreground flex flex-wrap items-center gap-x-3 gap-y-2 text-[11px]'>
        <LegendSwatch
          className={SEGMENT_CLASS.healthy}
          label={i18n.t('稳定')}
        />
        <LegendSwatch
          className={SEGMENT_CLASS.unstable}
          label={i18n.t('波动')}
        />
        <LegendSwatch className={SEGMENT_CLASS.failed} label={i18n.t('异常')} />
        <LegendSwatch
          className={SEGMENT_CLASS.unknown}
          label={i18n.t('无样本')}
        />
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
  return timeFormatter().format(date)
}

function buildBucketLabel(
  bucket: SidebarGroupStatusBucket,
  bucketSeconds: number
) {
  const range = formatBucketRange(bucket.ts, bucketSeconds)
  if (bucket.request_count <= 0 || bucket.success_rate == null) {
    return i18n.t('{{range}}，暂无请求样本', { range })
  }
  return i18n.t('{{range}}，成功率 {{rate}}%', {
    range,
    rate: bucket.success_rate.toFixed(1),
  })
}

function inferBucketSeconds(sampleWindowHours: number, segmentCount: number) {
  const totalSeconds = Math.max(1, Math.round(sampleWindowHours * 3600))
  return Math.max(60, Math.round(totalSeconds / Math.max(segmentCount, 1)))
}
