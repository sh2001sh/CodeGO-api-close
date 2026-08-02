import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Bell, CheckCheck, ExternalLink } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { Button } from '@/components/ui/button'
import {
  Popover,
  PopoverContent,
  PopoverDescription,
  PopoverHeader,
  PopoverTitle,
  PopoverTrigger,
} from '@/components/ui/popover'
import {
  markAllBountyNotificationsRead,
  markBountyNotificationRead,
} from '../api'
import { useBountyNotifications } from '../hooks/use-bounty-list'
import { formatBountyDate } from '../lib/bounty-format'
import type { BountyNotification } from '../types'

const NOTIFICATION_COPY: Record<string, { title: string; content: string }> = {
  task_published: {
    title: 'Task published',
    content: 'The reward is frozen and the task is waiting for applications.',
  },
  application_submitted: {
    title: 'New application received',
    content: 'Someone applied to take your task. Review their application.',
  },
  application_accepted: {
    title: 'Application accepted',
    content:
      'The publisher confirmed you as the executor. You can start development.',
  },
  application_rejected: {
    title: 'Application not selected',
    content: 'The publisher selected another executor. Thank you for applying.',
  },
  task_started: {
    title: 'Development started',
    content: 'The task has entered development.',
  },
  material_requested: {
    title: 'Material requested',
    content: 'The executor asked for additional material or a decision.',
  },
  material_replied: {
    title: 'Material request replied',
    content: 'A material request now has a reply.',
  },
  material_resolved: {
    title: 'Material request resolved',
    content: 'The material request is resolved and the task can continue.',
  },
  material_timeout: {
    title: 'Material request timed out',
    content:
      'No reply was received within 48 hours. Choose an available timeout action.',
  },
  material_timeout_extension: {
    title: 'Material reply window extended',
    content: 'The material reply window was extended.',
  },
  submission_created: {
    title: 'New delivery received',
    content: 'A GitHub delivery was submitted. Review it within 72 hours.',
  },
  changes_requested: {
    title: 'Changes requested',
    content: 'The delivery needs more changes before settlement.',
  },
  review_deadline_soon: {
    title: 'Review deadline approaching',
    content: 'The 72-hour review window will end soon.',
  },
  task_auto_settled: {
    title: 'Task settled automatically',
    content:
      'The review window ended and the reward was settled automatically.',
  },
  task_reward_paid: {
    title: 'Reward settled',
    content:
      'The reward was recorded in the ledger according to the review result.',
  },
  task_cancelled: {
    title: 'Task cancelled',
    content: 'The task was cancelled and the frozen reward was released.',
  },
  task_expired: {
    title: 'Task expired',
    content: 'The deadline passed and the frozen reward was released.',
  },
  dispute_opened: {
    title: 'Dispute opened',
    content: 'The task entered platform review and the reward remains frozen.',
  },
  dispute_resolved: {
    title: 'Dispute resolved',
    content: 'An administrator recorded a final dispute resolution.',
  },
  task_suspended: {
    title: 'Task suspended',
    content: 'An administrator paused the task and participant actions.',
  },
  task_resumed: {
    title: 'Task resumed',
    content: 'An administrator resumed the task.',
  },
  task_reported: {
    title: 'Task reported',
    content: 'A user reported this task. Review it in the admin area.',
  },
  report_resolved: {
    title: 'Report resolved',
    content: 'An administrator handled your task report.',
  },
}

function notificationCopy(
  notification: BountyNotification,
  t: ReturnType<typeof useTranslation>['t']
) {
  const copy = NOTIFICATION_COPY[notification.type]
  if (!copy) {
    return {
      title: t('Bounty notification'),
      content: t('Open the task to review the latest update.'),
    }
  }
  const preservesUserContent = [
    'changes_requested',
    'material_requested',
    'material_replied',
  ].includes(notification.type)
  return {
    title: t(copy.title),
    content:
      preservesUserContent && notification.content.trim()
        ? t('Message: {{content}}', { content: notification.content })
        : t(copy.content),
  }
}

