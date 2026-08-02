import { Link } from '@tanstack/react-router'
import { ArrowRight, RotateCcw } from 'lucide-react'
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
        <div className='min-w-0'>
          <div className='text-foreground flex items-center gap-2 text-sm font-semibold'>
            <RotateCcw className='text-warning h-4 w-4' />
            {props.title || t('Quota reset opportunity')}
          </div>
          <div className='text-muted-foreground mt-1 text-xs leading-5'>
            {props.description ||
              t(
                'Earned when an invited new user purchases a monthly plan; used to clear quota consumed by the current subscription.'
              )}
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
