import { useState } from 'react'
import {
  Activity,
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
import { AutoProbeStatusView, GPT56MappingStatusView } from './model-verification'
import { OwnerChannelActions } from './owner-channel-actions'
import { IncomeMetric } from './owner-channel-metric'
import { OwnerIncomeOverview } from './owner-income-overview'
import { MarketplaceStatusBadge } from './status-badge'

export function OwnerChannels(props: { onAdd: () => void }) {
  const { t } = useTranslation()
  const query = useMyMarketplaceChannels()
  const [editing, setEditing] = useState<MarketplaceChannel | null>(null)
  const [deleting, setDeleting] = useState<MarketplaceChannel | null>(null)
  const channels = query.data ?? []
  return (
    <>
      <section className='border-border bg-card overflow-hidden rounded-lg border'>
        <header className='flex flex-wrap items-center justify-between gap-4 px-4 py-5 sm:px-5'>
          <div className='flex min-w-0 items-start gap-3'>
            <span className='bg-primary/10 text-primary flex size-10 shrink-0 items-center justify-center rounded-md'>
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
                <div
                  key={channel.id}
                  className='hover:bg-muted/20 flex flex-col gap-4 px-4 py-4 transition-colors sm:px-5 lg:flex-row lg:items-start lg:justify-between'
                >
                  <div className='min-w-0 flex-1'>
                    <div className='flex flex-wrap items-center gap-2'>
                      <span className='text-[15px] font-semibold'>
                        {channel.system_display_name}
                      </span>
                      <MarketplaceStatusBadge
                        status={channel.lifecycle_status}
                      />
                    </div>
                    <div className='text-muted-foreground mt-2 flex flex-wrap items-center gap-x-4 gap-y-1.5 text-xs'>
                      <span className='tabular-nums'>ID {channel.id}</span>
                      <span>{channel.provider_type}</span>
                      <span>
                        {channel.submitted_source_label} ·{' '}
                        {channel.source_label_status === 'approved'
                          ? t('来源已审核')
                          : channel.source_label_status === 'rejected'
                            ? t('来源未通过')
                            : t('来源待审核')}
                      </span>
                      <span>Key ····{channel.credential_tail}</span>
                      <span>
                        {channel.declared_models.length} {t('个模型')}
                      </span>
                      <span className='text-foreground font-medium tabular-nums'>
                        {formatMultiplier(channel.multiplier)}x
                      </span>
                    </div>
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
                    <GPT56MappingStatusView
                      models={channel.declared_models}
                      status={channel.gpt56_mapping_status}
                      results={channel.gpt56_mapping_results}
                      checkedAt={channel.gpt56_mapping_checked_at}
                    />
                    <AutoProbeStatusView
                      enabled={channel.auto_probe_enabled}
                      intervalMinutes={channel.auto_probe_interval_minutes}
                      model={channel.auto_probe_model}
                      status={channel.auto_probe_last_status}
                      checkedAt={channel.auto_probe_last_at}
                    />
                    <div className='mt-4 grid gap-2 sm:grid-cols-2 xl:grid-cols-4'>
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
                  <OwnerChannelActions
                    channel={channel}
                    onEdit={() => setEditing(channel)}
                    onDelete={() => setDeleting(channel)}
                  />
                </div>
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
