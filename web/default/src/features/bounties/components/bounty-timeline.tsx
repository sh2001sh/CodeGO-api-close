import {
  CheckCircle2,
  CircleDot,
  Clock3,
  GitPullRequest,
  MessageSquare,
  ShieldAlert,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatBountyDate } from '../lib/bounty-format'
import type { BountyEvent } from '../types'

const EVENT_ICONS: Record<string, typeof CircleDot> = {
  task_published: CheckCircle2,
  application_submitted: CircleDot,
  application_accepted: CheckCircle2,
  material_requested: MessageSquare,
  material_replied: MessageSquare,
  submission_created: GitPullRequest,
  dispute_opened: ShieldAlert,
  dispute_resolved: CheckCircle2,
}

function eventLabel(type: string, t: ReturnType<typeof useTranslation>['t']) {
  const labels: Record<string, string> = {
    task_published: 'Task published',
    task_reward_hold: 'Reward quota frozen',
    application_submitted: 'Application submitted',
    application_accepted: 'Executor confirmed',
    application_rejected: 'Application not selected',
    task_started: 'Development started',
    task_draft_saved: 'Draft saved',
    material_requested: 'Material requested',
    material_replied: 'Publisher replied',
    material_resolved: 'Material request resolved',
    material_timeout: 'Material request timed out',
    material_timeout_action: 'Material timeout handled',
    submission_created: 'Delivery submitted',
    review_started: 'Review started',
    review_deadline_soon: 'Review deadline approaching',
    changes_requested: 'Changes requested',
    review_approved: 'Delivery accepted',
    task_completed: 'Task completed',
    task_reward_paid: 'Reward quota paid',
    task_reward_release: 'Reward quota released',
    task_cancelled: 'Task cancelled',
    task_expired: 'Task expired',
    dispute_opened: 'Dispute opened',
    dispute_resolved: 'Dispute resolved',
    task_suspended: 'Task suspended',
    task_resumed: 'Task resumed',
  }
  return labels[type] ? t(labels[type]) : t('Unknown timeline event')
}

export function BountyTimeline(props: { events: BountyEvent[] }) {
  const { t } = useTranslation()
  if (!props.events.length)
    return (
      <p className='text-muted-foreground text-sm'>
        {t('No timeline events yet.')}
      </p>
    )
  return (
    <ol className='space-y-0'>
      {props.events.map((event, index) => {
        const Icon = EVENT_ICONS[event.event_type] ?? Clock3
        return (
          <li
            key={event.event_id}
            className='relative flex gap-3 pb-5 last:pb-0'
          >
            {index < props.events.length - 1 ? (
              <span
                className='bg-border absolute top-7 left-[9px] h-[calc(100%-16px)] w-px'
                aria-hidden='true'
              />
            ) : null}
            <span className='bg-muted text-muted-foreground relative z-10 flex size-5 shrink-0 items-center justify-center rounded-full'>
              <Icon className='size-3' aria-hidden='true' />
            </span>
            <div className='min-w-0 flex-1'>
              <div className='text-sm font-medium'>
                {eventLabel(event.event_type, t)}
              </div>
              <div className='text-muted-foreground mt-1 text-xs'>
                {event.actor?.display_name ?? t('System')} ·{' '}
                {formatBountyDate(event.created_at)}
              </div>
            </div>
          </li>
        )
      })}
    </ol>
  )
}
