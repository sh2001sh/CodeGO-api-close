import { useMemo, useState } from 'react'
import { getIntlLocale } from '@/i18n/format'
import { RefreshCcw, RotateCcw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatQuotaWithCurrency } from '@/lib/currency'
import dayjs from '@/lib/dayjs'
import { formatQuota } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { BoundedSelect } from '@/components/bounded-select'
import { CompactDateTimeRangePicker } from '@/features/usage-logs/components/compact-date-time-range-picker'
import {
  useMyMarketplaceIncomeFreeze,
  useMyMarketplaceUsageLogs,
} from '../hooks'
import type { OwnerChannelOption } from '../types'

interface DateRange {
  start?: Date
  end?: Date
}

export function OwnerIncomeOverview(props: { channels: OwnerChannelOption[] }) {
  const { t } = useTranslation()
  const freeze = useMyMarketplaceIncomeFreeze()
  const [channelId, setChannelId] = useState('all')
  const channelOptions = useMemo(
    () => [
      { value: 'all', label: t('全部渠道') },
      ...props.channels.map((channel) => ({
        value: channel.id,
        label: `${channel.system_display_name}${channel.deleted_at ? ` (${t('已删除')})` : ''}`,
      })),
    ],
    [props.channels, t]
  )
  const [range, setRange] = useState<DateRange>(() => ({
    start: dayjs().startOf('day').toDate(),
  }))
  const query = useMyMarketplaceUsageLogs({
    summaryOnly: true,
    channelId: channelId === 'all' ? undefined : channelId,
    startTimestamp: toTimestamp(range.start),
    endTimestamp: toTimestamp(range.end),
    page: 1,
    pageSize: 20,
  })
  const period = useMemo(() => formatPeriod(range, t), [range, t])
  const metrics = [
    {
      label: t('筛选范围总收入'),
      value: formatQuota(query.data?.summary.owner_income ?? 0),
    },
    {
      label: t('已到账收益'),
      value: formatQuota(query.data?.summary.released_income ?? 0),
    },
    {
      label: t('筛选范围调用'),
      value: (query.data?.summary.request_count ?? 0).toLocaleString(
        getIntlLocale()
      ),
    },
    {
      label: t('已回收额度'),
      value: formatQuota(query.data?.summary.reclaimed_income ?? 0),
    },
  ]

  return (
    <div>
      {freeze.isError && (
        <div
          role='alert'
          className='text-destructive px-4 py-3 text-sm sm:px-5'
        >
          {t('冻结状态加载失败，请刷新重试。')}
        </div>
      )}
      {!freeze.isError && (
        <div
          role='status'
          className='border-border bg-muted/50 border-b px-4 py-3 text-sm sm:px-5'
        >
          <div className='font-medium'>
            {t('已冻结')} ·{' '}
            {freeze.isPending
              ? t('加载中…')
              : formatQuotaWithCurrency(freeze.data?.frozen_amount ?? 0, {
                  digitsLarge: 6,
                  digitsSmall: 6,
                  abbreviate: false,
                })}
          </div>
          <p className='text-muted-foreground mt-1 text-xs'>
            {t(
              '该金额已从钱包可用余额中冻结，等待人工结算，不受下方统计范围影响。'
            )}
          </p>
        </div>
      )}
      <div className='owner-income-filter'>
        <div className='min-w-0'>
          <div className='text-sm font-medium'>{t('收入统计范围')}</div>
          <div className='text-muted-foreground mt-0.5 text-xs'>{period}</div>
        </div>
        <div className='flex min-w-0 flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center'>
          <BoundedSelect
            value={channelId}
            options={channelOptions}
            onValueChange={setChannelId}
            aria-label={t('收入统计范围')}
            className='w-full sm:w-52'
          />
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
            onClick={() => {
              void query.refetch()
              void freeze.refetch()
            }}
            disabled={query.isFetching || freeze.isFetching}
            title={t('刷新收入')}
            aria-label={t('刷新收入')}
          >
            <RefreshCcw className={query.isFetching ? 'animate-spin' : ''} />
          </Button>
        </div>
      </div>
      {query.isError && (
        <div
          role='alert'
          className='bg-destructive/5 text-destructive border-border border-t px-4 py-2 text-xs sm:px-5'
        >
          {t('所选范围的收入统计加载失败，请重试。')}
        </div>
      )}
      <dl className='owner-income-metrics'>
        {metrics.map(({ label, value }) => (
          <div key={label} className='min-w-0'>
            <dt>{label}</dt>
            <dd>
              {query.isFetching ? t('加载中…') : query.isError ? '—' : value}
            </dd>
          </div>
        ))}
      </dl>
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
