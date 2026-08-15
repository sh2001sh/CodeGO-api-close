import { useState } from 'react'
import { Pencil, ShieldCheck, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { useAdminMarketplaceChannels } from '../hooks'
import type { MarketplaceChannel } from '../types'
import { ChannelDeleteDialog } from './channel-delete-dialog'
import { ChannelEditDialog } from './channel-edit-dialog'
import { ModelConsistencyBadge } from './model-verification'
import { MarketplaceStatusBadge } from './status-badge'

export function AdminGovernance() {
  const { t } = useTranslation()
  const query = useAdminMarketplaceChannels(true)
  const [editing, setEditing] = useState<MarketplaceChannel | null>(null)
  const [deleting, setDeleting] = useState<MarketplaceChannel | null>(null)

  return (
    <div className='space-y-4'>
      <div className='border-success/35 bg-success/8 flex items-start gap-3 rounded-md border p-3'>
        <ShieldCheck className='text-success mt-0.5 size-4' />
        <p className='text-sm leading-6'>
          {t(
            '固定来源不再需要人工审核。管理员可直接编辑协议、来源、模型、连接和服务策略；连接或模型变更会重新检测。'
          )}
        </p>
      </div>
      <section className='border-border overflow-hidden rounded-md border'>
        {query.isLoading ? (
          <div className='space-y-2 p-3'>
            {Array.from({ length: 5 }).map((_, index) => (
              <Skeleton key={index} className='h-24 w-full' />
            ))}
          </div>
        ) : (query.data ?? []).length === 0 ? (
          <div className='px-4 py-12 text-center text-sm'>
            {t('当前没有待治理渠道')}
          </div>
        ) : (
          <div className='divide-border divide-y'>
            {(query.data ?? []).map((channel) => (
              <div
                key={channel.id}
                className='flex flex-col gap-3 px-4 py-4 sm:flex-row sm:items-center sm:justify-between'
              >
                <div className='min-w-0'>
                  <div className='flex flex-wrap items-center gap-2'>
                    <span className='font-medium'>
                      {channel.system_display_name}
                    </span>
                    <MarketplaceStatusBadge status={channel.lifecycle_status} />
                    <ModelConsistencyBadge
                      status={channel.model_consistency_status}
                    />
                  </div>
                  <div className='text-muted-foreground mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs'>
                    <span className='tabular-nums'>ID {channel.id}</span>
                    <span>{channel.provider_type}</span>
                    <span className='text-foreground font-medium'>
                      {t('来源')}: {channel.submitted_source_label || '--'}
                    </span>
                    <span>
                      {channel.declared_models.length} {t('个模型')}
                    </span>
                    <span>{channel.multiplier.toFixed(2)}x</span>
                    <span>
                      {t('检测')}: {channel.verification_status}
                    </span>
                    <span>
                      {t('并发')} {channel.max_concurrency}
                    </span>
                    <span>QPS {channel.qps}</span>
                  </div>
                </div>
                <div className='flex shrink-0 items-center gap-2'>
                  <Button
                    variant='outline'
                    size='sm'
                    onClick={() => setEditing(channel)}
                  >
                    <Pencil />
                    {t('编辑渠道')}
                  </Button>
                  <Button
                    variant='ghost'
                    size='sm'
                    className='text-destructive hover:text-destructive'
                    onClick={() => setDeleting(channel)}
                  >
                    <Trash2 />
                    {t('删除')}
                  </Button>
                </div>
              </div>
            ))}
          </div>
        )}
      </section>
      <ChannelEditDialog
        admin
        channel={editing}
        open={editing != null}
        onOpenChange={(open) => {
          if (!open) setEditing(null)
        }}
      />
      <ChannelDeleteDialog
        admin
        channel={deleting}
        open={deleting != null}
        onOpenChange={(open) => {
          if (!open) setDeleting(null)
        }}
      />
    </div>
  )
}
