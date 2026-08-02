import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  CircleAlert,
  FileWarning,
  ShieldCheck,
  Pause,
  Play,
  Scale,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { api } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { SectionPageLayout } from '@/components/layout'
import { getAdminBountyReports, unwrap } from '../api'
import {
  useAdminBountyAction,
  useResolveAdminBountyDispute,
} from '../hooks/use-bounty-actions'
import {
  formatBountyAmount,
  formatBountyDate,
  bountyUsdToQuota,
  taskStatusLabel,
} from '../lib/bounty-format'
import type { BountyDispute, BountyListResponse, BountyReport } from '../types'

async function getAdminBounties() {
  const response = await api.get('/api/admin/bounties', {
    params: { page: 1, page_size: 50 },
  })
  return unwrap<BountyListResponse>(response)
}

async function getAdminDisputes() {
  const response = await api.get('/api/admin/bounties/disputes')
  return unwrap<BountyDispute[]>(response)
}

export function BountyAdminPage() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const tasks = useQuery({
    queryKey: ['admin-bounties'],
    queryFn: getAdminBounties,
  })
  const disputes = useQuery({
    queryKey: ['admin-bounty-disputes'],
    queryFn: getAdminDisputes,
  })
  const reports = useQuery({
    queryKey: ['admin-bounty-reports'],
    queryFn: getAdminBountyReports,
  })
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Bounty operations')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Description>
        {t(
          'Review task evidence, disputes, suspensions, and ledger-linked decisions.'
        )}
      </SectionPageLayout.Description>
      <p className='text-muted-foreground max-w-3xl text-sm leading-6'>
        {t(
          'Current operator: {{name}}. Every suspension, report decision, and dispute resolution is recorded with the operator in the task audit trail.',
          {
            name: user?.display_name ?? user?.username ?? t('Current admin'),
          }
        )}
      </p>
      <SectionPageLayout.Content>
        <div className='mx-auto grid w-full max-w-[1440px] gap-4 xl:grid-cols-[minmax(0,1.2fr)_minmax(360px,0.8fr)]'>
          <section className='border-border/70 bg-card/55 overflow-hidden rounded-xl border'>
            <div className='border-border/60 flex items-center gap-2 border-b px-4 py-3 text-sm font-semibold'>
              <ShieldCheck className='text-info size-4' aria-hidden='true' />
              {t('Task queue')}
            </div>
            {tasks.isLoading ? (
              <div className='space-y-3 p-4'>
                {Array.from({ length: 5 }).map((_, index) => (
                  <Skeleton key={index} className='h-16 w-full' />
                ))}
              </div>
            ) : tasks.error ? (
              <AdminError />
            ) : tasks.data?.items.length ? (
              <div>
                {tasks.data.items.map((task) => (
                  <AdminTaskRow key={task.task_id} task={task} />
                ))}
              </div>
            ) : (
              <AdminEmpty label={t('No bounty tasks in the queue.')} />
            )}
          </section>
          <section className='border-border/70 bg-card/55 overflow-hidden rounded-xl border'>
            <div className='border-border/60 flex items-center gap-2 border-b px-4 py-3 text-sm font-semibold'>
              <Scale className='text-warning size-4' aria-hidden='true' />
              {t('Open disputes')}
            </div>
            {disputes.isLoading ? (
              <div className='space-y-3 p-4'>
                {Array.from({ length: 3 }).map((_, index) => (
                  <Skeleton key={index} className='h-24 w-full' />
                ))}
              </div>
            ) : disputes.error ? (
              <AdminError />
            ) : (
              <div className='space-y-3 p-4'>
                {disputes.data?.length ? (
                  disputes.data.map((dispute) => (
                    <AdminDisputeRow
                      key={dispute.dispute_id}
                      dispute={dispute}
                    />
                  ))
                ) : (
                  <p className='text-muted-foreground text-sm'>
                    {t('No open disputes.')}
                  </p>
                )}
              </div>
            )}
          </section>
          <section className='border-border/70 bg-card/55 overflow-hidden rounded-xl border xl:col-span-2'>
            <div className='border-border/60 flex items-center gap-2 border-b px-4 py-3 text-sm font-semibold'>
              <FileWarning
                className='text-destructive size-4'
                aria-hidden='true'
              />
              {t('Open reports')}
            </div>
            {reports.isLoading ? (
              <div className='space-y-3 p-4'>
                {Array.from({ length: 2 }).map((_, index) => (
                  <Skeleton key={index} className='h-20 w-full' />
                ))}
              </div>
            ) : reports.error ? (
              <AdminError />
            ) : reports.data?.length ? (
              <div className='grid gap-3 p-4 md:grid-cols-2'>
                {reports.data.map((report) => (
                  <AdminReportRow key={report.report_id} report={report} />
                ))}
              </div>
            ) : (
              <AdminEmpty label={t('No open reports.')} />
            )}
          </section>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

