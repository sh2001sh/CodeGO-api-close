import { useState } from 'react'
import { Check, GitPullRequest, RotateCcw, ShieldAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { useBountyAction } from '../hooks/use-bounty-actions'
import type { BountyTask } from '../types'

export function BountyReviewPanel(props: { task: BountyTask }) {
  const { t } = useTranslation()
  const [comment, setComment] = useState('')
  const [confirmAction, setConfirmAction] = useState<string | null>(null)
  const mutation = useBountyAction(props.task.task_id, 'review')
  const review = (action: string) => {
    mutation.mutate(
      { action, comment },
      { onSuccess: () => setConfirmAction(null) }
    )
  }
  return (
    <section className='border-border/70 bg-card/60 space-y-4 rounded-xl border p-4 sm:p-5'>
      <div className='flex items-start gap-3'>
        <GitPullRequest
          className='text-info mt-0.5 size-5 shrink-0'
          aria-hidden='true'
        />
        <div>
          <h2 className='text-base font-semibold'>{t('Review delivery')}</h2>
          <p className='text-muted-foreground mt-1 text-sm'>
            {t(
              'You have 72 hours after delivery to accept, request changes, or open a dispute.'
            )}
          </p>
        </div>
      </div>
      <Label htmlFor='bounty-review-comment' className='text-sm'>
        {t('Review note')}
      </Label>
      <Textarea
        id='bounty-review-comment'
        value={comment}
        onChange={(event) => setComment(event.target.value)}
        placeholder={t(
          'Add a short acceptance note or explain what needs to change.'
        )}
        className='min-h-24 resize-y'
      />
      <div className='flex flex-wrap gap-2'>
        <Button
          onClick={() => setConfirmAction('approve')}
          disabled={mutation.isPending}
        >
          <Check aria-hidden='true' />
          {t('Accept and settle')}
        </Button>
        <Button
          variant='outline'
          onClick={() => setConfirmAction('request_changes')}
          disabled={!comment.trim() || mutation.isPending}
        >
          <RotateCcw aria-hidden='true' />
          {t('Request changes')}
        </Button>
        <Button
          variant='destructive'
          onClick={() => setConfirmAction('dispute')}
          disabled={!comment.trim() || mutation.isPending}
        >
          <ShieldAlert aria-hidden='true' />
          {t('Reject and open dispute')}
        </Button>
      </div>
      <ConfirmDialog
        open={confirmAction !== null}
        onOpenChange={(open) => {
          if (!open) setConfirmAction(null)
        }}
        title={t('Confirm review decision')}
        desc={
          confirmAction === 'approve'
            ? t('Acceptance settles the frozen reward immediately.')
            : confirmAction === 'request_changes'
              ? t(
                  'The executor will receive a revision request and settlement will remain frozen.'
                )
              : t(
                  'This opens a dispute for admin review; the reward remains frozen.'
                )
        }
        confirmText={t('Confirm decision')}
        destructive={confirmAction === 'dispute'}
        isLoading={mutation.isPending}
        handleConfirm={() => {
          if (confirmAction) review(confirmAction)
        }}
      />
    </section>
  )
}
