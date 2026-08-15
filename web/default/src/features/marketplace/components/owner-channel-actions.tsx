import { Loader2, Pause, Pencil, Play, RefreshCcw, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { useMarketplaceMutations } from '../hooks'
import type { MarketplaceChannel } from '../types'

export function OwnerChannelActions(props: {
  channel: MarketplaceChannel
  onEdit: () => void
  onDelete: () => void
}) {
  const { t } = useTranslation()
  const mutations = useMarketplaceMutations()
  const channel = props.channel

  const act = async (action: 'verify' | 'pause' | 'resume') => {
    try {
      if (action === 'verify') {
        await mutations.verify.mutateAsync(channel.id)
        toast.info(t('检测已开始，页面会自动更新进度和结果'))
        return
      }
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
    <div className='flex shrink-0 flex-wrap items-center gap-2 lg:justify-end'>
      <Button variant='outline' size='sm' onClick={props.onEdit}>
        <Pencil />
        {t('编辑')}
      </Button>
      <Button
        variant='outline'
        size='sm'
        onClick={() => void act('verify')}
        disabled={
          mutations.verify.isPending ||
          channel.lifecycle_status === 'verifying' ||
          ['queued', 'running'].includes(channel.verification_status)
        }
      >
        <RefreshCcw
          className={cn(
            channel.lifecycle_status === 'verifying' && 'animate-spin'
          )}
        />
        {channel.lifecycle_status === 'verifying' ? t('检测中') : t('重新检测')}
      </Button>
      {channel.lifecycle_status === 'suspended' ? (
        <Button variant='outline' size='sm' onClick={() => void act('resume')}>
          <Play />
          {t('恢复')}
        </Button>
      ) : (
        <Button
          variant='outline'
          size='sm'
          onClick={() => void act('pause')}
          disabled={
            channel.lifecycle_status !== 'active' &&
            channel.lifecycle_status !== 'degraded'
          }
        >
          <Pause />
          {t('暂停')}
        </Button>
      )}
      <Button
        variant='ghost'
        size='sm'
        className='text-destructive hover:text-destructive'
        onClick={props.onDelete}
      >
        <Trash2 />
        {t('删除')}
      </Button>
      {(mutations.pause.isPending || mutations.verify.isPending) && (
        <Loader2 className='text-muted-foreground size-4 animate-spin' />
      )}
    </div>
  )
}
