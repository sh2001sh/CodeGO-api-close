import { useState } from 'react'
import { ShieldAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { useBountyAction } from '../hooks/use-bounty-actions'
import type { BountyTask } from '../types'

export function BountyDisputePanel(props: { task: BountyTask }) {
  const { t } = useTranslation()
  const [reason, setReason] = useState('')
  const [desiredOutcome, setDesiredOutcome] = useState('')
  const [githubLinks, setGithubLinks] = useState('')
  const [evidence, setEvidence] = useState('')
  const [confirmOpen, setConfirmOpen] = useState(false)
  const mutation = useBountyAction(props.task.task_id, 'disputes')
  return (
    <section className='border-destructive/25 bg-destructive/5 space-y-4 rounded-xl border p-4 sm:p-5'>
      <div className='flex items-start gap-3'>
        <ShieldAlert
          className='text-destructive mt-0.5 size-5 shrink-0'
          aria-hidden='true'
        />
        <div>
          <h2 className='text-base font-semibold'>{t('Open a dispute')}</h2>
          <p className='text-muted-foreground mt-1 text-sm'>
            {t(
              'The platform will attach the task timeline, discussion, GitHub delivery, and evidence analysis for admin review.'
            )}
          </p>
        </div>
      </div>
      <div className='space-y-2'>
        <Label htmlFor='dispute-reason'>{t('What is wrong?')} *</Label>
        <Textarea
          id='dispute-reason'
          value={reason}
          onChange={(event) => setReason(event.target.value)}
          className='min-h-24 resize-y'
        />
      </div>
      <div className='space-y-2'>
        <Label htmlFor='dispute-outcome'>{t('Desired outcome')}</Label>
        <Input
          id='dispute-outcome'
          value={desiredOutcome}
          onChange={(event) => setDesiredOutcome(event.target.value)}
          placeholder={t('Full settlement, partial settlement, or release')}
        />
      </div>
      <div className='space-y-2'>
        <Label htmlFor='dispute-github-links'>
          {t('Related GitHub links')}
        </Label>
        <Textarea
          id='dispute-github-links'
          value={githubLinks}
          onChange={(event) => setGithubLinks(event.target.value)}
          className='min-h-20 resize-y font-mono text-xs'
          placeholder='https://github.com/example/project/pull/42'
        />
        <p className='text-muted-foreground text-xs'>
          {t('Add one GitHub URL per line or separate links with commas.')}
        </p>
      </div>
      <div className='space-y-2'>
        <Label htmlFor='dispute-evidence'>{t('Additional evidence')}</Label>
        <Textarea
          id='dispute-evidence'
          value={evidence}
          onChange={(event) => setEvidence(event.target.value)}
          className='min-h-20 resize-y'
          placeholder={t(
            'Add GitHub links, test output, or a concise explanation.'
          )}
        />
      </div>
      <div className='flex justify-end'>
        <Button
          variant='destructive'
          onClick={() => setConfirmOpen(true)}
          disabled={!reason.trim() || mutation.isPending}
        >
          {t('Submit dispute')}
        </Button>
      </div>
      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={t('Submit this dispute?')}
        desc={t(
          'The task will enter admin review and the frozen reward will remain unchanged until a final decision.'
        )}
        confirmText={t('Submit dispute')}
        destructive
        isLoading={mutation.isPending}
        handleConfirm={() =>
          mutation.mutate(
            {
              reason,
              desired_outcome: desiredOutcome,
              evidence_text: evidence,
              github_urls: githubLinks
                .split(/[\n,]/)
                .map((value) => value.trim())
                .filter(Boolean),
            },
            { onSuccess: () => setConfirmOpen(false) }
          )
        }
      />
    </section>
  )
}
