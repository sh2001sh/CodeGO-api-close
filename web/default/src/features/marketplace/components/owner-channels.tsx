import { useMemo, useState } from 'react'
import interfaceI18n from '@/i18n/config'
import { getIntlLocale } from '@/i18n/format'
import {
  AlertTriangle,
  Activity,
  CheckCircle2,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  CircleDollarSign,
  Clock3,
  PauseCircle,
  Pencil,
  Plus,
  RefreshCcw,
  Search,
  WalletCards,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatQuota } from '@/lib/format'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'
import { Skeleton } from '@/components/ui/skeleton'
import { useMyMarketplaceChannels, useMyMarketplaceChannelPage } from '../hooks'
import { formatMultiplier } from '../lib/format'
import type { MarketplaceChannel, OwnerChannelIndexItem } from '../types'
import { ChannelDeleteDialog } from './channel-delete-dialog'
import { ChannelEditDialog } from './channel-edit-dialog'
import { ChannelVerificationStatus } from './channel-verification-status'
import { AutoProbeStatusView } from './model-verification'
import { OwnerChannelActions } from './owner-channel-actions'
import { IncomeMetric } from './owner-channel-metric'
import { SensitiveWordPolicyControl } from './sensitive-word-policy-control'
import { MarketplaceStatusBadge } from './status-badge'

