import {
  GitBranch,
  GitPullRequest,
  ImageOff,
  MessageSquare,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { IconGithub } from '@/assets/brand-icons'
import { CopyButton } from '@/components/copy-button'
import { formatBountyDate } from '../lib/bounty-format'
import type { BountyDetail } from '../types'

export function BountySubmissionCard(props: {
  submission: BountyDetail['submissions'][number]
}) {
  const { t } = useTranslation()
  return (
    <article className='border-border/60 space-y-3 rounded-lg border p-3'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div className='flex items-center gap-2 text-sm font-medium'>
          <GitBranch className='text-info size-4' aria-hidden='true' />
          {t('Version {{version}}', { version: props.submission.version })}
        </div>
        <span className='text-muted-foreground text-xs'>
          {formatBountyDate(props.submission.created_at)}
        </span>
      </div>
      <div className='grid gap-2 text-sm sm:grid-cols-2'>
        <a
          href={props.submission.repo_url}
          target='_blank'
          rel='noreferrer'
          className='text-primary inline-flex min-h-11 items-center gap-2 truncate underline-offset-4 hover:underline'
        >
          <IconGithub className='size-4 shrink-0' aria-hidden='true' />
          {props.submission.repo_url}
        </a>
        <div className='bg-muted flex min-h-11 min-w-0 items-center justify-between gap-2 rounded-lg px-3'>
          <code className='min-w-0 truncate font-mono text-xs'>
            {props.submission.commit_sha}
          </code>
          <CopyButton
            value={props.submission.commit_sha}
            size='icon'
            className='size-8'
            tooltip={t('Copy Commit SHA')}
          />
        </div>
      </div>
      {props.submission.issue_url ? (
        <a
          href={props.submission.issue_url}
          target='_blank'
          rel='noreferrer'
          className='text-primary inline-flex min-h-11 items-center gap-2 text-sm underline-offset-4 hover:underline'
        >
          <MessageSquare className='size-4' aria-hidden='true' />
          {t('Open GitHub Issue')}
        </a>
      ) : null}
      {props.submission.pull_request_url ? (
        <a
          href={props.submission.pull_request_url}
          target='_blank'
          rel='noreferrer'
          className='text-primary inline-flex min-h-11 items-center gap-2 text-sm underline-offset-4 hover:underline'
        >
          <GitPullRequest className='size-4' aria-hidden='true' />
          {t('Open Pull Request')}
        </a>
      ) : null}
      <div className='bg-muted/35 rounded-lg p-3 text-sm leading-6 whitespace-pre-wrap'>
        <div className='text-muted-foreground mb-1 text-xs font-medium'>
          {t('Completion summary')}
        </div>
        {props.submission.completion_summary}
      </div>
      <div className='bg-muted/35 rounded-lg p-3 text-sm leading-6 whitespace-pre-wrap'>
        <div className='text-muted-foreground mb-1 text-xs font-medium'>
          {t('Test results')}
        </div>
        {props.submission.test_report}
      </div>
      {props.submission.known_limitations ? (
        <div className='bg-muted/35 rounded-lg p-3 text-sm leading-6 whitespace-pre-wrap'>
          <div className='text-muted-foreground mb-1 text-xs font-medium'>
            {t('Known limitations')}
          </div>
          {props.submission.known_limitations}
        </div>
      ) : null}
      {props.submission.effect_images.length ? (
        <div className='space-y-2'>
          <div className='text-muted-foreground text-xs font-medium'>
            {t('Effect images from Commit {{commit}}', {
              commit: props.submission.commit_sha,
            })}
          </div>
          <div className='grid gap-3 sm:grid-cols-2'>
            {props.submission.effect_images.map((image) => (
              <a
                key={image}
                href={image}
                target='_blank'
                rel='noreferrer'
                className='bounty-image-frame'
              >
                <img
                  src={image}
                  alt={t('GitHub effect image from Commit {{commit}}', {
                    commit: props.submission.commit_sha,
                  })}
                  className='border-border/60 bg-muted/30 aspect-video w-full rounded-lg border object-cover'
                  loading='lazy'
                  onError={(event) => {
                    event.currentTarget.style.display = 'none'
                    event.currentTarget.parentElement?.classList.add(
                      'bounty-image-error'
                    )
                  }}
                />
                <span className='bounty-image-error-content text-muted-foreground hidden items-center justify-center gap-2 p-6 text-sm'>
                  <ImageOff className='size-4' aria-hidden='true' />
                  {t(
                    'Effect image unavailable. Ask the publisher to grant GitHub access or provide a public link.'
                  )}
                </span>
              </a>
            ))}
          </div>
        </div>
      ) : null}
    </article>
  )
}
