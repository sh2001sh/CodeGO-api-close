import type { TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import type { MarketplaceGroup } from '../types'

export function RecentRequestStrip(props: { group: MarketplaceGroup }) {
  const { t, i18n } = useTranslation()
  const bucketSeconds = props.group.recent_request_bucket_seconds || 1800
  const threshold = t(
    '每个色块代表 30 分钟：90%（含）以上稳定，85%（含）至 90%（不含）波动，低于 85% 异常，灰色表示无请求'
  )

  return (
    <div className='mt-1.5 min-w-0'>
      <div
        className='flex w-full gap-0.5'
        aria-label={`${t('近 6 小时请求状态')}。${threshold}`}
      >
        {props.group.recent_request_series.map((bucket, index) => {
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
                      'focus-visible:ring-ring h-2.5 min-w-0 flex-1 rounded-sm transition-[filter,box-shadow] hover:brightness-90 focus-visible:ring-2 focus-visible:ring-offset-1 focus-visible:outline-none',
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

function bucketTone(bucket: MarketplaceGroup['recent_request_series'][number]) {
  if (bucket.request_count <= 0) return 'bg-muted'
  if (bucket.success_rate >= 90) return 'bg-success'
  if (bucket.success_rate >= 85) return 'bg-warning'
  return 'bg-destructive'
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