export function OwnerChannels(props: { onAdd: () => void }) {
  const { t } = useTranslation()
  const query = useMyMarketplaceChannels()
  const [search, setSearch] = useState('')
  const [status, setStatus] = useState('')
  const [pagination, setPagination] = useState({ scope: '', page: 1 })
  const [editing, setEditing] = useState<MarketplaceChannel | null>(null)
  const [presentationEditing, setPresentationEditing] =
    useState<MarketplaceChannel | null>(null)
  const [deleting, setDeleting] = useState<MarketplaceChannel | null>(null)
  const [expandedChannelIDs, setExpandedChannelIDs] = useState<Set<string>>(
    new Set()
  )
  const channels = useMemo(() => query.data ?? [], [query.data])
  const visible = useMemo(() => {
    const keyword = search.trim().toLowerCase()
    return channels.filter(
      (channel) =>
        (!status ||
          (status === 'deleted'
            ? Boolean(channel.deleted_at)
            : !channel.deleted_at && channel.lifecycle_status === status)) &&
        (!keyword || channel.search_text.includes(keyword))
    )
  }, [channels, search, status])
  const pageScope = JSON.stringify([search, status])
  const pageCount = Math.max(1, Math.ceil(visible.length / 50))
  const page = Math.min(
    pagination.scope === pageScope ? pagination.page : 1,
    pageCount
  )
  if (pagination.scope !== pageScope || pagination.page !== page) {
    setPagination({ scope: pageScope, page })
  }
  const pageChannels = visible.slice((page - 1) * 50, page * 50)
  const details = useMyMarketplaceChannelPage(
    pageChannels.map((channel) => channel.id)
  )
  const isLoading =
    query.isLoading || (pageChannels.length > 0 && details.isLoading)
  const isError = query.isError || details.isError
  const refresh = () => {
    void query.refetch()
    if (pageChannels.length > 0) void details.refetch()
  }
  const setPage = (value: number) => {
    if (!Number.isFinite(value)) return
    setPagination({
      scope: pageScope,
      page: Math.max(1, Math.min(pageCount, Math.trunc(value))),
    })
  }
  const toggleChannel = (channelID: string) => {
    setExpandedChannelIDs((current) => {
      const next = new Set(current)
      if (next.has(channelID)) next.delete(channelID)
      else next.add(channelID)
      return next
    })
  }
  return (
    <>
      <section className='owner-channel-inventory'>
        <header className='flex flex-wrap items-center justify-between gap-4 px-4 py-5 sm:px-5'>
          <h2 className='text-lg font-semibold'>{t('我的渠道')}</h2>
          <Button
            variant='ghost'
            size='sm'
            disabled={query.isFetching || details.isFetching}
            onClick={refresh}
          >
            <RefreshCcw
              className={
                query.isFetching || details.isFetching
                  ? 'animate-spin motion-reduce:animate-none'
                  : ''
              }
              aria-hidden='true'
            />
            {t('刷新')}
          </Button>
        </header>
        {!query.isLoading && !query.isError && channels.length > 0 && (
          <OwnerOperationsPulse channels={channels} />
        )}
        <div className='owner-channel-filters'>
          <Input
            className='w-full sm:w-80'
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder={t('搜索渠道名称、ID、来源或模型')}
            aria-label={t('搜索我的渠道')}
          />
          <NativeSelect
            value={status}
            onChange={(event) => setStatus(event.target.value)}
            aria-label={t('渠道状态')}
          >
            <option value=''>{t('全部状态')}</option>
            <option value='draft'>{t('草稿')}</option>
            <option value='verifying'>{t('检测中')}</option>
            <option value='pending_review'>{t('待审核')}</option>
            <option value='active'>{t('在售')}</option>
            <option value='degraded'>{t('质量下降')}</option>
            <option value='suspended'>{t('已暂停')}</option>
            <option value='disabled'>{t('已下架')}</option>
            <option value='deleted'>{t('已删除')}</option>
          </NativeSelect>
        </div>
        {!query.isLoading && !query.isError && pageCount > 1 && (
          <nav
            className='owner-channel-pagination flex flex-wrap items-center justify-between gap-3 px-4 pb-4 sm:px-5'
            aria-label={t('pagination')}
          >
            <span className='text-muted-foreground text-sm'>
              {t('Total')} {visible.length.toLocaleString(getIntlLocale())}
            </span>
            <div className='flex flex-wrap items-center gap-2'>
              <Button
                variant='outline'
                disabled={page === 1}
                onClick={() => setPage(page - 1)}
              >
                <ChevronLeft aria-hidden='true' />
                {t('上一页')}
              </Button>
              <label className='flex items-center gap-2 text-sm'>
                <span className='sr-only'>{t('Page')}</span>
                <Input
                  type='number'
                  className='w-[68px] text-center tabular-nums'
                  min={1}
                  max={pageCount}
                  value={page}
                  aria-label={t('Page')}
                  onChange={(event) => setPage(Number(event.target.value))}
                />
                <span>/ {pageCount.toLocaleString(getIntlLocale())}</span>
              </label>
              <Button
                variant='outline'
                disabled={page === pageCount}
                onClick={() => setPage(page + 1)}
              >
                {t('下一页')}
                <ChevronRight aria-hidden='true' />
              </Button>
            </div>
          </nav>
        )}
        <div>
          {isLoading ? (
            <div className='space-y-2 p-3'>
              {Array.from({ length: 4 }).map((_, index) => (
                <Skeleton key={index} className='h-20 w-full' />
              ))}
            </div>
          ) : isError ? (
            <div className='px-4 py-12 text-center'>
              <div className='font-medium'>{t('渠道数据加载失败')}</div>
              <Button
                variant='outline'
                size='sm'
                className='mt-4'
                onClick={refresh}
              >
                <RefreshCcw />
                {t('重试')}
              </Button>
            </div>
          ) : channels.length === 0 ? (
            <div className='px-4 py-12 text-center'>
              <div className='font-medium'>{t('还没有渠道')}</div>
              <Button size='sm' className='mt-4' onClick={props.onAdd}>
                <Plus />
                {t('添加渠道')}
              </Button>
            </div>
          ) : visible.length === 0 ? (
            <p className='text-muted-foreground p-6 text-center'>
              {t('当前筛选条件下没有渠道')}
            </p>
          ) : (
            <div className='divide-border divide-y'>
              {(details.data ?? []).map((channel) => (
                <article key={channel.id} className='owner-channel-record'>
                  <div>
                    <div className='min-w-0'>
                      <div className='flex flex-wrap items-center gap-2'>
                        <span
                          className='min-w-0 flex-1 truncate text-[15px] font-semibold'
                          title={
                            channel.custom_name
                              ? `${channel.id}-${channel.custom_name}`
                              : channel.system_display_name
                          }
                        >
                          {channel.custom_name
                            ? `${channel.id}-${channel.custom_name}`
                            : channel.system_display_name}
                        </span>
                        {channel.deleted_at ? (
                          <Badge
                            variant='outline'
                            className='text-muted-foreground'
                          >
                            {t('已删除')}
                          </Badge>
                        ) : (
                          <MarketplaceStatusBadge
                            status={channel.lifecycle_status}
                          />
                        )}
                        <span className='owner-channel-multiplier app-numeric'>
                          {formatMultiplier(channel.multiplier)}x
                        </span>
                      </div>
                      <dl className='text-muted-foreground mt-2.5 grid grid-cols-2 gap-x-4 gap-y-1 text-xs sm:grid-cols-3 xl:grid-cols-5'>
                        <div className='flex min-w-0 items-baseline gap-1.5'>
                          <dt className='shrink-0 opacity-80'>ID</dt>
                          <dd className='truncate font-mono tabular-nums'>
                            {channel.id}
                          </dd>
                        </div>
                        <div className='flex min-w-0 items-baseline gap-1.5'>
                          <dt className='shrink-0 opacity-80'>{t('协议')}</dt>
                          <dd className='truncate'>{channel.provider_type}</dd>
                        </div>
                        <div className='flex min-w-0 items-baseline gap-1.5'>
                          <dt className='shrink-0 opacity-80'>
                            {t('渠道主声明')}
                          </dt>
                          <dd className='truncate'>
                            {channel.submitted_source_label}
                          </dd>
                        </div>
                        <div className='flex min-w-0 items-baseline gap-1.5'>
                          <dt className='shrink-0 opacity-80'>{t('模型')}</dt>
                          <dd className='tabular-nums'>
                            {channel.declared_models.length} {t('个')}
                          </dd>
                        </div>
                        <div className='flex min-w-0 items-baseline gap-1.5'>
                          <dt className='shrink-0 opacity-80'>
                            {interfaceI18n.t('Key')}
                          </dt>
                          <dd className='truncate font-mono'>
                            ····{channel.credential_tail}
                          </dd>
                        </div>
                      </dl>
                      <div className='mt-3 flex flex-wrap items-center gap-2'>
                        <Button
                          variant='ghost'
                          size='sm'
                          className='text-muted-foreground h-8 px-2'
                          onClick={() => toggleChannel(channel.id)}
                          aria-expanded={expandedChannelIDs.has(channel.id)}
                          aria-controls={`owner-channel-details-${channel.id}`}
                        >
                          <ChevronDown
                            className={`transition-transform motion-reduce:transition-none ${expandedChannelIDs.has(channel.id) ? 'rotate-180' : ''}`}
                          />
                          {expandedChannelIDs.has(channel.id)
                            ? t('收起详情')
                            : t('查看详情')}
                        </Button>
                        {!channel.deleted_at && (
                          <Button
                            variant='outline'
                            size='sm'
                            className='h-8 px-2'
                            onClick={() => setPresentationEditing(channel)}
                          >
                            <Pencil aria-hidden />
                            {t('商品展示')}
                          </Button>
                        )}
                        {!channel.deleted_at && (
                          <Button
                            variant='outline'
                            size='sm'
                            className='h-8 px-2'
                            onClick={() => setEditing(channel)}
                          >
                            {t('编辑渠道')}
                          </Button>
                        )}
                        {!expandedChannelIDs.has(channel.id) && (
                          <span className='text-muted-foreground flex flex-wrap items-center gap-x-3 gap-y-1 text-xs'>
                            <ChannelHealthSummary channel={channel} />
                          </span>
                        )}
                      </div>
                      {expandedChannelIDs.has(channel.id) && (
                        <div
                          id={`owner-channel-details-${channel.id}`}
                          className='owner-channel-details mt-3'
                        >
                          {!channel.deleted_at && (
                            <div className='border-border/60 mb-3 border-b pb-3'>
                              <OwnerChannelActions
                                channel={channel}
                                onEdit={() => setEditing(channel)}
                                onDelete={() => setDeleting(channel)}
                              />
                            </div>
                          )}
                          <div className='border-border/60 mb-3 flex flex-wrap items-center justify-between gap-3 border-b pb-3'>
                            <span className='text-muted-foreground text-xs'>
                              {t('渠道操作')}
                            </span>
                            <span />
                          </div>
                          <ReviewSteps channel={channel} />
                          {channel.last_review_reason && (
                            <p className='text-muted-foreground mt-1 text-xs'>
                              {channel.last_review_reason}
                            </p>
                          )}
                          {channel.source_label_review_reason &&
                            channel.source_label_review_reason !==
                              channel.last_review_reason && (
                              <p className='text-muted-foreground mt-1 text-xs'>
                                {channel.source_label_review_reason}
                              </p>
                            )}
                          <ChannelVerificationStatus channel={channel} />
                          {!channel.deleted_at && (
                            <SensitiveWordPolicyControl channel={channel} />
                          )}
                          <AutoProbeStatusView
                            enabled={channel.auto_probe_enabled}
                            intervalMinutes={
                              channel.auto_probe_interval_minutes
                            }
                            model={channel.auto_probe_model}
                            status={channel.auto_probe_last_status}
                            checkedAt={channel.auto_probe_last_at}
                          />
                          <div className='border-border/60 mt-4 border-t pt-3'>
                            <div className='text-muted-foreground mb-3 text-xs font-medium'>
                              {t('收入与结算')}
                            </div>
                            <div className='grid gap-2 sm:grid-cols-2 xl:grid-cols-4'>
                              <IncomeMetric
                                icon={CircleDollarSign}
                                label={t('累计收入')}
                                value={formatQuota(channel.total_income)}
                              />
                              <IncomeMetric
                                icon={Clock3}
                                label={t('待结算')}
                                value={formatQuota(channel.pending_income)}
                              />
                              <IncomeMetric
                                icon={WalletCards}
                                label={t('已到账')}
                                value={formatQuota(channel.released_income)}
                              />
                              <IncomeMetric
                                icon={WalletCards}
                                label={t('已回收额度')}
                                value={formatQuota(channel.reclaimed_income)}
                              />
                              <IncomeMetric
                                icon={Activity}
                                label={t('结算请求')}
                                value={String(channel.request_count)}
                              />
                            </div>
                          </div>
                        </div>
                      )}
                    </div>
                  </div>
                </article>
              ))}
            </div>
          )}
        </div>
        <details className='owner-channel-policy'>
          <summary>{t('渠道关停与冻结额度规则')}</summary>
          <p>
            {t(
              '暂停、下架或删除渠道都不会没收收益；冻结期内的待结算额度会继续冻结，并在原定时间到账。已删除渠道的使用日志与结算记录仍可查询。'
            )}
          </p>
        </details>
      </section>
      <ChannelEditDialog
        channel={presentationEditing}
        open={presentationEditing != null}
        presentationOnly
        onOpenChange={(open) => {
          if (!open) setPresentationEditing(null)
        }}
      />
      <ChannelEditDialog
        channel={editing}
        open={editing != null}
        onOpenChange={(open) => {
          if (!open) setEditing(null)
        }}
      />
      <ChannelDeleteDialog
        channel={deleting}
        open={deleting != null}
        onOpenChange={(open) => {
          if (!open) setDeleting(null)
        }}
      />
    </>
  )
}

