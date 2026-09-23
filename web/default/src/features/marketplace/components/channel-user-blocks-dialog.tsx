import { useState } from 'react'
import dayjs from 'dayjs'
import { Loader2, ShieldBan, ShieldCheck, UserX } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import {
  useMarketplaceChannelUserBlocks,
  useMarketplaceMutations,
} from '../hooks'
import type { MarketplaceChannel } from '../types'

export function ChannelUserBlocksDialog(props: {
  channel: MarketplaceChannel
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [page, setPage] = useState(1)
  const [identifier, setIdentifier] = useState('')
  const [unblockingUserID, setUnblockingUserID] = useState<number | null>(null)
  const blocks = useMarketplaceChannelUserBlocks(props.channel.id, page, open)
  const mutations = useMarketplaceMutations()

  const blockUser = () => {
    const normalized = identifier.trim()
    if (!normalized) return
    const numericUserId = /^\d+$/.test(normalized)
      ? Number(normalized)
      : undefined
    mutations.userBlock.mutate(
      {
        channelId: props.channel.id,
        userId: numericUserId,
        userExternalId: numericUserId ? undefined : normalized,
        blocked: true,
      },
      {
        onSuccess: () => {
          setIdentifier('')
          toast.success(t('用户已被拉黑'))
        },
        onError: (error) =>
          toast.error(error instanceof Error ? error.message : t('拉黑失败')),
      }
    )
  }

  const unblockUser = (userID: number) => {
    if (!window.confirm(t('确认解除该用户在当前渠道的拉黑状态？'))) return
    setUnblockingUserID(userID)
    mutations.userBlock.mutate(
      { channelId: props.channel.id, userId: userID, blocked: false },
      {
        onSuccess: () => toast.success(t('已解除用户拉黑')),
        onError: (error) =>
          toast.error(error instanceof Error ? error.message : t('操作失败')),
        onSettled: () => setUnblockingUserID(null),
      }
    )
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        setOpen(nextOpen)
        if (!nextOpen) setPage(1)
      }}
    >
      <DialogTrigger render={<Button variant='outline' size='sm' />}>
        <ShieldBan />
        {t('黑名单管理')}
      </DialogTrigger>
      <DialogContent className='flex max-h-[min(86dvh,720px)] max-w-lg grid-rows-[auto_auto_minmax(0,1fr)] flex-col gap-0 overflow-hidden p-0 sm:max-w-lg'>
        <DialogHeader className='border-b px-4 py-4 pr-12'>
          <DialogTitle>{t('渠道用户黑名单')}</DialogTitle>
          <DialogDescription>
            {t('被拉黑的用户无法使用当前渠道，不影响其他渠道。')}
          </DialogDescription>
        </DialogHeader>

        <div className='bg-muted/30 flex flex-col gap-2 border-b p-4 sm:flex-row'>
          <Input
            value={identifier}
            onChange={(event) => setIdentifier(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter') blockUser()
            }}
            placeholder={t('用户编号或数字 ID')}
            aria-label={t('用户编号或数字 ID')}
          />
          <Button
            variant='destructive'
            onClick={blockUser}
            disabled={!identifier.trim() || mutations.userBlock.isPending}
          >
            {mutations.userBlock.isPending && unblockingUserID == null ? (
              <Loader2 className='animate-spin' />
            ) : (
              <UserX />
            )}
            {t('拉黑用户')}
          </Button>
        </div>

        <div className='min-h-0 overflow-y-auto p-4'>
          {blocks.isLoading ? (
            <div className='space-y-2' aria-label={t('正在加载黑名单')}>
              {[0, 1, 2].map((item) => (
                <Skeleton key={item} className='h-16 w-full' />
              ))}
            </div>
          ) : blocks.isError ? (
            <div className='flex min-h-40 flex-col items-center justify-center gap-3 text-center'>
              <p className='text-destructive text-sm'>
                {blocks.error instanceof Error
                  ? blocks.error.message
                  : t('黑名单加载失败')}
              </p>
              <Button
                variant='outline'
                size='sm'
                onClick={() => blocks.refetch()}
              >
                {t('重新加载')}
              </Button>
            </div>
          ) : blocks.data?.items.length ? (
            <>
              <div className='divide-y'>
                {blocks.data.items.map((item) => (
                  <div
                    key={item.user_id}
                    className='flex min-h-16 items-center justify-between gap-3 py-3'
                  >
                    <div className='min-w-0'>
                      <div className='flex flex-wrap items-baseline gap-x-2'>
                        <span className='truncate font-medium'>
                          {item.display_name || item.username || t('未知用户')}
                        </span>
                        <span className='text-muted-foreground font-mono text-xs'>
                          {item.user_external_id || `#${item.user_id}`}
                        </span>
                      </div>
                      <p className='text-muted-foreground mt-1 text-xs'>
                        {t('拉黑时间')}：
                        {dayjs(item.blocked_at).format('YYYY-MM-DD HH:mm')}
                      </p>
                    </div>
                    <Button
                      variant='outline'
                      size='sm'
                      className='shrink-0'
                      disabled={unblockingUserID === item.user_id}
                      onClick={() => unblockUser(item.user_id)}
                    >
                      {unblockingUserID === item.user_id ? (
                        <Loader2 className='animate-spin' />
                      ) : (
                        <ShieldCheck />
                      )}
                      {t('解除拉黑')}
                    </Button>
                  </div>
                ))}
              </div>
              {blocks.data.total > blocks.data.page_size && (
                <div className='mt-3 flex items-center justify-between border-t pt-3'>
                  <span className='text-muted-foreground text-xs'>
                    {t('共 {{count}} 人', { count: blocks.data.total })}
                  </span>
                  <div className='flex gap-2'>
                    <Button
                      variant='outline'
                      size='sm'
                      disabled={page <= 1 || blocks.isFetching}
                      onClick={() => setPage((current) => current - 1)}
                    >
                      {t('上一页')}
                    </Button>
                    <Button
                      variant='outline'
                      size='sm'
                      disabled={
                        page * blocks.data.page_size >= blocks.data.total ||
                        blocks.isFetching
                      }
                      onClick={() => setPage((current) => current + 1)}
                    >
                      {t('下一页')}
                    </Button>
                  </div>
                </div>
              )}
            </>
          ) : (
            <div className='text-muted-foreground flex min-h-40 flex-col items-center justify-center gap-2 text-center'>
              <ShieldCheck className='size-8' />
              <p className='text-sm'>{t('当前渠道没有拉黑用户')}</p>
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
