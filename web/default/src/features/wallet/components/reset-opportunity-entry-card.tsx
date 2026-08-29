import { Link } from '@tanstack/react-router'
import { ArrowRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import type { SubscriptionResetOpportunitySummary } from '@/features/subscriptions/types'

interface ResetOpportunityEntryCardProps {
  resetOpportunity: SubscriptionResetOpportunitySummary
  title?: string
  description?: string
  compact?: boolean
  className?: string
}

export function ResetOpportunityEntryCard(
  props: ResetOpportunityEntryCardProps
) {
  const { t } = useTranslation()
  const availableCount = props.resetOpportunity.available_count
  const monthlyState = props.resetOpportunity.used_this_month
    ? t('Used this month')
    : availableCount > 0
      ? t('One reset available this month')
      : t('No opportunity available')

  return (
    <div className={cn('app-subtle-panel p-4', props.className)}>
      <div className='flex items-start justify-between gap-3'>
        <div className='flex min-w-0 items-center gap-2.5'>
          <span aria-hidden className='bg-primary block h-3 w-[3px] shrink-0' />
          <div className='text-foreground truncate text-[13px] font-semibold'>
            {props.title || t('Quota reset opportunity')}
          </div>
        </div>
        <div className='border-warning/20 bg-background/80 text-foreground rounded-full border px-3 py-1 text-xs font-semibold'>
          {t('{{count}} available', { count: availableCount })}
        </div>
      </div>

      <div className='mt-3 grid gap-2 sm:grid-cols-2'>
        <div className='border-border/70 bg-background/72 rounded-xl border px-3 py-2'>
          <div className='text-muted-foreground text-[11px] font-medium'>
            {t('Current status')}
          </div>
          <div className='mt-1 text-sm font-semibold'>{monthlyState}</div>
        </div>
        <div className='border-border/70 bg-background/72 rounded-xl border px-3 py-2'>
          <div className='text-muted-foreground text-[11px] font-medium'>
            {t('Total earned / used')}
          </div>
          <div className='mt-1 text-sm font-semibold'>
            {props.resetOpportunity.earned_total} /{' '}
            {props.resetOpportunity.used_total}
          </div>
        </div>
      </div>

      <Button
        className={cn('mt-3 w-full justify-between', props.compact && 'h-9')}
        variant='outline'
        render={<Link to='/invite-rewards' />}
      >
        <span>{t('Invite and reset')}</span>
        <ArrowRight data-icon='inline-end' />
      </Button>
    </div>
  )
}
