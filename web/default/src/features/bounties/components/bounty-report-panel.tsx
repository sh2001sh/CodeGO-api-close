import { useState } from 'react'
import { Flag } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { useReportBounty } from '../hooks/use-bounty-actions'
import type { BountyTask } from '../types'

export function BountyReportPanel(props: { task: BountyTask }) {
  if (!props.task.can_report) return null
  return <BountyReportForm task={props.task} />
}

function BountyReportForm(props: { task: BountyTask }) {
  const { t } = useTranslation()
  const [reason, setReason] = useState('')
  const [details, setDetails] = useState('')
  const [confirmOpen, setConfirmOpen] = useState(false)
  const mutation = useReportBounty(props.task.task_id)
  return (
    <section className='border-border/70 bg-card/40 space-y-4 rounded-xl border p-4 sm:p-5'>
      <div className='flex items-start gap-3'>
        <Flag
          className='text-destructive mt-0.5 size-5 shrink-0'
          aria-hidden='true'
        />
        <div>
          <h2 className='text-base font-semibold'>{t('Report this task')}</h2>
          <p className='text-muted-foreground mt-1 text-sm'>
            {t(
              'Report malicious code, phishing links, illegal content, or requests for secrets.'
            )}
          </p>
        </div>
      </div>
      <div className='space-y-2'>
        <Label htmlFor='bounty-report-reason'>{t('Report reason')} *</Label>
        <Textarea
          id='bounty-report-reason'
          value={reason}
          onChange={(event) => setReason(event.target.value)}
          className='min-h-20 resize-y'
          placeholder={t('Explain the risk briefly.')}
        />
      </div>
      <div className='space-y-2'>
        <Label htmlFor='bounty-report-details'>{t('Additional details')}</Label>
        <Textarea
          id='bounty-report-details'
          value={details}
          onChange={(event) => setDetails(event.target.value)}
          className='min-h-20 resize-y'
        />
      </div>
      <div className='flex justify-end'>
        <Button
          variant='outline'
          onClick={() => setConfirmOpen(true)}
          disabled={!reason.trim()}
        >
          <Flag aria-hidden='true' />
          {t('Submit report')}
        </Button>
      </div>
      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={t('Submit this report?')}
        desc={t(
          'The report will be sent to administrators for review and kept with the task audit trail.'
        )}
        confirmText={t('Submit report')}
        destructive
        isLoading={mutation.isPending}
        handleConfirm={() =>
          mutation.mutate(
            { reason: reason.trim(), details: details.trim() },
            { onSuccess: () => setConfirmOpen(false) }
          )
        }
      />
    </section>
  )
}
