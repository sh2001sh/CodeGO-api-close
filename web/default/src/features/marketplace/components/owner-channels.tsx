import { useState } from 'react'
import {
  Activity,
  CircleDollarSign,
  Clock3,
  Loader2,
  Pause,
  Pencil,
  Play,
  Plus,
  RefreshCcw,
  WalletCards,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatQuota } from '@/lib/format'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { useMarketplaceMutations, useMyMarketplaceChannels } from '../hooks'
import { formatMultiplier } from '../lib/format'
import type { MarketplaceChannel } from '../types'
import { ChannelEditDialog } from './channel-edit-dialog'
import { ChannelVerificationStatus } from './channel-verification-status'
import { MarketplaceStatusBadge } from './status-badge'
import { IncomeMetric } from './owner-channel-metric'

export function OwnerChannels(props: { onAdd: () => void }) {
  const { t } = useTranslation()
  const query = useMyMarketplaceChannels()
  const mutations = useMarketplaceMutations()
  const [editing, setEditing] = useState<MarketplaceChannel | null>(null)
  const channels = query.data ?? []
  const metrics = [
    {
      icon: Activity,
      label: t('渠道总数'),
      value: String(channels.length),
    },
    {
      icon: RefreshCcw,
      label: t('运行中'),
      value: String(
        channels.filter((channel) =>
          ['active', 'degraded'].includes(channel.lifecycle_status)
        ).length
      ),
    },
    {
      icon: CircleDollarSign,
      label: t('累计收入'),
      value: formatQuota(
        channels.reduce((total, channel) => total + channel.total_income, 0)
      ),
    },
    {
      icon: Clock3,
      label: t('待结算'),
      value: formatQuota(
        channels.reduce((total, channel) => total + channel.pending_income, 0)
      ),
    },
  ]

  const act = async (
    channel: MarketplaceChannel,
    action: 'verify' | 'pause' | 'resume'
  ) => {
    try {
      if (action === 'verify') {
        await mutations.verify.mutateAsync(channel.id)
        toast.info(t('检测已开始，页面会自动更新进度和结果'))
        return
      } else
        await mutations.pause.mutateAsync({
          id: channel.id,
          paused: action === 'pause',
        })
      toast.success(action === 'pause' ? t('渠道已暂停') : t('渠道已恢复'))
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('操作失败'))
    }
  }

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
        <div className='border-border bg-muted/20 grid border-y sm:grid-cols-2 xl:grid-cols-4'>
          {metrics.map(({ icon: Icon, label, value }) => (
            <div
              key={label}
              className='border-border flex min-h-24 items-center gap-3 border-b px-4 py-4 last:border-r-0 sm:border-r xl:border-b-0'
            >
              <span className='bg-background flex size-9 shrink-0 items-center justify-center rounded-md border shadow-sm'>
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
                  <div className='flex shrink-0 flex-wrap items-center gap-2 lg:justify-end'>
                    <Button
                      variant='outline'
                      size='sm'
                      onClick={() => setEditing(channel)}
                    >
                      <Pencil />
                      {t('编辑')}
                    </Button>
                    <Button
                      variant='outline'
                      size='sm'
                      onClick={() => void act(channel, 'verify')}
                      disabled={
                        mutations.verify.isPending ||
                        channel.lifecycle_status === 'verifying' ||
                        ['queued', 'running'].includes(
                          channel.verification_status
                        )
                      }
                    >
                      <RefreshCcw
                        className={cn(
                          channel.lifecycle_status === 'verifying' &&
                            'animate-spin'
                        )}
                      />
                      {channel.lifecycle_status === 'verifying'
                        ? t('检测中')
                        : t('重新检测')}
                    </Button>
                    {channel.lifecycle_status === 'suspended' ? (
                      <Button
                        variant='outline'
                        size='sm'
                        onClick={() => void act(channel, 'resume')}
                      >
                        <Play />
                        {t('恢复')}
                      </Button>
                    ) : (
                      <Button
                        variant='outline'
                        size='sm'
                        onClick={() => void act(channel, 'pause')}
                        disabled={
                          channel.lifecycle_status !== 'active' &&
                          channel.lifecycle_status !== 'degraded'
                        }
                      >
                        <Pause />
                        {t('暂停')}
                      </Button>
                    )}
                    {(mutations.pause.isPending ||
                      mutations.verify.isPending) && (
                      <Loader2 className='text-muted-foreground size-4 animate-spin' />
                    )}
                  </div>
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
    </>
  )
}
