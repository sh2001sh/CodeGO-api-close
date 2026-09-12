import { useMemo, useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import {
  Activity,
  BellRing,
  ChevronLeft,
  ChevronRight,
  Download,
  ExternalLink,
  RefreshCw,
  Search,
  ShieldAlert,
  ShieldCheck,
  ShieldBan,
  UserRound,
  Waypoints,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import dayjs from '@/lib/dayjs'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import { exportSecurityAuditEvents } from '@/features/marketplace/api'
import {
  useSecurityAuditEvents,
  useSecurityAuditEventUpdate,
  useMarketplaceMutations,
} from '@/features/marketplace/hooks'
import type {
  MarketplaceChannel,
  SecurityAuditEvent,
  SecurityAuditEventFilters,
  SecurityAuditNotificationStatus,
  SecurityAuditReviewStatus,
  SecurityAuditSeverity,
} from '@/features/marketplace/types'
import { CompactDateTimeRangePicker } from '@/features/usage-logs/components/compact-date-time-range-picker'

type DateRange = { start?: Date; end?: Date }

const statusLabels: Record<SecurityAuditReviewStatus, string> = {
  unreviewed: '待处理',
  acknowledged: '已确认',
  resolved: '已处理',
  false_positive: '误报',
}

const sourceLabels: Record<string, string> = {
  prompt_guard: '站内 Prompt Guard',
  upstream_cyber_policy: '上游 Cyber Policy',
}

const severityLabels: Record<SecurityAuditSeverity, string> = {
  low: '低风险',
  medium: '中风险',
  high: '高风险',
  critical: '严重风险',
}

const notificationLabels: Record<SecurityAuditNotificationStatus, string> = {
  pending: '通知中',
  dispatched: '已提交通知',
  partial_failed: '部分通知失败',
  failed: '通知失败',
  skipped: '无需通知',
}

function notificationLabel(status: SecurityAuditEvent['notification_status']) {
  return status ? notificationLabels[status] : '历史事件未记录'
}

interface SecurityAuditPanelProps {
  admin?: boolean
  channels?: MarketplaceChannel[]
  onInspectUser?: (event: SecurityAuditEvent) => void
  onInspectLogs?: (event: SecurityAuditEvent) => void
}

export function SecurityAuditPanel({
  admin = false,
  channels = [],
  onInspectUser,
  onInspectLogs,
}: SecurityAuditPanelProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [page, setPage] = useState(1)
  const [searchDraft, setSearchDraft] = useState('')
  const [search, setSearch] = useState('')
  const [source, setSource] = useState('')
  const [reviewStatus, setReviewStatus] = useState('')
  const [channelId, setChannelId] = useState('')
  const [model, setModel] = useState('')
  const [range, setRange] = useState<DateRange>({})
  const [selected, setSelected] = useState<SecurityAuditEvent | null>(null)
  const [note, setNote] = useState('')
  const [exporting, setExporting] = useState(false)
  const filters: SecurityAuditEventFilters = {
    source,
    reviewStatus,
    channelId,
    model,
    search,
    startTimestamp: range.start
      ? Math.floor(range.start.getTime() / 1000)
      : undefined,
    endTimestamp: range.end
      ? Math.floor(range.end.getTime() / 1000)
      : undefined,
    page,
    pageSize: 20,
  }
  const events = useSecurityAuditEvents(filters, admin)
  const update = useSecurityAuditEventUpdate(admin)
  const marketplaceMutations = useMarketplaceMutations()
  const data = events.data
  const totalPages = Math.max(1, Math.ceil((data?.total ?? 0) / 20))

  const metrics = useMemo(
    () => [
      ['事件总数', data?.summary.total ?? 0],
      ['待处理', data?.summary.unreviewed ?? 0],
      ['今日触发', data?.summary.today ?? 0],
      ['影响渠道', data?.summary.affected_channels ?? 0],
      ['影响用户', data?.summary.affected_users ?? 0],
    ],
    [data]
  )
  const attention = useMemo(() => {
    const items = data?.items ?? []
    return {
      repeated: items.filter((item) => item.recent_trigger_count > 1).length,
      notificationIssues: items.filter(
        (item) =>
          item.notification_status === 'failed' ||
          item.notification_status === 'partial_failed'
      ).length,
    }
  }, [data?.items])

  const inspectLogs = (event: SecurityAuditEvent) => {
    if (onInspectLogs) {
      onInspectLogs(event)
      setSelected(null)
      return
    }
    void navigate({
      to: '/usage-logs/$section',
      params: { section: 'common' },
      search: {
        page: 1,
        requestId: event.request_id || undefined,
        channel: event.channel_id ? String(event.channel_id) : undefined,
        model: event.model || undefined,
      },
    })
  }

  const inspectUser = (event: SecurityAuditEvent) => {
    if (onInspectUser) {
      onInspectUser(event)
      setSelected(null)
      return
    }
    void navigate({
      to: '/users',
      search: {
        page: 1,
        pageSize: 10,
        filter: String(event.user_id),
        status: [],
        role: [],
        group: '',
      },
    })
  }

  const inspectChannel = (event: SecurityAuditEvent) => {
    void navigate({
      to: '/channels',
      search: {
        page: 1,
        pageSize: 10,
        filter: String(event.channel_id),
        status: [],
        type: [],
        group: [],
        model: '',
      },
    })
  }

  const saveStatus = (status: SecurityAuditReviewStatus) => {
    if (!selected) return
    update.mutate(
      { id: selected.id, status, note },
      {
        onSuccess: (updated) => {
          setSelected(updated)
          toast.success(t('审计事件已更新'))
        },
        onError: (error) =>
          toast.error(
            error instanceof Error ? error.message : t('审计事件更新失败')
          ),
      }
    )
  }

  const openEvent = (event: SecurityAuditEvent) => {
    setSelected(event)
    setNote(event.review_note ?? '')
  }

  const toggleUserBlock = () => {
    if (
      !selected ||
      admin ||
      selected.user_id <= 0 ||
      !selected.marketplace_channel_id
    ) {
      return
    }
    const blocked = !selected.user_blocked
    if (
      !window.confirm(
        blocked
          ? t('确认拉黑该用户？拉黑只对当前渠道生效。')
          : t('确认解除该用户在当前渠道的拉黑状态？')
      )
    ) {
      return
    }
    marketplaceMutations.userBlock.mutate(
      {
        channelId: selected.marketplace_channel_id,
        userId: selected.user_id,
        blocked,
      },
      {
        onSuccess: () => {
          setSelected({ ...selected, user_blocked: blocked })
          void events.refetch()
          toast.success(blocked ? t('用户已被拉黑') : t('已解除用户拉黑'))
        },
        onError: (error) =>
          toast.error(error instanceof Error ? error.message : t('操作失败')),
      }
    )
  }

  const exportEvents = async () => {
    setExporting(true)
    try {
      const blob = await exportSecurityAuditEvents(filters)
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = `security-audit-${dayjs().format('YYYYMMDD-HHmmss')}.csv`
      document.body.appendChild(link)
      link.click()
      link.remove()
      URL.revokeObjectURL(url)
      toast.success(t('安全审计日志已导出'))
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t('安全审计日志导出失败')
      )
    } finally {
      setExporting(false)
    }
  }

  return (
    <section className='border-border bg-card overflow-hidden rounded-lg border'>
      <header className='flex flex-wrap items-center justify-between gap-3 border-b px-4 py-4'>
        <div>
          <h2 className='font-semibold'>{t('风险事件')}</h2>
          <p className='text-muted-foreground mt-1 text-xs'>
            {admin
              ? t('查看全站 Prompt Guard 与上游安全策略阻断事件。')
              : t('查看与你的市场渠道相关的安全策略阻断事件。')}
          </p>
        </div>
        <div className='flex items-center gap-2'>
          {!admin && (
            <Button
              variant='outline'
              size='sm'
              disabled={exporting || events.isLoading || !data?.total}
              onClick={exportEvents}
            >
              <Download className={exporting ? 'animate-pulse' : ''} />
              {exporting ? t('导出中') : t('导出日志')}
            </Button>
          )}
          <Button
            variant='outline'
            size='sm'
            disabled={events.isFetching}
            onClick={() => events.refetch()}
          >
            <RefreshCw className={events.isFetching ? 'animate-spin' : ''} />
            {t('刷新')}
          </Button>
        </div>
      </header>

      <div className='grid grid-cols-2 border-b sm:grid-cols-3 lg:grid-cols-5'>
        {metrics.map(([label, value]) => (
          <div className='border-r px-4 py-3 last:border-r-0' key={label}>
            <p className='text-muted-foreground text-xs'>{t(String(label))}</p>
            <p className='mt-1 text-xl font-semibold tabular-nums'>{value}</p>
          </div>
        ))}
      </div>

      <div className='bg-muted/20 flex flex-col gap-2 border-b px-4 py-3 text-xs sm:flex-row sm:items-center sm:justify-between'>
        <div className='flex items-start gap-2'>
          {data?.summary.unreviewed ? (
            <ShieldAlert className='text-destructive mt-0.5 size-4 shrink-0' />
          ) : (
            <ShieldCheck className='text-success mt-0.5 size-4 shrink-0' />
          )}
          <span className='text-muted-foreground'>
            {data?.summary.unreviewed
              ? t(
                  '当前有 {{count}} 条事件等待复核，优先检查重复触发的用户与 Key。',
                  {
                    count: data.summary.unreviewed,
                  }
                )
              : t('当前筛选范围内没有待处理事件。')}
          </span>
        </div>
        <div className='flex flex-wrap gap-x-4 gap-y-1 tabular-nums'>
          <span className='inline-flex items-center gap-1'>
            <Activity className='size-3.5' />
            {t('本页重复触发 {{count}} 项', { count: attention.repeated })}
          </span>
          <span className='inline-flex items-center gap-1'>
            <BellRing className='size-3.5' />
            {t('通知异常 {{count}} 项', {
              count: attention.notificationIssues,
            })}
          </span>
        </div>
      </div>

      <div className='flex flex-wrap gap-2 border-b p-3'>
        <form
          className='flex min-w-56 flex-1 gap-2'
          onSubmit={(event) => {
            event.preventDefault()
            setPage(1)
            setSearch(searchDraft.trim())
          }}
        >
          <Input
            value={searchDraft}
            onChange={(event) => setSearchDraft(event.target.value)}
            placeholder={t('搜索请求 ID、Key、用户或错误')}
          />
          <Button
            type='submit'
            variant='outline'
            size='icon'
            aria-label={t('搜索')}
          >
            <Search />
          </Button>
        </form>
        <NativeSelect
          value={source}
          onChange={(event) => {
            setPage(1)
            setSource(event.target.value)
          }}
        >
          <NativeSelectOption value=''>{t('全部来源')}</NativeSelectOption>
          <NativeSelectOption value='prompt_guard'>
            Prompt Guard
          </NativeSelectOption>
          <NativeSelectOption value='upstream_cyber_policy'>
            Cyber Policy
          </NativeSelectOption>
        </NativeSelect>
        <NativeSelect
          value={reviewStatus}
          onChange={(event) => {
            setPage(1)
            setReviewStatus(event.target.value)
          }}
        >
          <NativeSelectOption value=''>{t('全部状态')}</NativeSelectOption>
          {Object.entries(statusLabels).map(([value, label]) => (
            <NativeSelectOption key={value} value={value}>
              {t(label)}
            </NativeSelectOption>
          ))}
        </NativeSelect>
        {admin ? (
          <Input
            className='w-40'
            value={channelId}
            onChange={(event) => {
              setPage(1)
              setChannelId(event.target.value)
            }}
            placeholder={t('渠道 ID')}
          />
        ) : (
          <NativeSelect
            className='w-full sm:w-56'
            value={channelId}
            aria-label={t('选择渠道')}
            onChange={(event) => {
              setPage(1)
              setChannelId(event.target.value)
            }}
          >
            <NativeSelectOption value=''>{t('全部渠道')}</NativeSelectOption>
            {channels.map((channel) => (
              <NativeSelectOption key={channel.id} value={channel.id}>
                {channel.system_display_name ||
                  channel.approved_source_label ||
                  channel.submitted_source_label ||
                  channel.id}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        )}
        <Input
          className='w-40'
          value={model}
          onChange={(event) => {
            setPage(1)
            setModel(event.target.value)
          }}
          placeholder={t('模型')}
        />
        <CompactDateTimeRangePicker
          className='w-auto max-w-72 min-w-44'
          start={range.start}
          end={range.end}
          onChange={(value) => {
            setPage(1)
            setRange(value)
          }}
        />
      </div>

      {events.isError ? (
        <div className='flex flex-col items-center gap-3 p-8 text-center'>
          <ShieldAlert className='text-destructive size-8' />
          <div>
            <p className='font-medium'>{t('安全审计事件加载失败')}</p>
            <p className='text-muted-foreground mt-1 text-xs'>
              {t('请检查网络或服务状态后重试。')}
            </p>
          </div>
          <Button variant='outline' size='sm' onClick={() => events.refetch()}>
            <RefreshCw />
            {t('重新加载')}
          </Button>
        </div>
      ) : events.isLoading ? (
        <div className='space-y-2 p-4'>
          {Array.from({ length: 5 }, (_, index) => (
            <Skeleton className='h-10 w-full' key={index} />
          ))}
        </div>
      ) : (data?.items.length ?? 0) === 0 ? (
        <div className='flex flex-col items-center p-10 text-center'>
          <ShieldCheck className='text-success size-9' />
          <p className='mt-3 font-medium'>
            {t('当前筛选条件下没有安全审计事件')}
          </p>
          <p className='text-muted-foreground mt-1 max-w-md text-xs'>
            {search ||
            source ||
            reviewStatus ||
            channelId ||
            model ||
            range.start
              ? t('调整或清除筛选条件后可查看其他事件。')
              : t(
                  '发生 Prompt Guard 阻断或上游安全策略拒绝后，事件会显示在这里。'
                )}
          </p>
        </div>
      ) : (
        <>
          <div className='hidden md:block'>
            <Table className='[&_tbody_tr]:!animate-none [&_tbody_tr]:!opacity-100'>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('时间')}</TableHead>
                  <TableHead>{t('来源')}</TableHead>
                  <TableHead>{t('渠道')}</TableHead>
                  <TableHead>{t('用户 / Key')}</TableHead>
                  <TableHead>{t('模型')}</TableHead>
                  <TableHead>{t('风险')}</TableHead>
                  <TableHead>{t('通知 / 计费')}</TableHead>
                  <TableHead>{t('状态')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {(data?.items ?? []).map((item) => (
                  <TableRow
                    key={item.id}
                    className='cursor-pointer'
                    tabIndex={0}
                    onClick={() => openEvent(item)}
                    onKeyDown={(event) => {
                      if (event.key === 'Enter' || event.key === ' ')
                        openEvent(item)
                    }}
                  >
                    <TableCell className='text-xs whitespace-nowrap'>
                      {dayjs(item.created_at).format('MM-DD HH:mm:ss')}
                    </TableCell>
                    <TableCell>
                      {sourceLabels[item.source] ?? item.source}
                    </TableCell>
                    <TableCell className='font-mono text-xs'>
                      {item.marketplace_channel_id || item.channel_id || '-'}
                    </TableCell>
                    <TableCell>
                      <div className='flex items-center gap-1'>
                        <span>{item.user_external_id || '-'}</span>
                        {item.user_blocked && (
                          <Badge variant='destructive' className='text-[10px]'>
                            {t('已拉黑')}
                          </Badge>
                        )}
                      </div>
                      <div className='text-muted-foreground max-w-36 truncate text-xs'>
                        ID {item.user_id || '-'} ·{' '}
                        {item.token_name || `#${item.token_id}`}
                      </div>
                    </TableCell>
                    <TableCell className='max-w-40 truncate'>
                      {item.model || '-'}
                    </TableCell>
                    <TableCell>
                      <div className='flex flex-col items-start gap-1'>
                        <RiskBadge event={item} />
                        <RecentTriggerHint event={item} />
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className='flex flex-col items-start gap-1 text-xs'>
                        <NotificationBadge event={item} />
                        <span className='text-muted-foreground'>
                          {item.billing_result === 'not_charged'
                            ? t('未计费')
                            : item.billing_result}
                        </span>
                      </div>
                    </TableCell>
                    <TableCell>
                      <ReviewStatusBadge event={item} />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
          <div className='divide-y md:hidden'>
            {(data?.items ?? []).map((item) => (
              <AuditEventCard event={item} key={item.id} onOpen={openEvent} />
            ))}
          </div>
        </>
      )}

      <footer className='flex items-center justify-between border-t px-4 py-3 text-sm'>
        <span className='text-muted-foreground'>
          {t('共 {{count}} 条', { count: data?.total ?? 0 })}
        </span>
        <div className='flex items-center gap-2'>
          <Button
            variant='outline'
            size='icon-sm'
            disabled={page <= 1}
            onClick={() => setPage((value) => value - 1)}
          >
            <ChevronLeft />
          </Button>
          <span className='tabular-nums'>
            {page} / {totalPages}
          </span>
          <Button
            variant='outline'
            size='icon-sm'
            disabled={page >= totalPages}
            onClick={() => setPage((value) => value + 1)}
          >
            <ChevronRight />
          </Button>
        </div>
      </footer>

      <Sheet
        open={Boolean(selected)}
        onOpenChange={(open) => !open && setSelected(null)}
      >
        <SheetContent className='w-full overflow-y-auto sm:max-w-2xl'>
          {selected && (
            <>
              <SheetHeader className='border-b'>
                <SheetTitle>{t('安全审计事件')}</SheetTitle>
                <SheetDescription className='font-mono'>
                  {selected.request_id || selected.id}
                </SheetDescription>
              </SheetHeader>
              <div className='space-y-5 px-4'>
                <div className='flex flex-wrap items-center gap-2'>
                  <RiskBadge event={selected} />
                  <RecentTriggerHint event={selected} />
                  <NotificationBadge event={selected} />
                  <ReviewStatusBadge event={selected} />
                </div>
                <DetailGrid event={selected} />
                <div className='bg-muted/30 flex flex-wrap gap-2 rounded-md border p-3'>
                  <Button
                    variant='outline'
                    size='sm'
                    onClick={() => inspectLogs(selected)}
                  >
                    <Waypoints />
                    {t('查看关联调用')}
                    <ExternalLink className='size-3.5' />
                  </Button>
                  {selected.user_id > 0 && (
                    <Button
                      variant='outline'
                      size='sm'
                      onClick={() => inspectUser(selected)}
                    >
                      <UserRound />
                      {admin ? t('查看用户') : t('查看渠道用户')}
                    </Button>
                  )}
                  {!admin &&
                    selected.user_id > 0 &&
                    selected.marketplace_channel_id && (
                      <Button
                        variant={
                          selected.user_blocked ? 'outline' : 'destructive'
                        }
                        size='sm'
                        disabled={marketplaceMutations.userBlock.isPending}
                        onClick={toggleUserBlock}
                      >
                        <ShieldBan />
                        {marketplaceMutations.userBlock.isPending
                          ? t('处理中')
                          : selected.user_blocked
                            ? t('解除拉黑')
                            : t('拉黑该用户')}
                      </Button>
                    )}
                  {admin && selected.channel_id > 0 && (
                    <Button
                      variant='outline'
                      size='sm'
                      onClick={() => inspectChannel(selected)}
                    >
                      <Activity />
                      {t('查看内部渠道')}
                    </Button>
                  )}
                </div>
                <DetailBlock
                  title={t('上游原始错误')}
                  value={
                    selected.upstream_error_body ||
                    selected.upstream_error_message ||
                    '-'
                  }
                />
                <DetailBlock
                  title={t('Prompt 脱敏摘要')}
                  value={selected.prompt_preview || '-'}
                />
                {selected.prompt_hash && (
                  <p className='text-muted-foreground font-mono text-xs break-all'>
                    SHA-256: {selected.prompt_hash}
                  </p>
                )}
                <div className='space-y-2'>
                  <label
                    className='text-sm font-medium'
                    htmlFor='security-audit-note'
                  >
                    {t('处理备注')}
                  </label>
                  <Textarea
                    id='security-audit-note'
                    value={note}
                    onChange={(event) => setNote(event.target.value)}
                    maxLength={1000}
                    rows={4}
                  />
                </div>
              </div>
              <SheetFooter className='border-t sm:flex-row sm:flex-wrap'>
                {(Object.keys(statusLabels) as SecurityAuditReviewStatus[]).map(
                  (status) => (
                    <Button
                      key={status}
                      variant={
                        status === selected.review_status
                          ? 'default'
                          : 'outline'
                      }
                      disabled={update.isPending}
                      onClick={() => saveStatus(status)}
                    >
                      {t(statusLabels[status])}
                    </Button>
                  )
                )}
              </SheetFooter>
            </>
          )}
        </SheetContent>
      </Sheet>
    </section>
  )
}

function DetailGrid({ event }: { event: SecurityAuditEvent }) {
  const items = [
    ['来源', sourceLabels[event.source] ?? event.source],
    ['触发时间', dayjs(event.created_at).format('YYYY-MM-DD HH:mm:ss')],
    ['用户内部 ID', String(event.user_id || '-')],
    ['用户外部 ID', event.user_external_id || '-'],
    ['Key', event.token_name || `#${event.token_id}`],
    ['渠道', event.marketplace_channel_id || String(event.channel_id || '-')],
    ['市场分组', event.marketplace_group_id || '-'],
    ['模型', event.model || '-'],
    ['协议', event.protocol || '-'],
    ['HTTP 状态', String(event.http_status || '-')],
    ['计费', event.billing_result],
    [
      '通知',
      `${notificationLabel(event.notification_status)}${event.notification_targets ? ` (${event.notification_success}/${event.notification_targets})` : ''}`,
    ],
    [
      '复核人 / 时间',
      event.reviewed_by
        ? `${event.reviewed_by} · ${event.reviewed_at ? dayjs(event.reviewed_at).format('YYYY-MM-DD HH:mm:ss') : '-'}`
        : '-',
    ],
  ]
  return (
    <dl className='grid gap-x-6 gap-y-3 sm:grid-cols-2'>
      {items.map(([label, value]) => (
        <div key={label}>
          <dt className='text-muted-foreground text-xs'>{label}</dt>
          <dd className='mt-1 text-sm break-all'>{value}</dd>
        </div>
      ))}
    </dl>
  )
}

function RiskBadge({ event }: { event: SecurityAuditEvent }) {
  const severity: SecurityAuditSeverity = event.severity || 'high'
  const tone: Record<SecurityAuditSeverity, string> = {
    low: 'border-info/20 bg-info/10 text-info',
    medium: 'border-warning/20 bg-warning/10 text-warning',
    high: 'border-destructive/20 bg-destructive/10 text-destructive',
    critical: 'border-destructive bg-destructive text-destructive-foreground',
  }
  return (
    <Badge variant='outline' className={tone[severity]}>
      {severityLabels[severity]}
      <span className='max-w-44 truncate font-normal'>{event.risk_code}</span>
    </Badge>
  )
}

function RecentTriggerHint({ event }: { event: SecurityAuditEvent }) {
  const count = event.recent_trigger_count ?? 0
  if (count <= 1) return null
  return (
    <Badge
      variant='outline'
      className='border-warning/20 bg-warning/10 text-warning'
    >
      近 24 小时 {count} 次
    </Badge>
  )
}

function NotificationBadge({ event }: { event: SecurityAuditEvent }) {
  const status = event.notification_status
  const problem = status === 'failed' || status === 'partial_failed'
  return (
    <Badge
      variant='outline'
      className={
        problem
          ? 'border-destructive/20 bg-destructive/10 text-destructive'
          : 'text-muted-foreground'
      }
    >
      <BellRing />
      {notificationLabel(status)}
    </Badge>
  )
}

function ReviewStatusBadge({ event }: { event: SecurityAuditEvent }) {
  return (
    <Badge
      variant={
        event.review_status === 'unreviewed' ? 'destructive' : 'secondary'
      }
    >
      {statusLabels[event.review_status]}
    </Badge>
  )
}

function AuditEventCard({
  event,
  onOpen,
}: {
  event: SecurityAuditEvent
  onOpen: (event: SecurityAuditEvent) => void
}) {
  const { t } = useTranslation()
  return (
    <button
      type='button'
      className='hover:bg-muted/30 focus-visible:ring-ring w-full space-y-3 p-4 text-left transition-colors focus-visible:ring-2 focus-visible:outline-none'
      onClick={() => onOpen(event)}
    >
      <div className='flex items-start justify-between gap-3'>
        <div className='min-w-0'>
          <div className='flex flex-wrap items-center gap-2'>
            <RiskBadge event={event} />
            <ReviewStatusBadge event={event} />
          </div>
          <p className='mt-2 truncate font-medium'>{event.model || '-'}</p>
        </div>
        <time className='text-muted-foreground shrink-0 text-xs tabular-nums'>
          {dayjs(event.created_at).format('MM-DD HH:mm')}
        </time>
      </div>
      <div className='grid grid-cols-2 gap-x-4 gap-y-2 text-xs'>
        <div>
          <span className='text-muted-foreground'>渠道</span>
          <p className='mt-0.5 truncate font-mono'>
            {event.marketplace_channel_id || event.channel_id || '-'}
          </p>
        </div>
        <div>
          <span className='text-muted-foreground'>用户 / Key</span>
          <p className='mt-0.5 truncate'>
            {event.user_external_id || '-'} ·{' '}
            {event.token_name || `#${event.token_id}`}
          </p>
          <p className='text-muted-foreground mt-0.5 truncate text-[11px]'>
            ID {event.user_id || '-'}
            {event.user_blocked ? ` · ${t('已拉黑')}` : ''}
          </p>
        </div>
        <div>
          <span className='text-muted-foreground'>来源</span>
          <p className='mt-0.5 truncate'>
            {sourceLabels[event.source] ?? event.source}
          </p>
        </div>
        <div>
          <span className='text-muted-foreground'>通知</span>
          <p className='mt-0.5 truncate'>
            {notificationLabel(event.notification_status)}
          </p>
        </div>
      </div>
      <RecentTriggerHint event={event} />
    </button>
  )
}

function DetailBlock({ title, value }: { title: string; value: string }) {
  return (
    <div className='space-y-2'>
      <h3 className='text-sm font-medium'>{title}</h3>
      <pre className='bg-muted max-h-72 overflow-auto rounded-md p-3 text-xs break-all whitespace-pre-wrap'>
        {value}
      </pre>
    </div>
  )
}
