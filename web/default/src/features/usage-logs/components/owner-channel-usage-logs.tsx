import * as React from 'react'
import {
  flexRender,
  getCoreRowModel,
  type PaginationState,
  useReactTable,
} from '@tanstack/react-table'
import { getIntlLocale } from '@/i18n/format'
import { Activity, Download, RefreshCcw, RotateCcw, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import dayjs from '@/lib/dayjs'
import { formatQuota } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { TableCell, TableRow } from '@/components/ui/table'
import { BoundedSelect } from '@/components/bounded-select'
import { DataTablePage } from '@/components/data-table'
import { exportMyMarketplaceUsageLogs } from '@/features/marketplace/api'
import { useMyMarketplaceUsageLogs } from '@/features/marketplace/hooks'
import type {
  OwnerChannelOption,
  MarketplaceOwnerUsageLog,
} from '@/features/marketplace/types'
import { CompactDateTimeRangePicker } from './compact-date-time-range-picker'
import { useOwnerChannelLogColumns } from './owner-channel-log-columns'
import { OwnerChannelLogDetailsDialog } from './owner-channel-log-details-dialog'

interface DateRange {
  start?: Date
  end?: Date
}

export function OwnerChannelUsageLogs(props: {
  channels: OwnerChannelOption[]
  focus?: {
    channelId: string
    requestId: string
    userId: number
    nonce: number
  }
}) {
  const { t } = useTranslation()
  const [channelId, setChannelId] = React.useState(
    props.focus?.channelId || 'all'
  )
  const [status, setStatus] = React.useState('all')
  const initialSearch =
    props.focus?.requestId ||
    (props.focus?.userId ? String(props.focus.userId) : '')
  const [searchDraft, setSearchDraft] = React.useState(initialSearch)
  const [search, setSearch] = React.useState(initialSearch)
  const [selectedLog, setSelectedLog] =
    React.useState<MarketplaceOwnerUsageLog | null>(null)
  const [range, setRange] = React.useState<DateRange>(() => ({
    start: dayjs().startOf('day').toDate(),
  }))
  const [pagination, setPagination] = React.useState<PaginationState>({
    pageIndex: 0,
    pageSize: 20,
  })
  const [exporting, setExporting] = React.useState(false)
  const query = useMyMarketplaceUsageLogs({
    channelId: channelId === 'all' ? undefined : channelId,
    status: status === 'success' || status === 'failed' ? status : undefined,
    search: search || undefined,
    startTimestamp: toTimestamp(range.start),
    endTimestamp: toTimestamp(range.end),
    page: pagination.pageIndex + 1,
    pageSize: pagination.pageSize,
  })
  const inspectLog = React.useCallback(
    (item: MarketplaceOwnerUsageLog) => setSelectedLog(item),
    []
  )
  const columns = useOwnerChannelLogColumns(inspectLog)
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

  const updateChannel = (value: string | null) => {
    if (!value) return
    setChannelId(value)
    resetPage(setPagination)
  }
  const updateRange = (nextRange: DateRange) => {
    setRange(nextRange)
    resetPage(setPagination)
  }
  const applySearch = () => {
    setSearch(searchDraft.trim())
    resetPage(setPagination)
  }

  const exportLogs = async () => {
    setExporting(true)
    try {
      const blob = await exportMyMarketplaceUsageLogs({
        channelId: channelId === 'all' ? undefined : channelId,
        status:
          status === 'success' || status === 'failed' ? status : undefined,
        search: search || undefined,
        startTimestamp: toTimestamp(range.start),
        endTimestamp: toTimestamp(range.end),
        page: 1,
        pageSize: pagination.pageSize,
      })
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = `channel-usage-logs-${dayjs().format('YYYYMMDD-HHmmss')}.csv`
      document.body.appendChild(link)
      link.click()
      link.remove()
      URL.revokeObjectURL(url)
      toast.success(t('渠道日志已导出'))
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t('渠道日志导出失败')
      )
    } finally {
      setExporting(false)
    }
  }

  return (
    <>
      {query.isError && (
        <p role='alert' className='text-destructive p-3 text-sm'>
          {t('渠道日志加载失败，请点击刷新重试。')}
        </p>
      )}
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
            status={status}
            search={searchDraft}
            range={range}
            summary={data?.summary}
            fetching={query.isFetching}
            onChannelChange={updateChannel}
            onStatusChange={(value) => {
              if (!value) return
              setStatus(value)
              resetPage(setPagination)
            }}
            onSearchChange={setSearchDraft}
            onSearch={applySearch}
            onRangeChange={updateRange}
            onRefresh={() => void query.refetch()}
            exporting={exporting}
            canExport={(data?.total ?? 0) > 0}
            onExport={exportLogs}
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
      <OwnerChannelLogDetailsDialog
        item={selectedLog}
        onOpenChange={(open) => !open && setSelectedLog(null)}
      />
    </>
  )
}

