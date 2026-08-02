import { Download, ExternalLink, Gift, PackageOpen } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { IconGithub } from '@/assets/brand-icons'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import type { CommunityResource, ResourceConfig } from './types'

const statusVariants = {
  approved: 'default',
  pending: 'secondary',
  rejected: 'destructive',
} as const

export function CommunityResourceList(props: {
  items?: CommunityResource[]
  loading: boolean
  admin: boolean
  reviewing: boolean
  config?: ResourceConfig
  onReview: (
    resource: CommunityResource,
    status: 'approved' | 'rejected',
    grantReward: boolean
  ) => void
}) {
  const { t } = useTranslation()
  if (props.loading) {
    return (
      <div className='divide-border overflow-hidden rounded-lg border'>
        {[0, 1, 2].map((item) => (
          <div key={item} className='space-y-3 p-5'>
            <Skeleton className='h-5 w-52' />
            <Skeleton className='h-4 w-full max-w-2xl' />
            <Skeleton className='h-8 w-40' />
          </div>
        ))}
      </div>
    )
  }
  if (!props.items?.length) {
    return (
      <Empty className='min-h-64 border'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <PackageOpen />
          </EmptyMedia>
          <EmptyTitle>{t('No resources found')}</EmptyTitle>
          <EmptyDescription>
            {t('Try another filter or submit the first resource in this view.')}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <div className='divide-border overflow-hidden rounded-lg border'>
      {props.items.map((resource) => (
        <article
          key={resource.id}
          className='bg-card/40 hover:bg-muted/25 p-4 transition-colors sm:p-5'
        >
          <div className='flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between'>
            <div className='min-w-0 flex-1'>
              <div className='flex flex-wrap items-center gap-2'>
                <h2 className='text-sm font-semibold sm:text-base'>
                  {resource.title}
                </h2>
                <Badge variant='outline'>{t(resource.category)}</Badge>
                {resource.status !== 'approved' || props.admin ? (
                  <Badge variant={statusVariants[resource.status]}>
                    {t(resource.status)}
                  </Badge>
                ) : null}
                {resource.reward_quota > 0 ? (
                  <Badge variant='secondary'>
                    <Gift />
                    {t('Rewarded')}
                  </Badge>
                ) : null}
              </div>
              <p className='text-muted-foreground mt-2 max-w-3xl text-sm leading-6'>
                {resource.description}
              </p>
              <div className='text-muted-foreground mt-3 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs'>
                <span>
                  {t('Submitted by {{name}}', {
                    name: resource.submitter_name,
                  })}
                </span>
                <span>
                  {new Date(resource.created_at).toLocaleDateString()}
                </span>
                {resource.acknowledgement_url ? (
                  <a
                    className='text-primary inline-flex items-center gap-1 hover:underline'
                    href={resource.acknowledgement_url}
                    target='_blank'
                    rel='noreferrer'
                  >
                    <Gift className='size-3' />
                    {t('View shu26.cfd acknowledgement')}
                  </a>
                ) : null}
              </div>
              {resource.review_note ? (
                <p className='text-destructive mt-2 text-xs'>
                  {t('Review note: {{note}}', { note: resource.review_note })}
                </p>
              ) : null}
            </div>
            <div className='flex shrink-0 flex-wrap gap-2'>
              <Button
                variant='outline'
                size='sm'
                render={
                  <a
                    href={resource.github_url}
                    target='_blank'
                    rel='noreferrer'
                  />
                }
              >
                <IconGithub aria-hidden='true' focusable='false' />
                {t('GitHub')}
                <ExternalLink />
              </Button>
              {resource.status === 'approved' ? (
                <Button size='sm' render={<a href={resource.download_url} />}>
                  <Download />
                  {t('Download')}
                </Button>
              ) : null}
              {props.admin && resource.status === 'pending' ? (
                <>
                  <Button
                    variant='outline'
                    size='sm'
                    disabled={props.reviewing}
                    onClick={() => props.onReview(resource, 'rejected', false)}
                  >
                    {t('Reject')}
                  </Button>
                  <Button
                    size='sm'
                    disabled={props.reviewing}
                    onClick={() => props.onReview(resource, 'approved', false)}
                  >
                    {t('Approve')}
                  </Button>
                  {resource.acknowledgement_url &&
                  props.config?.reward_enabled ? (
                    <Button
                      size='sm'
                      variant='secondary'
                      disabled={props.reviewing}
                      onClick={() => props.onReview(resource, 'approved', true)}
                    >
                      <Gift />
                      {t('Approve + {{amount}} USD', {
                        amount: props.config.reward_usd,
                      })}
                    </Button>
                  ) : null}
                </>
              ) : null}
            </div>
          </div>
        </article>
      ))}
    </div>
  )
}
