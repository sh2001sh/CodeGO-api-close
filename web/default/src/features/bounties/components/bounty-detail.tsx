import { Link } from '@tanstack/react-router'
import { ArrowLeft, ExternalLink, LockKeyhole, UserRound } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { IconGithub } from '@/assets/brand-icons'
import { Button } from '@/components/ui/button'
import { Markdown } from '@/components/ui/markdown'
import { Skeleton } from '@/components/ui/skeleton'
import { CopyButton } from '@/components/copy-button'
import { useBountyDetail } from '../hooks/use-bounty-detail'
import {
  formatBountyAmount,
  formatBountyDate,
  formatBountyRelativeTime,
  taskStatusLabel,
  taskStatusTone,
  taskTypeLabel,
  walletLabel,
} from '../lib/bounty-format'
import { BountyActionPanel } from './bounty-action-panel'
import { BountyDisputePanel } from './bounty-dispute-panel'
import { BountyDisputeRecords } from './bounty-dispute-records'
import { BountyReportPanel } from './bounty-report-panel'
import { BountyReviewPanel } from './bounty-review-panel'
import { BountySubmissionCard } from './bounty-submission-card'
import { BountySubmissionForm } from './bounty-submission-form'
import { BountyTimeline } from './bounty-timeline'
import { MaterialRequestPanel } from './material-request-panel'

interface BountyDetailProps {
  taskId: string
}

