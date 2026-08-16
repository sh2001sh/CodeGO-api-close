import * as React from 'react'
import {
  flexRender,
  getCoreRowModel,
  type PaginationState,
  useReactTable,
} from '@tanstack/react-table'
import { Activity, RefreshCcw, RotateCcw, ShieldCheck } from 'lucide-react'
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
import { TableCell, TableRow } from '@/components/ui/table'
import { DataTablePage } from '@/components/data-table'
import { useMyMarketplaceUsageLogs } from '@/features/marketplace/hooks'
import type { MarketplaceChannel } from '@/features/marketplace/types'
import { CompactDateTimeRangePicker } from './compact-date-time-range-picker'
import { useOwnerChannelLogColumns } from './owner-channel-log-columns'

interface DateRange {
  start?: Date
  end?: Date
}

export function OwnerChannelUsageLogs(props: {
  channels: MarketplaceChannel[]
}) {
  const { t } = useTranslation()
  const [channelId, setChannelId] = React.useState('all')
  const [range, setRange] = React.useState<DateRange>({})
  const [pagination, setPagination] = React.useState<PaginationState>({
    pageIndex: 0,
    pageSize: 20,
  })
  const query = useMyMarketplaceUsageLogs({
    channelId: channelId === 'all' ? undefined : channelId,
    startTimestamp: toTimestamp(range.start),
    endTimestamp: toTimestamp(range.end),
    page: pagination.pageIndex + 1,
    pageSize: pagination.pageSize,
  })
  const columns = useOwnerChannelLogColumns()
  const data = query.data
  const pageCount = Math.max(
    1,
    Math.ceil((data?.total ?? 0) / pagination.pageSize)
  )
  const table = useReactTable({
    data: data?.items ?? [],
    columns,
    state: { pagination },
    onPaginationChange: setPagination,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    pageCount,
  })

  React.useEffect(() => {
    if (pagination.pageIndex >= pageCount) {
      setPagination((current) => ({ ...current, pageIndex: pageCount - 1 }))
    }
  }, [pageCount, pagination.pageIndex])

  const periodLabel = React.useMemo(
    () => formatPeriodLabel(range, t),
    [range, t]
  )
  const updateChannel = (value: string | null) => {
    if (!value) return
    setChannelId(value)
    resetPage(setPagination)
  }
  const updateRange = (nextRange: DateRange) => {
    setRange(nextRange)
    resetPage(setPagination)
  }

  return (
    <DataTablePage
      table={table}
      columns={columns}
      isLoading={query.isLoading}
      isFetching={query.isFetching}
      emptyTitle={t('暂无渠道调用日志')}
      emptyDescription={t('渠道产生真实调用后，记录和逐笔收入会显示在这里。')}
      skeletonKeyPrefix='owner-channel-log'
      paginationInFooter={false}
      tableClassName='max-h-[calc(100dvh-18rem)] overflow-auto'
      tableHeaderClassName='bg-muted/30 sticky top-0 z-10'
      toolbar={
        <OwnerLogToolbar
          channels={props.channels}
          channelId={channelId}
          range={range}
          periodLabel={periodLabel}
          summary={data?.summary}
          fetching={query.isFetching}
          onChannelChange={updateChannel}
          onRangeChange={updateRange}
          onRefresh={() => void query.refetch()}
        />
      }
      renderRow={(row) => (
        <TableRow key={row.id} className='transition-colors'>
          {row.getVisibleCells().map((cell) => (
            <TableCell key={cell.id} className='py-2.5'>
              {flexRender(cell.column.columnDef.cell, cell.getContext())}
            </TableCell>
          ))}
        </TableRow>
      )}
    />
  )
}

