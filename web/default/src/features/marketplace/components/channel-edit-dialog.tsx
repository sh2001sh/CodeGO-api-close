import { useTranslation } from 'react-i18next'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import type { MarketplaceChannel } from '../types'
import { ChannelEditorForm } from './channel-create-form'

export function ChannelEditDialog(props: {
  channel: MarketplaceChannel | null
  open: boolean
  admin?: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  if (!props.channel) return null
  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='flex h-[min(92dvh,900px)] max-h-[calc(100dvh-1rem)] max-w-6xl flex-col gap-0 overflow-hidden p-0 sm:max-w-6xl'>
        <DialogHeader className='border-border shrink-0 border-b px-5 py-4 pr-14 sm:px-6'>
          <DialogTitle>{t('编辑渠道')}</DialogTitle>
          <DialogDescription>
            {t(
              'Provider、来源、连接凭据或模型变化后，旧结果会失效；倍率、并发、QPS 等服务参数不会影响结果。'
            )}
          </DialogDescription>
        </DialogHeader>
        <div className='min-h-0 flex-1 overflow-y-auto'>
          <ChannelEditorForm
            channel={props.channel}
            admin={props.admin}
            onSaved={() => props.onOpenChange(false)}
          />
        </div>
      </DialogContent>
    </Dialog>
  )
}
