import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { useMarketplaceChannelDelete } from '../hooks'
import type { MarketplaceChannel } from '../types'

export function ChannelDeleteDialog(props: {
  admin?: boolean
  channel: MarketplaceChannel | null
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const remove = useMarketplaceChannelDelete(Boolean(props.admin))
  const channel = props.channel

  const handleDelete = async () => {
    if (!channel) return
    try {
      await remove.mutateAsync(channel.id)
      toast.success(t('渠道已删除'))
      props.onOpenChange(false)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('删除渠道失败'))
    }
  }

  return (
    <ConfirmDialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('删除渠道')}
      desc={t(
        '删除后，该渠道会立即从分组市场、API Key 分组和 Auto 路由池中移除，无法恢复。历史检测与结算记录将保留用于审计。'
      )}
      confirmText={t('确认删除')}
      destructive
      isLoading={remove.isPending}
      handleConfirm={() => void handleDelete()}
    >
      {channel && (
        <div className='border-border bg-muted/30 rounded-md border px-3 py-2.5 text-sm'>
          <div className='font-medium'>{channel.system_display_name}</div>
          <div className='text-muted-foreground mt-1 text-xs tabular-nums'>
            {t('渠道 ID')}: {channel.id}
          </div>
        </div>
      )}
    </ConfirmDialog>
  )
}