function OwnerLogToolbar(props: {
  channels: MarketplaceChannel[]
  channelId: string
  range: DateRange
  periodLabel: string
  summary?: {
    request_count: number
    success_count: number
    failed_count: number
    consumer_amount: number
    owner_income: number
  }
  fetching: boolean
  onChannelChange: (value: string | null) => void
  onRangeChange: (range: DateRange) => void
  onRefresh: () => void
}) {
  const { t } = useTranslation()
  return (
    <div className='space-y-3'>
      <div className='flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between'>
        <div className='min-w-0'>
          <div className='flex items-center gap-2 font-semibold'>
            <Activity className='text-primary size-4' />
            {t('渠道使用日志')}
          </div>
          <p className='text-muted-foreground mt-1 text-sm'>
            {t('按渠道和时间范围核对调用、用户扣费与逐笔收入。')}
          </p>
        </div>
        <div className='flex flex-col gap-2 sm:flex-row sm:items-center'>
          <Select value={props.channelId} onValueChange={props.onChannelChange}>
            <SelectTrigger className='w-full sm:w-56'>
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
            start={props.range.start}
            end={props.range.end}
            onChange={props.onRangeChange}
            className='w-full sm:w-[18.5rem]'
          />
          {(props.range.start || props.range.end) && (
            <Button
              variant='outline'
              size='icon'
              onClick={() => props.onRangeChange({})}
              title={t('清除时间范围')}
              aria-label={t('清除时间范围')}
            >
              <RotateCcw />
            </Button>
          )}
          <Button
            variant='outline'
            size='icon'
            onClick={props.onRefresh}
            disabled={props.fetching}
            title={t('刷新')}
            aria-label={t('刷新')}
          >
            <RefreshCcw className={props.fetching ? 'animate-spin' : ''} />
          </Button>
        </div>
      </div>
      <div className='border-border bg-info/5 text-muted-foreground flex items-start gap-2 rounded-md border px-3 py-2.5 text-xs'>
        <ShieldCheck className='text-info mt-0.5 size-3.5 shrink-0' />
        <span>
          {t('日志已脱敏，仅展示用户外部 ID；当前统计范围：{{period}}。', {
            period: props.periodLabel,
          })}
        </span>
      </div>
      <SummaryBand summary={props.summary} />
    </div>
  )
}

function SummaryBand(props: { summary?: OwnerLogToolbarProps['summary'] }) {
  const { t } = useTranslation()
  const summary = props.summary
  const items = [
    {
      label: t('总调用'),
      value: (summary?.request_count ?? 0).toLocaleString(),
    },
    {
      label: t('成功调用'),
      value: (summary?.success_count ?? 0).toLocaleString(),
    },
    {
      label: t('失败调用'),
      value: (summary?.failed_count ?? 0).toLocaleString(),
    },
    {
      label: t('用户总扣费'),
      value: formatQuota(summary?.consumer_amount ?? 0),
    },
    { label: t('渠道总收入'), value: formatQuota(summary?.owner_income ?? 0) },
  ]
  return (
    <div className='border-border bg-card grid overflow-hidden rounded-lg border sm:grid-cols-2 xl:grid-cols-5'>
      {items.map((item) => (
        <div
          key={item.label}
          className='border-border min-w-0 border-b px-4 py-3 sm:border-r xl:border-b-0'
        >
          <div className='text-muted-foreground text-xs'>{item.label}</div>
          <div className='mt-1 truncate font-semibold tabular-nums'>
            {item.value}
          </div>
        </div>
      ))}
    </div>
  )
}

type OwnerLogToolbarProps = Parameters<typeof OwnerLogToolbar>[0]

function toTimestamp(value?: Date) {
  return value ? Math.floor(value.getTime() / 1000) : undefined
}

function resetPage(
  setPagination: React.Dispatch<React.SetStateAction<PaginationState>>
) {
  setPagination((current) => ({ ...current, pageIndex: 0 }))
}

function formatPeriodLabel(range: DateRange, t: (key: string) => string) {
  if (!range.start && !range.end) return t('全部时间')
  const start = range.start
    ? dayjs(range.start).format('YYYY-MM-DD HH:mm')
    : '-'
  const end = range.end ? dayjs(range.end).format('YYYY-MM-DD HH:mm') : '-'
  return `${start} ~ ${end}`
}
