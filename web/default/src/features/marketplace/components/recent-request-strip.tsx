import type { TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'
import {
  classifyRequestHealth,
  getRequestHealthLabel,
  type RequestHealthStatus,
} from '@/lib/request-health'
import { cn } from '@/lib/utils'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import {
  normalizeRecentRequestSeries,
  resolveRecentRequestStatus,
} from '../lib/recent-request-series'
import type { MarketplaceGroup } from '../types'

export function RecentRequestStrip(props: { group: MarketplaceGroup }) {
  const { t, i18n } = useTranslation()
  const bucketSeconds = props.group.recent_request_bucket_seconds || 1800
  const series = normalizeRecentRequestSeries(
    props.group.recent_request_series,
    bucketSeconds
  )
  const latestStatus = resolveRecentRequestStatus(series)
  const threshold = t(
    '每个色块代表 30 分钟：90%（含）以上稳定，85%（含）至 90%（不含）波动，低于 85% 异常，灰色表示无请求'
  )

  return (
    <div className='mt-1.5 min-w-0'>
      <div className='mb-1 flex items-center justify-between gap-2 text-[11px]'>
        <span className='text-muted-foreground'>{t('最近请求状态')}</span>
        <RequestStatus status={latestStatus} t={t} />
      </div>
      <div
        className='flex w-full gap-0.5'
        aria-label={`${t('近 6 小时请求状态')}。${threshold}`}
      >
        {series.map((bucket, index) => {
          const range = formatBucketRange(
            bucket.ts,
            bucketSeconds,
            i18n.language
          )
          const summary = buildBucketSummary(bucket, t)
          return (
            <Tooltip key={`${bucket.ts}-${index}`}>
              <TooltipTrigger
                render={
                  <button
                    type='button'
                    aria-label={`${range}，${summary}`}
                    className={cn(
                      'focus-visible:ring-ring h-3 min-w-0 flex-1 rounded-sm transition-[filter,box-shadow] hover:brightness-90 focus-visible:ring-2 focus-visible:ring-offset-1 focus-visible:outline-none',
                      bucketTone(bucket)
                    )}
                  />
                }
              />
              <TooltipContent side='top' className='max-w-none'>
                <div className='space-y-0.5'>
                  <div className='font-medium'>{range}</div>
                  <div className='text-background/80 text-xs'>{summary}</div>
                </div>
              </TooltipContent>
            </Tooltip>
          )
        })}
      </div>
    </div>
  )
}

function RequestStatus(props: { status: RequestHealthStatus; t: TFunction }) {
  const label = props.t(getRequestHealthLabel(props.status))
  let tone = 'bg-muted text-muted-foreground'
  if (props.status === 'healthy') {
    tone = 'bg-success/12 text-success'
  } else if (props.status === 'unstable') {
    tone = 'bg-warning/15 text-warning-foreground'
  } else if (props.status === 'failed') {
    tone = 'bg-destructive/12 text-destructive'
  }
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1 rounded-full px-2 py-0.5 font-medium',
        tone
      )}
    >
      <span className='size-1.5 rounded-full bg-current' aria-hidden='true' />
      {label}
    </span>
  )
}

function bucketTone(bucket: MarketplaceGroup['recent_request_series'][number]) {
  const status = classifyRequestHealth(
    bucket.success_rate,
    bucket.request_count
  )
  if (status === 'healthy') return 'bg-success'
  if (status === 'unstable') return 'bg-warning'
  if (status === 'failed') return 'bg-destructive'
  return 'bg-muted'
}

function buildBucketSummary(
  bucket: MarketplaceGroup['recent_request_series'][number],
  t: TFunction
) {
  if (bucket.request_count <= 0) {
    return t('暂无请求')
  }
  return t('{{count}} 次请求，成功率 {{rate}}%', {
    count: bucket.request_count,
    rate: bucket.success_rate.toFixed(1),
  })
}

function formatBucketRange(ts: number, bucketSeconds: number, locale: string) {
  const formatter = new Intl.DateTimeFormat(locale, {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
  return `${formatter.format(new Date(ts * 1000))} - ${formatter.format(
    new Date((ts + bucketSeconds) * 1000)
  )}`
}