function AdminError() {
  const { t } = useTranslation()
  return (
    <div
      className='text-destructive flex items-center gap-2 p-4 text-sm'
      role='alert'
    >
      <CircleAlert className='size-4' aria-hidden='true' />
      {t('Unable to load admin bounty data. Refresh and try again.')}
    </div>
  )
}

function AdminEmpty(props: { label: string }) {
  return <p className='text-muted-foreground p-4 text-sm'>{props.label}</p>
}

function AdminTaskRow(props: { task: BountyListResponse['items'][number] }) {
  const { t } = useTranslation()
  const suspend = useAdminBountyAction(props.task.task_id, 'suspend')
  const resume = useAdminBountyAction(props.task.task_id, 'resume')
  const [suspendOpen, setSuspendOpen] = useState(false)
  return (
    <div className='border-border/60 flex flex-col gap-3 border-b px-4 py-4 last:border-b-0 sm:flex-row sm:items-center sm:justify-between'>
      <div className='min-w-0'>
        <div className='truncate text-sm font-medium'>{props.task.title}</div>
        <div className='text-muted-foreground mt-1 flex flex-wrap gap-x-3 gap-y-1 text-xs'>
          <span>{taskStatusLabel(props.task.status, t)}</span>
          <span className='font-mono'>
            {formatBountyAmount(props.task.reward_amount)}
          </span>
          <span>{formatBountyDate(props.task.updated_at)}</span>
        </div>
      </div>
      <div className='flex shrink-0 gap-2'>
        <Button
          variant='outline'
          size='sm'
          render={<a href={`/bounties/${props.task.task_id}`} />}
        >
          {t('Inspect')}
        </Button>
        {props.task.status === 'suspended' ? (
          <Button
            variant='outline'
            size='sm'
            onClick={() => resume.mutate({})}
            disabled={resume.isPending}
          >
            <Play aria-hidden='true' />
            {t('Resume')}
          </Button>
        ) : (
          <Button
            variant='ghost'
            size='sm'
            onClick={() => setSuspendOpen(true)}
            disabled={suspend.isPending}
          >
            <Pause aria-hidden='true' />
            {t('Suspend')}
          </Button>
        )}
      </div>
      <ConfirmDialog
        open={suspendOpen}
        onOpenChange={setSuspendOpen}
        title={t('Suspend this bounty task?')}
        desc={t(
          'Suspension pauses task deadlines and blocks participant actions until an administrator resumes it.'
        )}
        confirmText={t('Suspend')}
        destructive
        isLoading={suspend.isPending}
        handleConfirm={() =>
          suspend.mutate({}, { onSuccess: () => setSuspendOpen(false) })
        }
      />
    </div>
  )
}

