import { useState } from 'react'
import {
  AlertTriangle,
  Activity,
  CheckCircle2,
  ChevronDown,
  CircleDollarSign,
  Clock3,
  PauseCircle,
  Plus,
  RefreshCcw,
  Search,
  WalletCards,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatQuota } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'
import { Skeleton } from '@/components/ui/skeleton'
import { useMyMarketplaceChannels } from '../hooks'
import { formatMultiplier } from '../lib/format'
import type { MarketplaceChannel } from '../types'
import { ChannelDeleteDialog } from './channel-delete-dialog'
import { ChannelEditDialog } from './channel-edit-dialog'
import { ChannelVerificationStatus } from './channel-verification-status'
import { GPT56MappingStatusView } from './gpt56-mapping-report'
import { AutoProbeStatusView } from './model-verification'
import { OwnerChannelActions } from './owner-channel-actions'
import { IncomeMetric } from './owner-channel-metric'
import { OwnerIncomeOverview } from './owner-income-overview'
import { SensitiveWordPolicyControl } from './sensitive-word-policy-control'
import { MarketplaceStatusBadge } from './status-badge'

export function OwnerChannels(props: { onAdd: () => void }) {
  const { t } = useTranslation()
  const query = useMyMarketplaceChannels()
  const [search, setSearch] = useState('')
  const [status, setStatus] = useState('')
  const [editing, setEditing] = useState<MarketplaceChannel | null>(null)
  const [deleting, setDeleting] = useState<MarketplaceChannel | null>(null)
  const [expandedChannelIDs, setExpandedChannelIDs] = useState<Set<string>>(
    new Set()
  )
  const channels = query.data ?? []
  const keyword = search.trim().toLowerCase()
  const visible = channels.filter(
    (channel) =>
      (!status || channel.lifecycle_status === status) &&
      (!keyword ||
        [
          channel.id,
          channel.system_display_name,
          channel.submitted_source_label,
          ...channel.declared_models,
        ].some((value) => value.toLowerCase().includes(keyword)))
  )
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
      <section className='border-border bg-card overflow-hidden rounded-lg border'>
        <header className='flex flex-wrap items-center justify-between gap-4 px-4 py-5 sm:px-5'>
          <div className='flex min-w-0 items-start gap-3'>
            <span className='border-primary/30 text-primary bg-primary/[0.04] flex size-10 shrink-0 items-center justify-center rounded-md border'>
              <WalletCards className='size-5' />
            </span>
            <div>
              <h3 className='font-semibold'>{t('渠道经营台')}</h3>
              <p className='text-muted-foreground mt-1 text-sm'>
                {t('统一查看服务状态、审核进度、收入与结算。')}
              </p>
            </div>
          </div>
          <Button size='sm' onClick={props.onAdd}>
            <Plus />
            {t('添加渠道')}
          </Button>
        </header>
        <div className='border-border bg-muted/30 text-muted-foreground border-y px-4 py-3 text-xs leading-5 sm:px-5'>
          <strong>{t('渠道关停与冻结额度规则')}</strong>：
          {t(
            '暂停渠道不会没收收益；只有删除或下架渠道时，仍处于冻结期的待结算额度才会按平台规则回收。'
          )}
        </div>
        <OwnerOperationsPulse channels={channels} />
        <OwnerIncomeOverview channels={channels} />
        <div className='flex flex-wrap gap-2 border-b p-4'>
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
          </NativeSelect>
        </div>
        <div>
          {query.isLoading ? (
            <div className='space-y-2 p-3'>
              {Array.from({ length: 4 }).map((_, index) => (
                <Skeleton key={index} className='h-20 w-full' />
              ))}
            </div>
          ) : query.isError ? (
            <div className='px-4 py-12 text-center'>
              <div className='font-medium'>{t('渠道数据加载失败')}</div>
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
          ) : channels.length === 0 ? (
            <div className='px-4 py-12 text-center'>
              <div className='font-medium'>{t('还没有渠道')}</div>
              <p className='text-muted-foreground mt-1 text-sm'>
                {t('添加第一个渠道后，可在这里查看审核状态和收入。')}
              </p>
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
              {visible.map((channel) => (
                <article
                  key={channel.id}
                  className='hover:bg-muted/20 px-4 py-4 transition-colors sm:px-5'
                >
                  <div>
                    <div className='min-w-0'>
                      <div className='flex flex-wrap items-center gap-2'>
                        <span className='text-[15px] font-semibold'>
                          {channel.system_display_name}
                        </span>
                        <MarketplaceStatusBadge
                          status={channel.lifecycle_status}
                        />
                        <span className='border-primary/25 bg-primary/[0.07] text-primary app-numeric ml-1 rounded-[4px] border px-1.5 py-0.5 text-xs font-semibold tabular-nums'>
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
                          <dt className='shrink-0 opacity-80'>{t('来源')}</dt>
                          <dd className='truncate'>
                            {channel.submitted_source_label}
                            {channel.source_label_status === 'approved'
                              ? ` · ${t('已审核')}`
                              : channel.source_label_status === 'rejected'
                                ? ` · ${t('未通过')}`
                                : ` · ${t('待审核')}`}
                          </dd>
                        </div>
                        <div className='flex min-w-0 items-baseline gap-1.5'>
                          <dt className='shrink-0 opacity-80'>{t('模型')}</dt>
                          <dd className='tabular-nums'>
                            {channel.declared_models.length} {t('个')}
                          </dd>
                        </div>
                        <div className='flex min-w-0 items-baseline gap-1.5'>
                          <dt className='shrink-0 opacity-80'>Key</dt>
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
                        <Button
                          variant='outline'
                          size='sm'
                          className='h-8 px-2'
                          onClick={() => setEditing(channel)}
                        >
                          {t('编辑渠道')}
                        </Button>
                        {!expandedChannelIDs.has(channel.id) && (
                          <span className='text-muted-foreground flex flex-wrap items-center gap-x-3 gap-y-1 text-xs'>
                            <ChannelHealthSummary channel={channel} />
                            <span>{t('检测、模型与收入信息已折叠')}</span>
                          </span>
                        )}
                      </div>
                      {expandedChannelIDs.has(channel.id) && (
                        <div
                          id={`owner-channel-details-${channel.id}`}
                          className='mt-3'
                        >
                          <div className='border-border/60 mb-3 border-b pb-3'>
                            <OwnerChannelActions
                              channel={channel}
                              onEdit={() => setEditing(channel)}
                              onDelete={() => setDeleting(channel)}
                            />
                          </div>
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
                          <SensitiveWordPolicyControl channel={channel} />
                          <GPT56MappingStatusView
                            models={channel.declared_models}
                            status={channel.gpt56_mapping_status}
                            results={channel.gpt56_mapping_results}
                            checkedAt={channel.gpt56_mapping_checked_at}
                            level={channel.gpt56_mapping_level}
                            trigger={channel.gpt56_mapping_trigger}
                            history={channel.gpt56_mapping_history}
                          />
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
                            <div className='text-muted-foreground mb-2 text-[11px] font-medium tracking-[0.14em] uppercase'>
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
      </section>
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

function OwnerOperationsPulse(props: { channels: MarketplaceChannel[] }) {
  const { t } = useTranslation()
  const channels = props.channels
  const active = channels.filter((channel) => channel.lifecycle_status === 'active').length
  const attention = channels.filter((channel) => {
    const failedModels = channel.model_verification_results.filter(
      (result) => result.status === 'failed' || !result.listed
    ).length
    return (
      ['degraded', 'suspended'].includes(channel.lifecycle_status) ||
      channel.connectivity_test_status === 'failed' ||
      failedModels > 0 ||
      channel.source_label_status === 'rejected'
    )
  }).length
  const checking = channels.filter((channel) =>
    ['verifying', 'queued', 'running'].includes(channel.lifecycle_status) ||
    ['queued', 'running'].includes(channel.connectivity_test_status)
  ).length
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
      value: channels.filter((channel) => channel.lifecycle_status === 'suspended').length,
      tone: 'text-muted-foreground bg-muted',
    },
  ]
  return (
    <section className='border-border bg-muted/15 border-y px-4 py-3 sm:px-5' aria-label={t('渠道运行概览')}>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <div>
          <h4 className='text-sm font-semibold'>{t('运营信号')}</h4>
          <p className='text-muted-foreground mt-0.5 text-xs'>
            {attention > 0
              ? t('有 {{count}} 个渠道需要关注，建议先处理检测或服务状态。', { count: attention })
              : t('当前没有需要立即处理的渠道。')}
          </p>
        </div>
        <div className='grid grid-cols-2 gap-2 sm:grid-cols-4'>
          {cards.map(({ icon: Icon, label, value, tone }) => (
            <div key={label} className='flex min-w-24 items-center gap-2 rounded-md border border-border/70 bg-card px-2.5 py-2'>
              <span className={`flex size-7 shrink-0 items-center justify-center rounded-md ${tone}`}>
                <Icon className='size-4' aria-hidden='true' />
              </span>
              <span className='min-w-0'>
                <span className='text-muted-foreground block truncate text-[11px]'>{label}</span>
                <span className='mt-0.5 block text-base font-semibold tabular-nums'>{value}</span>
              </span>
            </div>
          ))}
        </div>
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
  const tone = status === 'passed' ? 'text-success' : status === 'failed' ? 'text-destructive' : 'text-muted-foreground'
  return (
    <span className='inline-flex items-center gap-1'>
      <span className={`size-1.5 rounded-full ${status === 'passed' ? 'bg-success' : status === 'failed' ? 'bg-destructive' : 'bg-muted-foreground'}`} aria-hidden='true' />
      <span className={tone}>{label}</span>
      {failedModels > 0 && <span className='text-destructive'>{t('{{count}} 个失败模型', { count: failedModels })}</span>}
      {props.channel.request_count > 0 && <span>{t('{{count}} 次调用', { count: props.channel.request_count })}</span>}
    </span>
  )
}

/** 审核进度：提交 → 连通检测 → 来源审核 → 上线 */
function ReviewSteps(props: { channel: MarketplaceChannel }) {
  const { t } = useTranslation()
  const c = props.channel
  const connectivity =
    c.connectivity_test_status === 'passed'
      ? 2
      : c.connectivity_test_status === 'failed'
        ? 1
        : 0
  const source =
    c.source_label_status === 'approved'
      ? 2
      : c.source_label_status === 'rejected'
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
    { label: t('来源审核'), state: source },
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
