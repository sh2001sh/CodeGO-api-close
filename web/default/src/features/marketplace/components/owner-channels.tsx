import { useState } from 'react'
import {
  Activity,
  ChevronDown,
  CircleDollarSign,
  Clock3,
  Plus,
  RefreshCcw,
  WalletCards,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatQuota } from '@/lib/format'
import { Button } from '@/components/ui/button'
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
  const [editing, setEditing] = useState<MarketplaceChannel | null>(null)
  const [deleting, setDeleting] = useState<MarketplaceChannel | null>(null)
  const [expandedChannelIDs, setExpandedChannelIDs] = useState<Set<number>>(
    new Set()
  )
  const channels = query.data ?? []
  const toggleChannel = (channelID: number) => {
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
        <OwnerIncomeOverview channels={channels} />
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
          ) : (
            <div className='divide-border divide-y'>
              {channels.map((channel) => (
                <article
                  key={channel.id}
                  className='hover:bg-muted/20 px-4 py-4 transition-colors sm:px-5'
                >
                  <div className='flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between'>
                    <div className='min-w-0 flex-1'>
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
                        {!expandedChannelIDs.has(channel.id) && (
                          <span className='text-muted-foreground text-xs'>
                            {t('检测、模型与收入信息已折叠')}
                          </span>
                        )}
                      </div>
                      {expandedChannelIDs.has(channel.id) && (
                        <div
                          id={`owner-channel-details-${channel.id}`}
                          className='mt-3'
                        >
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
                                icon={Activity}
                                label={t('结算请求')}
                                value={String(channel.request_count)}
                              />
                            </div>
                          </div>
                        </div>
                      )}
                    </div>
                    <OwnerChannelActions
                      channel={channel}
                      onEdit={() => setEditing(channel)}
                      onDelete={() => setDeleting(channel)}
                    />
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