function AdminDisputeRow(props: { dispute: BountyDispute }) {
  const { t } = useTranslation()
  const [amount, setAmount] = useState('')
  const [note, setNote] = useState('')
  const [pendingResolution, setPendingResolution] = useState<string | null>(
    null
  )
  const resolveMutation = useResolveAdminBountyDispute(props.dispute.task_id)
  const resolve = async (resolutionType: string) => {
    await resolveMutation.mutateAsync({
      dispute_id: props.dispute.dispute_id,
      resolution_type: resolutionType as
        | 'pay_full'
        | 'pay_partial'
        | 'release'
        | 'changes_requested',
      amount: amount ? bountyUsdToQuota(Number(amount)) : undefined,
      note,
    })
    setPendingResolution(null)
  }
  return (
    <article className='border-border/60 space-y-3 rounded-lg border p-3'>
      <div className='text-sm font-medium'>{props.dispute.reason}</div>
      <div className='text-muted-foreground text-xs'>
        {props.dispute.opened_by.display_name} ·{' '}
        {formatBountyDate(props.dispute.created_at)}
      </div>
      <div className='bg-muted/35 rounded-lg p-3 text-xs leading-5'>
        {props.dispute.ai_analysis?.recommended_resolution ??
          t('Manual review required')}
      </div>
      <div className='space-y-1'>
        <label
          htmlFor={`partial-amount-${props.dispute.dispute_id}`}
          className='text-muted-foreground text-xs'
        >
          {t('Partial settlement amount (USD)')}
        </label>
        <div className='flex gap-2'>
          <input
            id={`partial-amount-${props.dispute.dispute_id}`}
            value={amount}
            onChange={(event) => setAmount(event.target.value)}
            type='number'
            min={0.01}
            step={0.01}
            className='border-input bg-background h-8 min-w-0 flex-1 rounded-lg border px-2 text-xs'
            placeholder={t('Partial amount (USD)')}
          />
          <Button
            size='sm'
            onClick={() => setPendingResolution('pay_full')}
            disabled={resolveMutation.isPending}
          >
            {t('Pay full')}
          </Button>
          <Button
            size='sm'
            variant='outline'
            onClick={() => setPendingResolution('pay_partial')}
            disabled={resolveMutation.isPending || !amount}
          >
            {t('Pay part')}
          </Button>
          <Button
            size='sm'
            variant='ghost'
            onClick={() => setPendingResolution('release')}
            disabled={resolveMutation.isPending}
          >
            {t('Release')}
          </Button>
        </div>
      </div>
      <Button
        size='sm'
        variant='outline'
        onClick={() => setPendingResolution('changes_requested')}
        disabled={resolveMutation.isPending}
      >
        {t('Require changes')}
      </Button>
      <div className='space-y-1'>
        <label
          htmlFor={`dispute-resolution-note-${props.dispute.dispute_id}`}
          className='text-muted-foreground text-xs'
        >
          {t('Resolution note')}
        </label>
        <textarea
          id={`dispute-resolution-note-${props.dispute.dispute_id}`}
          value={note}
          onChange={(event) => setNote(event.target.value)}
          placeholder={t('Resolution note')}
          className='border-input bg-background min-h-16 w-full rounded-lg border px-2 py-1.5 text-xs'
        />
      </div>
      <ConfirmDialog
        open={pendingResolution !== null}
        onOpenChange={(open) => {
          if (!open) setPendingResolution(null)
        }}
        title={t('Confirm dispute resolution')}
        desc={
          pendingResolution === 'release'
            ? t(
                'Release returns the frozen reward to the publisher and closes the dispute.'
              )
            : pendingResolution === 'changes_requested'
              ? t(
                  'The task returns to a revision flow without settling the reward.'
                )
              : t(
                  'This decision writes a permanent ledger-linked resolution for the dispute.'
                )
        }
        confirmText={t('Confirm resolution')}
        destructive={pendingResolution === 'release'}
        isLoading={resolveMutation.isPending}
        handleConfirm={() => {
          if (pendingResolution) void resolve(pendingResolution)
        }}
      />
    </article>
  )
}

function AdminReportRow(props: { report: BountyReport }) {
  const { t } = useTranslation()
  const [note, setNote] = useState('')
  const [confirmOpen, setConfirmOpen] = useState(false)
  const resolve = useAdminBountyAction(
    props.report.task_id,
    `reports/${props.report.report_id}/resolve`
  )
  return (
    <article className='border-border/60 space-y-3 rounded-lg border p-3'>
      <div className='text-sm font-medium'>{props.report.reason}</div>
      <div className='text-muted-foreground text-xs'>
        {props.report.reporter.display_name} ·{' '}
        {formatBountyDate(props.report.created_at)}
      </div>
      {props.report.details ? (
        <p className='text-sm leading-5 whitespace-pre-wrap'>
          {props.report.details}
        </p>
      ) : null}
      <div className='space-y-1'>
        <label
          htmlFor={`report-resolution-note-${props.report.report_id}`}
          className='text-muted-foreground text-xs'
        >
          {t('Resolution note')}
        </label>
        <textarea
          id={`report-resolution-note-${props.report.report_id}`}
          value={note}
          onChange={(event) => setNote(event.target.value)}
          placeholder={t('Resolution note')}
          className='border-input bg-background min-h-16 w-full rounded-lg border px-2 py-1.5 text-xs'
        />
      </div>
      <Button size='sm' variant='outline' onClick={() => setConfirmOpen(true)}>
        {t('Mark report handled')}
      </Button>
      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={t('Mark report handled?')}
        desc={t(
          'The report will be closed and the handling note will be kept in the audit timeline.'
        )}
        confirmText={t('Confirm')}
        isLoading={resolve.isPending}
        handleConfirm={() =>
          resolve.mutate({ note }, { onSuccess: () => setConfirmOpen(false) })
        }
      />
    </article>
  )
}
