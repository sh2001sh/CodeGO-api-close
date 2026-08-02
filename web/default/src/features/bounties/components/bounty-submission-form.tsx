import { useState } from 'react'
import { ImagePlus, Plus, Send, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { useBountyAction } from '../hooks/use-bounty-actions'
import { bountyRequiresEffectImages } from '../lib/bounty-format'
import type { BountyTask } from '../types'

export function BountySubmissionForm(props: { task: BountyTask }) {
  const { t } = useTranslation()
  const requiresEffectImages = bountyRequiresEffectImages(
    props.task.task_type,
    props.task.tags
  )
  const [repoUrl, setRepoUrl] = useState(props.task.repo_url)
  const [issueUrl, setIssueUrl] = useState('')
  const [pullRequestUrl, setPullRequestUrl] = useState('')
  const [commitSha, setCommitSha] = useState('')
  const [completionSummary, setCompletionSummary] = useState('')
  const [images, setImages] = useState<string[]>([])
  const [testReport, setTestReport] = useState('')
  const [limitations, setLimitations] = useState('')
  const mutation = useBountyAction(props.task.task_id, 'submissions')
  const addImage = () => setImages([...images, ''])
  const submit = () => {
    mutation.mutate({
      repo_url: repoUrl,
      issue_url: issueUrl,
      pull_request_url: pullRequestUrl,
      commit_sha: commitSha,
      completion_summary: completionSummary,
      effect_images: images.filter(Boolean),
      test_report: testReport,
      known_limitations: limitations,
    })
  }
  return (
    <div className='border-border/70 bg-card/60 space-y-5 rounded-xl border p-4 sm:p-5'>
      <div>
        <h2 className='text-base font-semibold'>{t('Submit delivery')}</h2>
        <p className='text-muted-foreground mt-1 text-sm'>
          {t(
            'The repository, final Commit SHA, and test evidence are fixed as a submission version.'
          )}
        </p>
      </div>
      <div className='grid gap-4 sm:grid-cols-2'>
        <div className='space-y-2 sm:col-span-2'>
          <Label htmlFor='submission-repo'>
            {t('GitHub repository or PR URL')} *
          </Label>
          <Input
            id='submission-repo'
            value={repoUrl}
            onChange={(event) => setRepoUrl(event.target.value)}
          />
        </div>
        <div className='space-y-2'>
          <Label htmlFor='submission-issue'>{t('Issue URL')}</Label>
          <Input
            id='submission-issue'
            value={issueUrl}
            onChange={(event) => setIssueUrl(event.target.value)}
          />
        </div>
        <div className='space-y-2'>
          <Label htmlFor='submission-pr'>{t('Pull Request URL')}</Label>
          <Input
            id='submission-pr'
            value={pullRequestUrl}
            onChange={(event) => setPullRequestUrl(event.target.value)}
          />
        </div>
        <div className='space-y-2 sm:col-span-2'>
          <Label htmlFor='submission-commit'>{t('Final Commit SHA')} *</Label>
          <Input
            id='submission-commit'
            value={commitSha}
            onChange={(event) => setCommitSha(event.target.value)}
            placeholder='a1b2c3d'
            className='font-mono'
          />
        </div>
        <div className='space-y-2 sm:col-span-2'>
          <Label htmlFor='submission-summary'>
            {t('Completion summary')} *
          </Label>
          <Textarea
            id='submission-summary'
            value={completionSummary}
            onChange={(event) => setCompletionSummary(event.target.value)}
            placeholder={t(
              'Summarize what changed and how the delivery meets the task requirements.'
            )}
            className='min-h-24 resize-y'
          />
        </div>
        <div className='space-y-2 sm:col-span-2'>
          <Label htmlFor='submission-test'>{t('Test results')} *</Label>
          <Textarea
            id='submission-test'
            value={testReport}
            onChange={(event) => setTestReport(event.target.value)}
            placeholder={t(
              'Example: bun run test passed; bun run build passed'
            )}
            className='min-h-24 resize-y font-mono text-xs'
          />
        </div>
        <div className='space-y-2 sm:col-span-2'>
          <div className='flex items-center justify-between'>
            <Label>{t('Effect images')}</Label>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={addImage}
            >
              <Plus aria-hidden='true' />
              {t('Add GitHub image')}
            </Button>
          </div>
          <p className='text-muted-foreground text-xs'>
            {requiresEffectImages
              ? t(
                  'UI and frontend tasks must include images from the final Commit.'
                )
              : t(
                  'Optional for non-UI tasks; use GitHub-hosted screenshots or test evidence.'
                )}
          </p>
          {images.length ? (
            <div className='space-y-2'>
              {images.map((image, index) => (
                <div key={`${index}-${image}`} className='space-y-1'>
                  <Label
                    htmlFor={`submission-image-${index}`}
                    className='text-xs'
                  >
                    {t('Effect image URL {{number}}', { number: index + 1 })}
                  </Label>
                  <div className='flex gap-2'>
                    <Input
                      id={`submission-image-${index}`}
                      value={image}
                      onChange={(event) =>
                        setImages(
                          images.map((item, itemIndex) =>
                            itemIndex === index ? event.target.value : item
                          )
                        )
                      }
                      placeholder='https://github.com/.../preview.png'
                    />
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon-sm'
                      aria-label={t('Remove image')}
                      onClick={() =>
                        setImages(
                          images.filter((_, itemIndex) => itemIndex !== index)
                        )
                      }
                    >
                      <Trash2 aria-hidden='true' />
                    </Button>
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <div className='border-border/60 text-muted-foreground flex items-center gap-2 rounded-lg border border-dashed p-3 text-sm'>
              <ImagePlus className='size-4' aria-hidden='true' />
              {t('No effect image added')}
            </div>
          )}
        </div>
        <div className='space-y-2 sm:col-span-2'>
          <Label htmlFor='submission-limitations'>
            {t('Known limitations')}
          </Label>
          <Textarea
            id='submission-limitations'
            value={limitations}
            onChange={(event) => setLimitations(event.target.value)}
            className='min-h-20 resize-y'
          />
        </div>
      </div>
      <div className='flex justify-end'>
        <Button
          onClick={submit}
          disabled={
            !repoUrl ||
            !commitSha ||
            !completionSummary.trim() ||
            !testReport ||
            mutation.isPending ||
            (requiresEffectImages && images.length === 0)
          }
        >
          <Send aria-hidden='true' />
          {mutation.isPending ? t('Submitting…') : t('Submit for review')}
        </Button>
      </div>
      {mutation.error ? (
        <p className='text-destructive text-sm' role='alert'>
          {mutation.error instanceof Error
            ? mutation.error.message
            : t('Submission failed. Check the GitHub links and try again.')}
        </p>
      ) : null}
    </div>
  )
}