export function BountyNotificationPanel() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const user = useAuthStore((state) => state.auth.user)
  const notifications = useBountyNotifications(Boolean(user))
  const markRead = useMutation({
    mutationFn: markBountyNotificationRead,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['bounty-notifications'] })
    },
    onError: () => toast.error(t('Unable to update notifications')),
  })
  const markAllRead = useMutation({
    mutationFn: markAllBountyNotificationsRead,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['bounty-notifications'] })
    },
    onError: () => toast.error(t('Unable to update notifications')),
  })
  const unreadCount = notifications.data?.unread_count ?? 0

  return (
    <Popover>
      <PopoverTrigger
        render={
          <Button
            variant='outline'
            size='icon'
            className='relative'
            aria-label={t('Bounty notifications')}
          />
        }
      >
        <Bell aria-hidden='true' />
        {unreadCount > 0 ? (
          <span className='bg-destructive text-destructive-foreground absolute -top-1 -right-1 flex min-w-4 items-center justify-center rounded-full px-1 text-[10px] leading-4 font-semibold'>
            {unreadCount > 99 ? '99+' : unreadCount}
          </span>
        ) : null}
      </PopoverTrigger>
      <PopoverContent
        align='end'
        className='w-[min(24rem,calc(100vw-2rem))] p-0'
      >
        <PopoverHeader className='border-border/60 border-b px-4 py-3'>
          <div className='flex items-start justify-between gap-3'>
            <div>
              <PopoverTitle>{t('Bounty notifications')}</PopoverTitle>
              <PopoverDescription className='mt-1'>
                {t('Updates about applications, delivery, and settlement.')}
              </PopoverDescription>
            </div>
            {unreadCount > 0 ? (
              <Button
                variant='ghost'
                size='sm'
                onClick={() => markAllRead.mutate()}
                disabled={markAllRead.isPending}
              >
                <CheckCheck aria-hidden='true' />
                {t('Mark all read')}
              </Button>
            ) : null}
          </div>
        </PopoverHeader>
        <div className='max-h-[min(28rem,60vh)] overflow-y-auto p-2'>
          {notifications.isLoading ? (
            <div className='text-muted-foreground px-2 py-8 text-center text-sm'>
              {t('Loading notifications…')}
            </div>
          ) : notifications.error ? (
            <div className='text-destructive px-2 py-8 text-center text-sm'>
              {t('Unable to load notifications')}
            </div>
          ) : notifications.data?.items.length ? (
            notifications.data.items.map((notification) => {
              const copy = notificationCopy(notification, t)
              return (
                <Link
                  key={notification.notification_id}
                  to='/bounties/$taskId'
                  params={{ taskId: notification.task_id }}
                  onClick={() => {
                    if (!notification.read_at) {
                      markRead.mutate(notification.notification_id)
                    }
                  }}
                  className={`group hover:bg-muted flex gap-3 rounded-lg p-3 transition-colors ${notification.read_at ? '' : 'bg-primary/5'}`}
                >
                  <span
                    className={`mt-1 size-2 shrink-0 rounded-full ${notification.read_at ? 'bg-border' : 'bg-primary'}`}
                    aria-hidden='true'
                  />
                  <span className='min-w-0 flex-1'>
                    <span className='flex items-start justify-between gap-2'>
                      <span className='text-sm font-medium'>{copy.title}</span>
                      <ExternalLink
                        className='text-muted-foreground mt-0.5 size-3.5 shrink-0 opacity-0 transition-opacity group-hover:opacity-100'
                        aria-hidden='true'
                      />
                    </span>
                    <span className='text-muted-foreground mt-1 block text-xs leading-5'>
                      {copy.content}
                    </span>
                    <span className='text-muted-foreground mt-1 block text-[11px]'>
                      {formatBountyDate(notification.created_at)}
                    </span>
                  </span>
                </Link>
              )
            })
          ) : (
            <div className='text-muted-foreground px-2 py-8 text-center text-sm'>
              {t('No bounty notifications yet.')}
            </div>
          )}
        </div>
      </PopoverContent>
    </Popover>
  )
}