function OwnerLogToolbar(props: {
  channels: OwnerChannelOption[]
  channelId: string
  status: string
  search: string
  range: DateRange
  summary?: {
    request_count: number
    success_count: number
    failed_count: number
    upstream_attempt_count: number
    upstream_success_count: number
    upstream_failed_count: number
    consumer_amount: number
    owner_income: number
  }
  fetching: boolean
  onChannelChange: (value: string | null) => void
  onStatusChange: (value: string | null) => void
  onSearchChange: (value: string) => void
  onSearch: () => void
  onRangeChange: (range: DateRange) => void
  onRefresh: () => void
  exporting: boolean
  canExport: boolean
  onExport: () => void
}) {
  const { t } = useTranslation()
  const channelOptions = React.useMemo(
    () => [
      { value: 'all', label: t('全部渠道') },
      ...props.channels.map((channel) => ({
        value: channel.id,
        label: `${channel.system_display_name}${channel.deleted_at ? ` (${t('已删除')})` : ''}`,
      })),
    ],
    [props.channels, t]
  )
  return (
    <div className='console-usage-table space-y-4'>
      <div className='flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between'>
        <div className='min-w-0'>
          <div className='flex items-center gap-2 font-semibold'>
            <Activity className='text-primary size-4' />
            {t('渠道使用日志')}
          </div>
        </div>
        <div className='flex flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center sm:justify-end'>
          <form
            className='flex min-w-0 sm:w-72'
            onSubmit={(event) => {
              event.preventDefault()
              props.onSearch()
            }}
          >
            <Input
              value={props.search}
              onChange={(event) => props.onSearchChange(event.target.value)}
              placeholder={t('搜索模型、用户、请求 ID 或错误')}
              className='rounded-r-none'
              aria-label={t('搜索渠道调用日志')}
            />
            <Button
              type='submit'
              variant='outline'
              size='icon'
              className='rounded-l-none border-l-0'
              aria-label={t('搜索')}
            >
              <Search />
            </Button>
          </form>
          <BoundedSelect
            value={props.channelId}
            options={channelOptions}
            onValueChange={props.onChannelChange}
            aria-label={t('渠道使用日志')}
            className='w-full sm:w-56'
          />
          <Select value={props.status} onValueChange={props.onStatusChange}>
            <SelectTrigger className='w-full sm:w-32'>
              <SelectValue>
                {props.status === 'all'
                  ? t('全部状态')
                  : props.status === 'success'
                    ? t('调用成功')
                    : t('调用失败')}
              </SelectValue>
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                <SelectItem value='all'>{t('全部状态')}</SelectItem>
                <SelectItem value='success'>{t('调用成功')}</SelectItem>
                <SelectItem value='failed'>{t('调用失败')}</SelectItem>
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
            size='sm'
            onClick={props.onExport}
            disabled={props.exporting || !props.canExport}
            title={t('导出当前筛选范围的渠道日志')}
          >
            <Download className={props.exporting ? 'animate-pulse' : ''} />
            {props.exporting ? t('导出中') : t('导出日志')}
          </Button>
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
      <SummaryBand summary={props.summary} />
    </div>
  )
}

function SummaryBand(props: { summary?: OwnerLogToolbarProps['summary'] }) {
  const { t } = useTranslation()
  const summary = props.summary
  const items = [
    {
      label: t('上游尝试'),
      value: (summary?.upstream_attempt_count ?? 0).toLocaleString(
        getIntlLocale()
      ),
    },
    {
      label: t('上游成功'),
      value: (summary?.upstream_success_count ?? 0).toLocaleString(
        getIntlLocale()
      ),
    },
    {
      label: t('上游失败'),
      value: (summary?.upstream_failed_count ?? 0).toLocaleString(
        getIntlLocale()
      ),
    },
    {
      label: t('计费请求'),
      value: (summary?.request_count ?? 0).toLocaleString(getIntlLocale()),
    },
    {
      label: t('用户总扣费'),
      value: formatQuota(summary?.consumer_amount ?? 0),
    },
    { label: t('渠道总收入'), value: formatQuota(summary?.owner_income ?? 0) },
  ]
  return (
    <div className='solaris-owner-stats grid gap-3 sm:grid-cols-2 xl:grid-cols-3'>
      {items.map((item) => (
        <div
          key={item.label}
          className='bg-card min-w-0 rounded-2xl border p-5'
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
