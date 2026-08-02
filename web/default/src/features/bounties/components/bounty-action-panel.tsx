import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import { CalendarClock } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { useAssignBounty, useBountyAction } from '../hooks/use-bounty-actions'
import {
  formatBountyAmount,
  formatBountyDate,
  formatBountyRelativeTime,
  applicationStatusLabel,
  walletLabel,
} from '../lib/bounty-format'
import type { BountyDetail } from '../types'

export function BountyActionPanel(props: { detail: BountyDetail }) {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const task = props.detail.task
  const [applicationMessage, setApplicationMessage] = useState('')
  const action = useBountyAction(task.task_id, 'applications')
  const start = useBountyAction(task.task_id, 'start')
  const cancel = useBountyAction(task.task_id, 'cancel')
  const apply = () => {
    if (!applicationMessage.trim()) return
    action.mutate({ message: applicationMessage.trim() })
  }
  return (
    <section className='border-border/70 bg-card/70 space-y-4 rounded-xl border p-4 shadow-[0_2px_8px_rgba(24,32,43,0.04)] dark:shadow-[0_2px_8px_rgba(0,0,0,0.18)]'>
      <div className='flex items-center gap-2 text-sm font-semibold'>
        <CalendarClock className='text-info size-4' aria-hidden='true' />
        {t('Task action')}
      </div>
      <div className='border-border/60 grid grid-cols-2 gap-3 border-y py-3'>
        <Metric
          label={t('Reward')}
          value={formatBountyAmount(task.reward_amount)}
          hint={walletLabel(task.reward_wallet_type, t)}
        />
        <Metric
          label={t('Deadline')}
          value={formatBountyDate(task.deadline_at)}
          hint={formatBountyRelativeTime(task.deadline_at, t)}
        />
      </div>
      {!user ? (
        <Button
          className='w-full'
          render={
            <Link
              to='/sign-in'
              search={{ redirect: `/bounties/${task.task_id}` }}
            />
          }
        >
          {t('Sign in to apply')}
        </Button>
      ) : task.can_apply ? (
        <div className='space-y-3'>
          <Label htmlFor='bounty-application' className='text-sm'>
            {t('Why are you a good fit?')}
          </Label>
          <Textarea
            id='bounty-application'
            value={applicationMessage}
            onChange={(event) => setApplicationMessage(event.target.value)}
            placeholder={t(
              'Share your approach and expected delivery briefly.'
            )}
            className='min-h-24 resize-y'
          />
          <Button
            className='w-full'
            onClick={apply}
            disabled={!applicationMessage.trim() || action.isPending}
          >
            {action.isPending ? t('Applying…') : t('Apply to take this task')}
          </Button>
        </div>
      ) : props.detail.my_application ? (
        <ApplicationStatus status={props.detail.my_application.status} />
      ) : null}
      {task.can_manage ? (
        <PublisherActions detail={props.detail} cancel={cancel} />
      ) : null}
      {task.can_start ? (
        <Button
          className='w-full'
          onClick={() => start.mutate({})}
          disabled={start.isPending}
        >
          {t('Start development')}
        </Button>
      ) : null}
      {task.can_submit ? (
        <div className='bounty-status bounty-status-info w-full justify-center'>
          {t('You can submit delivery below.')}
        </div>
      ) : null}
      {!task.can_manage &&
      !task.can_start &&
      !task.can_submit &&
      (task.status === 'submitted' || task.status === 'reviewing') ? (
        <div className='bounty-status bounty-status-info w-full justify-center'>
          {t('Waiting for publisher review')}
        </div>
      ) : null}
      {task.status === 'completed' ? (
        <div className='bounty-status bounty-status-success w-full justify-center'>
          {t('Reward settled')}
        </div>
      ) : null}
      {task.status === 'disputed' ? (
        <div className='bounty-status bounty-status-danger w-full justify-center'>
          {t('Awaiting admin resolution')}
        </div>
      ) : null}
    </section>
  )
}

function ApplicationStatus(props: { status: string }) {
  const { t } = useTranslation()
  return (
    <div className='bounty-status bounty-status-info w-full justify-center'>
      {applicationStatusLabel(props.status, t)}
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

function PublisherActions(props: {
  detail: BountyDetail
  cancel: ReturnType<typeof useBountyAction>
}) {
  const { t } = useTranslation()
  const assign = useAssignBounty(props.detail.task.task_id)
  const [selected, setSelected] = useState('')
  const [cancelOpen, setCancelOpen] = useState(false)
  const pending = props.detail.applications.filter(
    (item) => item.status === 'pending'
  )
  return (
    <div className='space-y-3'>
      <div className='text-muted-foreground text-xs'>
        {t('Publisher controls')}
      </div>
      {pending.length ? (
        <div className='space-y-2'>
          {pending.map((application) => (
            <div
              key={application.application_id}
              className='border-border/60 rounded-lg border p-3'
            >
              <div className='flex items-center justify-between gap-2 text-sm font-medium'>
                <span>{application.applicant.display_name}</span>
                <span className='text-muted-foreground text-xs'>
                  {formatBountyDate(application.created_at)}
                </span>
              </div>
              <p className='mt-2 text-sm leading-6'>{application.message}</p>
              <Button
                size='sm'
                className='mt-3 w-full'
                onClick={() => {
                  setSelected(application.application_id)
                  assign.mutate(application.application_id)
                }}
                disabled={
                  assign.isPending && selected === application.application_id
                }
              >
                {t('Confirm this executor')}
              </Button>
            </div>
          ))}
        </div>
      ) : null}
      {['published', 'selecting', 'assigned'].includes(
        props.detail.task.status
      ) ? (
        <Button
          variant='outline'
          className='w-full'
          onClick={() => setCancelOpen(true)}
          disabled={props.cancel.isPending}
        >
          {t('Cancel task and release quota')}
        </Button>
      ) : null}
      <ConfirmDialog
        open={cancelOpen}
        onOpenChange={setCancelOpen}
        title={t('Cancel this bounty task?')}
        desc={t(
          'The frozen reward quota will be released. This action cannot be undone.'
        )}
        confirmText={t('Cancel task')}
        destructive
        isLoading={props.cancel.isPending}
        handleConfirm={() =>
          props.cancel.mutate({}, { onSuccess: () => setCancelOpen(false) })
        }
      />
    </div>
  )
}
