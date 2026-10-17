import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import type { MarketplaceChannel } from '../types'
import { ChannelEditorForm } from './channel-create-form'
import { ChannelPresentationForm } from './channel-presentation-form'

export function ChannelEditDialog(props: {
  channel: MarketplaceChannel | null
  open: boolean
  admin?: boolean
  presentationOnly?: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  if (!props.channel) return null
  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent
        aria-describedby={undefined}
        className={cn(
          'admin-editor marketplace-channel-editor flex max-h-[calc(100dvh-1rem)] flex-col gap-0 overflow-hidden p-0',
          props.presentationOnly
            ? 'max-w-2xl sm:max-w-2xl'
            : 'h-[min(92dvh,900px)] max-w-6xl sm:max-w-6xl'
        )}
      >
        <DialogHeader className='border-border shrink-0 border-b px-5 py-4 pr-14 sm:px-6'>
          <DialogTitle>
            {props.presentationOnly ? t('商品展示') : t('编辑渠道')}
          </DialogTitle>
        </DialogHeader>
        <div className='min-h-0 flex-1 overflow-y-auto'>
          {props.presentationOnly ? (
            <ChannelPresentationForm
              key={props.channel.id}
              channel={props.channel}
              onSaved={() => props.onOpenChange(false)}
            />
          ) : (
            <ChannelEditorForm
              channel={props.channel}
              admin={props.admin}
              onSaved={() => props.onOpenChange(false)}
            />
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