function OwnerOperationsPulse(props: { channels: OwnerChannelIndexItem[] }) {
  const { t } = useTranslation()
  const channels = props.channels.filter((channel) => !channel.deleted_at)
  const active = channels.filter(
    (channel) => channel.lifecycle_status === 'active'
  ).length
  const attention = channels.filter((channel) => channel.needs_attention).length
  const checking = channels.filter((channel) => channel.checking).length
  const cards = [
    {
      icon: CheckCircle2,
      label: t('正常运行'),
      value: active,
      tone: 'text-success bg-success/10',
    },
    {
      icon: AlertTriangle,
      label: t('需要处理'),
      value: attention,
      tone: 'text-warning bg-warning/10',
    },
    {
      icon: Search,
      label: t('正在检测'),
      value: checking,
      tone: 'text-info bg-info/10',
    },
    {
      icon: PauseCircle,
      label: t('已暂停'),
      value: channels.filter(
        (channel) => channel.lifecycle_status === 'suspended'
      ).length,
      tone: 'text-muted-foreground bg-muted',
    },
  ]
  return (
    <section className='owner-channel-pulse' aria-label={t('渠道运行概览')}>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <div>
          <h3 className='text-sm font-semibold'>{t('运营信号')}</h3>
        </div>
        <dl className='owner-channel-pulse-counts'>
          {cards.map(({ icon: Icon, label, value, tone }) => (
            <div key={label} className='flex items-center gap-2'>
              <span
                className={`flex size-7 shrink-0 items-center justify-center rounded-md ${tone}`}
              >
                <Icon className='size-4' aria-hidden='true' />
              </span>
              <dt className='text-muted-foreground text-xs'>{label}</dt>
              <dd className='font-medium tabular-nums'>{value}</dd>
            </div>
          ))}
        </dl>
      </div>
    </section>
  )
}

