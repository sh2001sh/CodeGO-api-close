import { useState } from 'react'
import {
  Activity,
  ChevronLeft,
  ChevronRight,
  RefreshCcw,
  ShieldCheck,
  UserRound,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatQuota, formatUseTime } from '@/lib/format'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { useMyMarketplaceUsageLogs } from '@/features/marketplace/hooks'
import type {
  MarketplaceChannel,
  MarketplaceOwnerUsageLog,
} from '@/features/marketplace/types'

const PAGE_SIZE = 20

function IncomeStatus(props: { item: MarketplaceOwnerUsageLog }) {
  const { t } = useTranslation()
  if (props.item.status === 'failed') {
    return (
      <span className='bg-destructive/10 text-destructive inline-flex rounded-md px-2 py-0.5 text-xs font-medium'>
        {t('调用失败')}
      </span>
    )
  }
  if (props.item.income_status === 'released') {
    return (
      <span className='bg-success/10 text-success inline-flex rounded-md px-2 py-0.5 text-xs font-medium'>
        {t('已到账')}
      </span>
    )
  }
  if (props.item.income_status === 'pending') {
    return (
      <span className='bg-warning/10 text-warning inline-flex rounded-md px-2 py-0.5 text-xs font-medium'>
        {t('待结算')}
      </span>
    )
  }
  return (
    <span className='bg-muted text-muted-foreground inline-flex rounded-md px-2 py-0.5 text-xs font-medium'>
      {t('未入账')}
    </span>
  )
}

function SummaryBand(props: {
  requestCount: number
  successCount: number
  failedCount: number
  consumerAmount: number
  ownerIncome: number
}) {
  const { t } = useTranslation()
  const items = [
    { label: t('总调用'), value: props.requestCount.toLocaleString() },
    { label: t('成功调用'), value: props.successCount.toLocaleString() },
    { label: t('失败调用'), value: props.failedCount.toLocaleString() },
    { label: t('用户总扣费'), value: formatQuota(props.consumerAmount) },
    { label: t('渠道总收入'), value: formatQuota(props.ownerIncome) },
  ]

  return (
    <div className='border-border bg-muted/20 grid border-y sm:grid-cols-2 xl:grid-cols-5'>
      {items.map((item) => (
        <div
          key={item.label}
          className='border-border min-w-0 border-b px-4 py-3 sm:border-r xl:border-b-0'
        >
          <div className='text-muted-foreground text-xs'>{item.label}</div>
          <div className='text-foreground mt-1 truncate font-semibold tabular-nums'>
            {item.value}
          </div>
        </div>
      ))}
    </div>
  )
}

