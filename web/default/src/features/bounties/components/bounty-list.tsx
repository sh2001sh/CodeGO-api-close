import { Link } from '@tanstack/react-router'
import {
  ArrowUpRight,
  CalendarClock,
  CircleAlert,
  Plus,
  UserRound,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { IconGithub } from '@/assets/brand-icons'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import {
  formatBountyAmount,
  formatBountyDate,
  formatBountyRelativeTime,
  taskStatusLabel,
  taskStatusTone,
  walletLabel,
} from '../lib/bounty-format'
import type { BountyListResponse, BountyTask } from '../types'

interface BountyListProps {
  result?: BountyListResponse
  loading: boolean
  error: Error | null
  onClearFilters: () => void
  onPublish: () => void
  onEdit?: (task: BountyTask) => void
}

function StatusBadge(props: { task: BountyTask }) {
  const { t } = useTranslation()
  const tone = taskStatusTone(props.task.status)
  return (
    <span className={`bounty-status bounty-status-${tone}`}>
      {taskStatusLabel(props.task.status, t)}
    </span>
  )
}

export function BountyList(props: BountyListProps) {
  const { t } = useTranslation()
  if (props.loading) {
    return (
      <div className='border-border/70 overflow-hidden rounded-xl border'>
        {Array.from({ length: 6 }).map((_, index) => (
          <div
            key={index}
            className='border-border/60 flex min-h-24 items-center gap-4 border-b px-4 py-4 last:border-b-0'
          >
            <Skeleton className='h-5 w-1/3' />
            <Skeleton className='h-4 w-24' />
            <Skeleton className='h-4 w-28' />
          </div>
        ))}
      </div>
    )
  }
  if (props.error) {
    return (
      <div
        className='border-destructive/30 bg-destructive/5 text-destructive flex items-start gap-3 rounded-xl border p-4 text-sm'
        role='alert'
      >
        <CircleAlert className='mt-0.5 size-4 shrink-0' aria-hidden='true' />
        <div>
          <div className='font-medium'>{t('Unable to load bounty tasks')}</div>
          <div className='mt-1'>
            {t('Refresh the page or try again in a moment.')}
          </div>
        </div>
      </div>
    )
  }
  if (!props.result?.items.length) {
    return (
      <Empty className='border-border/70 bg-card/40 min-h-64 border'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <IconGithub aria-hidden='true' />
          </EmptyMedia>
          <EmptyTitle>{t('No tasks match these filters')}</EmptyTitle>
          <EmptyDescription>
            {t(
              'Clear the filters or publish a coding task with a verifiable GitHub delivery.'
            )}
          </EmptyDescription>
        </EmptyHeader>
        <EmptyContent>
          <div className='flex flex-wrap justify-center gap-2'>
            <Button variant='outline' onClick={props.onClearFilters}>
              {t('Clear filters')}
            </Button>
            <Button onClick={props.onPublish}>
              <Plus aria-hidden='true' />
              {t('Publish a task')}
            </Button>
          </div>
        </EmptyContent>
      </Empty>
    )
  }
  return (
    <div className='border-border/70 bg-card/50 overflow-hidden rounded-xl border'>
      <div className='text-muted-foreground hidden grid-cols-[minmax(0,1fr)_160px_160px_130px_96px] gap-4 border-b px-4 py-3 text-xs font-medium md:grid'>
        <span>{t('Task')}</span>
        <span>{t('Reward')}</span>
        <span>{t('Deadline')}</span>
        <span>{t('Status')}</span>
        <span className='text-right'>{t('Action')}</span>
      </div>
      {props.result.items.map((task) => (
        <BountyRow key={task.task_id} task={task} onEdit={props.onEdit} />
      ))}
    </div>
  )
}

function BountyRow(props: {
  task: BountyTask
  onEdit?: (task: BountyTask) => void
}) {
  const { t } = useTranslation()
  return (
    <div className='border-border/60 grid gap-3 border-b px-4 py-4 last:border-b-0 md:grid-cols-[minmax(0,1fr)_160px_160px_130px_96px] md:items-center md:gap-4'>
      <div className='min-w-0'>
        <Link
          to='/bounties/$taskId'
          params={{ taskId: props.task.task_id }}
          className='group flex min-w-0 items-start gap-2'
        >
          <span className='group-hover:text-primary min-w-0 truncate text-sm font-semibold'>
            {props.task.title}
          </span>
          <ArrowUpRight
            className='text-muted-foreground mt-0.5 size-3.5 shrink-0'
            aria-hidden='true'
          />
        </Link>
        <div className='text-muted-foreground mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs'>
          <span className='inline-flex items-center gap-1'>
            <UserRound className='size-3' aria-hidden='true' />
            {props.task.publisher.display_name}
          </span>
          {props.task.tags.slice(0, 3).map((tag) => (
            <span key={tag} className='bg-muted rounded-md px-1.5 py-0.5'>
              {tag}
            </span>
          ))}
        </div>
      </div>
      <div>
        <div className='font-mono text-base font-semibold tabular-nums'>
          {formatBountyAmount(props.task.reward_amount)}
        </div>
        <div className='text-muted-foreground text-xs'>
          {walletLabel(props.task.reward_wallet_type, t)}
        </div>
      </div>
      <div>
        <div className='flex items-center gap-1.5 text-sm'>
          <CalendarClock
            className='text-muted-foreground size-3.5'
            aria-hidden='true'
          />
          {formatBountyDate(props.task.deadline_at)}
        </div>
        <div className='text-muted-foreground mt-1 text-xs'>
          {formatBountyRelativeTime(props.task.deadline_at, t)}
        </div>
      </div>
      <div>
        <StatusBadge task={props.task} />
      </div>
      <div className='flex justify-start gap-2 md:justify-end'>
        {props.task.status === 'draft' &&
        props.task.can_manage &&
        props.onEdit ? (
          <Button size='sm' onClick={() => props.onEdit?.(props.task)}>
            {t('Edit draft')}
          </Button>
        ) : null}
        <Button
          variant={props.task.can_apply ? 'default' : 'outline'}
          size='sm'
          render={
            <Link
              to='/bounties/$taskId'
              params={{ taskId: props.task.task_id }}
            />
          }
        >
          {props.task.can_apply ? t('View and apply') : t('View details')}
        </Button>
      </div>
    </div>
  )
}
