import { useMemo, useState } from 'react'
import {
  Activity,
  CircleDollarSign,
  RefreshCcw,
  RotateCcw,
  Store,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import dayjs from '@/lib/dayjs'
import { formatQuota } from '@/lib/format'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { CompactDateTimeRangePicker } from '@/features/usage-logs/components/compact-date-time-range-picker'
import { useMyMarketplaceUsageLogs } from '../hooks'
import type { MarketplaceChannel } from '../types'

interface DateRange {
  start?: Date
  end?: Date
}

export function OwnerIncomeOverview(props: { channels: MarketplaceChannel[] }) {
  const { t } = useTranslation()
  const [channelId, setChannelId] = useState('all')
  const [range, setRange] = useState<DateRange>({})
  const query = useMyMarketplaceUsageLogs({
    channelId: channelId === 'all' ? undefined : channelId,
    startTimestamp: toTimestamp(range.start),
    endTimestamp: toTimestamp(range.end),
    page: 1,
    pageSize: 20,
  })
  const runningCount = props.channels.filter((channel) =>
    ['active', 'degraded'].includes(channel.lifecycle_status)
  ).length
  const period = useMemo(() => formatPeriod(range, t), [range, t])
  const metrics = [
    {
      icon: Store,
      label: t('渠道总数'),
      value: String(props.channels.length),
    },
    {
      icon: RefreshCcw,
      label: t('运行中'),
      value: String(runningCount),
    },
    {
      icon: CircleDollarSign,
      label: t('筛选范围总收入'),
      value: formatQuota(query.data?.summary.owner_income ?? 0),
    },
    {
      icon: CircleDollarSign,
      label: t('已到账收益'),
      value: formatQuota(query.data?.summary.released_income ?? 0),
    },
    {
      icon: Activity,
      label: t('筛选范围调用'),
      value: (query.data?.summary.request_count ?? 0).toLocaleString(),
    },
    {
      icon: CircleDollarSign,
      label: t('已回收额度'),
      value: formatQuota(query.data?.summary.reclaimed_income ?? 0),
    },
  ]

  return (
    <div className='border-border border-y'>
      <div className='bg-muted/15 flex flex-col gap-2 px-4 py-3 sm:px-5 lg:flex-row lg:items-center lg:justify-between'>
        <div className='min-w-0'>
          <div className='text-sm font-medium'>{t('收入统计范围')}</div>
          <div className='text-muted-foreground mt-0.5 truncate text-xs'>
            {period}
          </div>
        </div>
        <div className='flex flex-col gap-2 sm:flex-row sm:items-center'>
          <Select
            value={channelId}
            onValueChange={(value) => value && setChannelId(value)}
          >
            <SelectTrigger className='w-full sm:w-52'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                <SelectItem value='all'>{t('全部渠道')}</SelectItem>
                {props.channels.map((channel) => (
                  <SelectItem key={channel.id} value={channel.id}>
                    {channel.system_display_name}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
          <CompactDateTimeRangePicker
            start={range.start}
            end={range.end}
            onChange={setRange}
            className='w-full sm:w-[18.5rem]'
          />
          {(range.start || range.end) && (
            <Button
              variant='outline'
              size='icon'
              onClick={() => setRange({})}
              title={t('清除时间范围')}
              aria-label={t('清除时间范围')}
            >
              <RotateCcw />
            </Button>
          )}
          <Button
            variant='outline'
            size='icon'
            onClick={() => void query.refetch()}
            disabled={query.isFetching}
            title={t('刷新收入')}
            aria-label={t('刷新收入')}
          >
            <RefreshCcw className={query.isFetching ? 'animate-spin' : ''} />
          </Button>
        </div>
      </div>
      {query.isError && (
        <div className='bg-destructive/5 text-destructive border-border border-t px-4 py-2 text-xs sm:px-5'>
          {t('所选范围的收入统计加载失败，请重试。')}
        </div>
      )}
      <div className='bg-card grid sm:grid-cols-2 xl:grid-cols-5'>
        {metrics.map(({ icon: Icon, label, value }) => (
          <div
            key={label}
            className='border-border flex min-h-20 items-center gap-3 border-b px-4 py-3 sm:border-r xl:border-b-0'
          >
            <span className='bg-muted flex size-9 shrink-0 items-center justify-center rounded-md'>
              <Icon className='text-primary size-4' />
            </span>
            <div className='min-w-0'>
              <div className='text-muted-foreground text-xs'>{label}</div>
              <div className='mt-1 truncate text-lg font-semibold tabular-nums'>
                {value}
              </div>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

function toTimestamp(value?: Date) {
  return value ? Math.floor(value.getTime() / 1000) : undefined
}

function formatPeriod(range: DateRange, t: (key: string) => string) {
  if (!range.start && !range.end) return t('全部时间')
  const start = range.start
    ? dayjs(range.start).format('YYYY-MM-DD HH:mm')
    : '-'
  const end = range.end ? dayjs(range.end).format('YYYY-MM-DD HH:mm') : '-'
  return `${start} ~ ${end}`
}