export function OwnerChannelUsageLogs(props: {
  channels: MarketplaceChannel[]
}) {
  const { t } = useTranslation()
  const [channelId, setChannelId] = useState('all')
  const [page, setPage] = useState(1)
  const query = useMyMarketplaceUsageLogs({
    channelId: channelId === 'all' ? undefined : channelId,
    page,
    pageSize: PAGE_SIZE,
  })
  const data = query.data
  const pageCount = Math.max(1, Math.ceil((data?.total ?? 0) / PAGE_SIZE))

  const selectChannel = (value: string | null) => {
    if (!value) return
    setChannelId(value)
    setPage(1)
  }

  return (
    <section className='border-border bg-card overflow-hidden rounded-lg border'>
      <header className='flex flex-wrap items-start justify-between gap-4 px-4 py-4 sm:px-5'>
        <div className='min-w-0'>
          <div className='flex items-center gap-2 font-semibold'>
            <Activity className='text-primary size-4' />
            {t('渠道使用日志')}
          </div>
          <p className='text-muted-foreground mt-1 text-sm'>
            {t('查看名下全部渠道或单个渠道的调用、扣费与逐笔收入。')}
          </p>
        </div>
        <div className='flex w-full items-center gap-2 sm:w-auto'>
          <Select value={channelId} onValueChange={selectChannel}>
            <SelectTrigger className='min-w-0 flex-1 sm:w-56'>
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
          <Button
            variant='outline'
            size='icon'
            onClick={() => void query.refetch()}
            disabled={query.isFetching}
            title={t('刷新')}
          >
            <RefreshCcw className={query.isFetching ? 'animate-spin' : ''} />
          </Button>
        </div>
      </header>

      <div className='border-border bg-info/5 text-muted-foreground flex items-start gap-2 border-t px-4 py-2.5 text-xs sm:px-5'>
        <ShieldCheck className='text-info mt-0.5 size-3.5 shrink-0' />
        <span>
          {t(
            '日志已脱敏，仅展示调用用户 ID，不展示用户名、Token、IP 或请求内容。'
          )}
        </span>
      </div>

      <SummaryBand
        requestCount={data?.summary.request_count ?? 0}
        successCount={data?.summary.success_count ?? 0}
        failedCount={data?.summary.failed_count ?? 0}
        consumerAmount={data?.summary.consumer_amount ?? 0}
        ownerIncome={data?.summary.owner_income ?? 0}
      />

      {query.isLoading ? (
        <div className='space-y-2 p-4'>
          {Array.from({ length: 6 }).map((_, index) => (
            <Skeleton key={index} className='h-12 w-full' />
          ))}
        </div>
      ) : query.isError ? (
        <div className='px-4 py-12 text-center'>
          <div className='font-medium'>{t('渠道日志加载失败')}</div>
          <Button
            variant='outline'
            size='sm'
            className='mt-4'
            onClick={() => void query.refetch()}
          >
            <RefreshCcw />
            {t('重试')}
          </Button>
        </div>
      ) : (data?.items.length ?? 0) === 0 ? (
        <div className='px-4 py-12 text-center'>
          <div className='font-medium'>{t('暂无渠道调用日志')}</div>
          <p className='text-muted-foreground mt-1 text-sm'>
            {t('渠道产生真实调用后，记录和逐笔收入会显示在这里。')}
          </p>
        </div>
      ) : (
        <Table>
          <TableHeader className='bg-muted/30'>
            <TableRow>
              <TableHead>{t('时间')}</TableHead>
              <TableHead>{t('渠道 / 模型')}</TableHead>
              <TableHead>{t('用户 ID')}</TableHead>
              <TableHead>{t('Tokens')}</TableHead>
              <TableHead>{t('耗时')}</TableHead>
              <TableHead>{t('用户扣费')}</TableHead>
              <TableHead>{t('渠道余额增加')}</TableHead>
              <TableHead>{t('状态')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {data?.items.map((item) => (
              <TableRow key={item.id}>
                <TableCell className='text-muted-foreground text-xs'>
                  {new Date(item.created_at * 1000).toLocaleString()}
                </TableCell>
                <TableCell>
                  <div className='font-medium'>{item.channel_name}</div>
                  <div className='text-muted-foreground mt-0.5 text-xs'>
                    {item.model_name || '-'}
                  </div>
                </TableCell>
                <TableCell>
                  <span className='inline-flex items-center gap-1 font-mono text-xs'>
                    <UserRound className='text-muted-foreground size-3' />
                    {item.user_id}
                  </span>
                </TableCell>
                <TableCell className='font-mono text-xs tabular-nums'>
                  {item.prompt_tokens.toLocaleString()} /{' '}
                  {item.completion_tokens.toLocaleString()}
                </TableCell>
                <TableCell className='text-xs tabular-nums'>
                  {formatUseTime(item.use_time)}
                </TableCell>
                <TableCell className='font-mono text-xs font-medium tabular-nums'>
                  {formatQuota(item.consumer_amount)}
                </TableCell>
                <TableCell className='text-success font-mono text-xs font-semibold tabular-nums'>
                  +{formatQuota(item.owner_income)}
                </TableCell>
                <TableCell>
                  <IncomeStatus item={item} />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      <div className='border-border flex items-center justify-between gap-3 border-t px-4 py-3 sm:px-5'>
        <div className='text-muted-foreground text-xs'>
          {t('共 {{count}} 条', { count: data?.total ?? 0 })}
        </div>
        <div className='flex items-center gap-2'>
          <Button
            variant='outline'
            size='sm'
            disabled={page <= 1}
            onClick={() => setPage((current) => Math.max(1, current - 1))}
          >
            <ChevronLeft />
            {t('上一页')}
          </Button>
          <span className='text-muted-foreground text-xs tabular-nums'>
            {page} / {pageCount}
          </span>
          <Button
            variant='outline'
            size='sm'
            disabled={page >= pageCount}
            onClick={() =>
              setPage((current) => Math.min(pageCount, current + 1))
            }
          >
            {t('下一页')}
            <ChevronRight />
          </Button>
        </div>
      </div>
    </section>
  )
}