export function BountyDetail(props: BountyDetailProps) {
  const { t } = useTranslation()
  const query = useBountyDetail(props.taskId)
  if (query.isLoading) return <BountyDetailSkeleton />
  if (query.error || !query.data)
    return (
      <div
        className='border-destructive/30 bg-destructive/5 text-destructive mx-auto max-w-5xl rounded-xl border p-5'
        role='alert'
      >
        {t('Unable to load this bounty task.')}
      </div>
    )
  const detail = query.data
  const task = detail.task
  return (
    <div className='mx-auto flex w-full max-w-[1440px] flex-col gap-4'>
      <div>
        <Button variant='ghost' size='sm' render={<Link to='/bounties' />}>
          <ArrowLeft aria-hidden='true' />
          {t('Back to bounty market')}
        </Button>
      </div>
      <header className='border-border/70 bg-card/65 rounded-xl border p-4 sm:p-6'>
        <div className='flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between'>
          <div className='min-w-0'>
            <div className='mb-3 flex flex-wrap items-center gap-2'>
              <span
                className={`bounty-status bounty-status-${taskStatusTone(task.status)}`}
              >
                {taskStatusLabel(task.status, t)}
              </span>
              <span className='text-muted-foreground text-xs'>
                {taskTypeLabel(task.task_type, t)}
              </span>
              {task.tags.map((tag) => (
                <span
                  key={tag}
                  className='bg-muted rounded-md px-1.5 py-0.5 text-xs'
                >
                  {tag}
                </span>
              ))}
            </div>
            <h1 className='text-xl font-semibold tracking-tight text-balance sm:text-2xl'>
              {task.title}
            </h1>
            <div className='text-muted-foreground mt-3 flex flex-wrap items-center gap-x-4 gap-y-2 text-sm'>
              <span className='inline-flex items-center gap-1.5'>
                <UserRound className='size-3.5' aria-hidden='true' />
                {task.publisher.display_name}
              </span>
              <span>
                {t('Published {{date}}', {
                  date: formatBountyDate(task.created_at),
                })}
              </span>
            </div>
          </div>
          <div className='grid grid-cols-2 gap-3 sm:min-w-64'>
            <Metric
              label={t('Reward')}
              value={formatBountyAmount(task.reward_amount)}
              hint={walletLabel(task.reward_wallet_type, t)}
              emphasis
            />
            <Metric
              label={t('Delivery deadline')}
              value={formatBountyDate(task.deadline_at)}
              hint={formatBountyRelativeTime(task.deadline_at, t)}
            />
          </div>
        </div>
      </header>
      <div className='grid items-start gap-4 md:grid-cols-[minmax(0,1fr)_288px]'>
        <div className='order-last min-w-0 space-y-4 md:order-none'>
          <section className='border-border/70 bg-card/50 space-y-5 rounded-xl border p-4 sm:p-5'>
            <div className='flex items-center justify-between gap-3'>
              <h2 className='text-base font-semibold'>
                {t('Task description')}
              </h2>
              <a
                href={task.repo_url}
                target='_blank'
                rel='noreferrer'
                className='text-primary inline-flex min-h-11 items-center gap-1.5 text-sm underline-offset-4 hover:underline'
              >
                <IconGithub className='size-4' aria-hidden='true' />
                {t('Open GitHub')}
                <ExternalLink className='size-3' aria-hidden='true' />
              </a>
            </div>
            <Markdown allowHtml={false}>{task.description}</Markdown>
          </section>
          <MaterialRequestPanel
            task={task}
            requests={detail.material_requests}
          />
          {detail.submissions.length ? (
            <section className='border-border/70 bg-card/50 space-y-4 rounded-xl border p-4 sm:p-5'>
              <h2 className='text-base font-semibold'>
                {t('Delivery records')}
              </h2>
              {detail.submissions.map((submission) => (
                <BountySubmissionCard
                  key={submission.submission_id}
                  submission={submission}
                />
              ))}
            </section>
          ) : null}
          {task.can_submit ? <BountySubmissionForm task={task} /> : null}
          {task.can_manage &&
          (task.status === 'reviewing' || task.status === 'submitted') ? (
            <BountyReviewPanel task={task} />
          ) : null}
          {task.can_dispute ? <BountyDisputePanel task={task} /> : null}
          {detail.disputes.length ? (
            <BountyDisputeRecords disputes={detail.disputes} />
          ) : null}
          <BountyReportPanel task={task} />
          <section className='border-border/70 bg-card/50 space-y-4 rounded-xl border p-4 sm:p-5'>
            <h2 className='text-base font-semibold'>{t('Timeline')}</h2>
            <BountyTimeline events={detail.timeline} />
          </section>
        </div>
        <aside className='order-first space-y-4 md:sticky md:top-5 md:order-none'>
          <BountyActionPanel detail={detail} />
          <section className='border-border/70 bg-card/50 space-y-4 rounded-xl border p-4'>
            <div className='flex items-center gap-2 text-sm font-semibold'>
              <LockKeyhole className='text-warning size-4' aria-hidden='true' />
              {t('Reward protection')}
            </div>
            <p className='text-muted-foreground text-sm leading-6'>
              {t(
                'The reward is frozen at publish time. It is only settled through acceptance, admin resolution, or released when the task is cancelled or expires.'
              )}
            </p>
            <div className='border-border/60 border-t pt-3 text-xs leading-5'>
              {t(
                'Private repositories are supported as links, but platform access is not automatically checked in this version.'
              )}
            </div>
            {task.reservation_id ? (
              <div className='border-border/60 space-y-1 border-t pt-3'>
                <div className='text-muted-foreground text-xs'>
                  {t('Ledger reservation')}
                </div>
                <div className='bg-muted flex min-h-9 items-center justify-between gap-2 rounded-lg px-2.5'>
                  <code className='min-w-0 truncate font-mono text-[11px]'>
                    {task.reservation_id}
                  </code>
                  <CopyButton
                    value={task.reservation_id}
                    size='icon'
                    className='size-8'
                    tooltip={t('Copy reservation ID')}
                  />
                </div>
              </div>
            ) : null}
          </section>
        </aside>
      </div>
    </div>
  )
}

function Metric(props: {
  label: string
  value: string
  hint: string
  emphasis?: boolean
}) {
  return (
    <div className='border-border/60 bg-background/45 rounded-lg border p-3'>
      <div className='text-muted-foreground text-xs'>{props.label}</div>
      <div
        className={`${props.emphasis ? 'text-primary' : ''}mt-1 truncate font-mono text-sm font-semibold tabular-nums`}
      >
        {props.value}
      </div>
      <div className='text-muted-foreground mt-1 truncate text-xs'>
        {props.hint}
      </div>
    </div>
  )
}

function BountyDetailSkeleton() {
  return (
    <div className='mx-auto max-w-[1440px] space-y-4'>
      <Skeleton className='h-8 w-44' />
      <Skeleton className='h-40 w-full rounded-xl' />
      <div className='grid gap-4 md:grid-cols-[minmax(0,1fr)_288px]'>
        <Skeleton className='h-[600px] rounded-xl' />
        <Skeleton className='h-80 rounded-xl' />
      </div>
    </div>
  )
}