function ChannelHealthSummary(props: { channel: MarketplaceChannel }) {
  const { t } = useTranslation()
  const failedModels = props.channel.model_verification_results.filter(
    (result) => result.status === 'failed' || !result.listed
  ).length
  const status = props.channel.connectivity_test_status
  const label =
    status === 'passed'
      ? t('检测通过')
      : status === 'failed'
        ? t('检测失败')
        : status === 'running' || status === 'queued'
          ? t('检测中')
          : t('未检测')
  const tone =
    status === 'passed'
      ? 'text-success'
      : status === 'failed'
        ? 'text-destructive'
        : 'text-muted-foreground'
  return (
    <span className='inline-flex items-center gap-1'>
      <span
        className={`size-1.5 rounded-full ${status === 'passed' ? 'bg-success' : status === 'failed' ? 'bg-destructive' : 'bg-muted-foreground'}`}
        aria-hidden='true'
      />
      <span className={tone}>{label}</span>
      {failedModels > 0 && (
        <span className='text-destructive'>
          {t('{{count}} 个失败模型', { count: failedModels })}
        </span>
      )}
      {props.channel.request_count > 0 && (
        <span>
          {t('{{count}} 次调用', { count: props.channel.request_count })}
        </span>
      )}
    </span>
  )
}

/** 上线进度：提交 → 连通检测 → 上线 */
function ReviewSteps(props: { channel: MarketplaceChannel }) {
  const { t } = useTranslation()
  const c = props.channel
  const connectivity =
    c.connectivity_test_status === 'passed'
      ? 2
      : c.connectivity_test_status === 'failed'
        ? 1
        : 0
  const online =
    c.lifecycle_status === 'active'
      ? 2
      : c.lifecycle_status === 'disabled' || c.lifecycle_status === 'suspended'
        ? 1
        : 0

  const steps = [
    { label: t('提交材料'), state: 2 },
    { label: t('连通检测'), state: connectivity },
    { label: t('上线服务'), state: online },
  ]

  return (
    <ol className='mt-3 flex flex-wrap items-center gap-x-1.5 gap-y-1 text-xs'>
      {steps.map((step, index) => (
        <li key={step.label} className='flex items-center gap-1.5'>
          {index > 0 && <span className='bg-border h-px w-4' aria-hidden />}
          <span
            className={
              step.state === 2
                ? 'text-primary flex items-center gap-1 font-medium'
                : step.state === 1
                  ? 'text-destructive flex items-center gap-1 font-medium'
                  : 'text-muted-foreground flex items-center gap-1'
            }
          >
            <span
              className={
                step.state === 2
                  ? 'bg-primary size-1.5 rounded-full'
                  : step.state === 1
                    ? 'bg-destructive size-1.5 rounded-full'
                    : 'bg-border size-1.5 rounded-full'
              }
              aria-hidden
            />
            {step.label}
          </span>
        </li>
      ))}
    </ol>
  )
}
