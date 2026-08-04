import { useCallback, useEffect, useRef } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { Trophy } from 'lucide-react'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { Button } from '@/components/ui/button'
import {
  getDailyLuckyRewardNotifications,
  markAllDailyLuckyRewardNotificationsRead,
} from '../api'
import { formatLuckyUsd } from '../lib'

const luckyRewardNotificationQueryKey = [
  'daily-lucky-number',
  'reward-notifications',
] as const

export function LuckyRewardNotifier() {
  const userID = useAuthStore((state) => state.auth.user?.id)
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const shownNotificationIDs = useRef(new Set<number>())
  const notifications = useQuery({
    queryKey: luckyRewardNotificationQueryKey,
    queryFn: async () => {
      const response = await getDailyLuckyRewardNotifications()
      if (!response.success || !response.data) {
        throw new Error(response.message || 'Unable to load lucky rewards.')
      }
      return response.data
    },
    enabled: Boolean(userID),
    staleTime: 5 * 60 * 1000,
    refetchInterval: 5 * 60 * 1000,
    retry: false,
  })
  const { mutate: markAllRead, isPending: markingAllRead } = useMutation({
    mutationFn: markAllDailyLuckyRewardNotificationsRead,
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: luckyRewardNotificationQueryKey,
      })
    },
  })
  const unread = notifications.data?.items.find((item) => item.read_at === 0)
  const unreadCount = notifications.data?.unread_count ?? 0

  const openResults = useCallback(() => {
    markAllRead()
    void navigate({ to: '/daily-lucky-number' })
  }, [markAllRead, navigate])

  useEffect(() => {
    if (!unread || shownNotificationIDs.current.has(unread.id)) return
    shownNotificationIDs.current.add(unread.id)
    const matchedDigits = unread.reward.reward.matched_digits
    toast.success('每日幸运号中奖到账', {
      description: `恭喜命中 ${matchedDigits} 位，${formatLuckyUsd(unread.reward.reward_usd)} 已到账钱包余额。`,
      duration: 12_000,
      action: {
        label: '查看结果',
        onClick: openResults,
      },
    })
  }, [unread, openResults])

  if (!userID || unreadCount <= 0) return null

  return (
    <Button
      variant='outline'
      size='icon'
      className='relative'
      onClick={openResults}
      disabled={markingAllRead}
      aria-label={`有 ${unreadCount} 条每日幸运号中奖通知`}
      title='查看每日幸运号奖励'
    >
      <Trophy className='text-primary' aria-hidden='true' />
      <span className='bg-primary text-primary-foreground absolute -top-1 -right-1 flex min-w-4 items-center justify-center rounded-full px-1 text-[10px] leading-4 font-semibold'>
        {unreadCount > 99 ? '99+' : unreadCount}
      </span>
    </Button>
  )
}
