import { Link } from '@tanstack/react-router'
import { Loader2, Sparkles } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import type { SubscriptionResetOpportunitySummary } from '@/features/subscriptions/types'
import { WalletStatItem } from './wallet-panel-primitives'

interface WalletResetOpportunityPanelProps {
  resetOpportunity: SubscriptionResetOpportunitySummary
  currentSubscriptionTitle?: string
  canUseResetOpportunity: boolean
  usingResetOpportunity: boolean
  onUseResetOpportunity: () => void
}

export function WalletResetOpportunityPanel(
  props: WalletResetOpportunityPanelProps
) {
  const { t } = useTranslation()
  const hasCurrentSubscription = Boolean(props.currentSubscriptionTitle)
  const monthlyStatus = props.resetOpportunity.used_this_month
    ? t('Used')
    : !hasCurrentSubscription
      ? t('No active plan')
      : props.resetOpportunity.available_count <= 0
        ? t('No opportunity available')
        : t('Available')

  return (
    <div className='app-page-shell p-4'>
      <div className='flex items-start justify-between gap-3'>
        <div>
          <div className='text-foreground flex items-center gap-2 text-sm font-semibold'>
            <Sparkles className='text-warning h-4 w-4' />
            {t('Quota reset opportunity')}
          </div>
          <div className='text-muted-foreground mt-1 text-xs leading-5'>
            {t(
              'Earned when an invited new user purchases a monthly plan. Opportunities are saved and can be used once per calendar month.'
            )}
          </div>
        </div>
        <div className='border-border bg-background/80 text-foreground rounded-full border px-3 py-1 text-xs font-semibold'>
          {t('{{count}} available', {
            count: props.resetOpportunity.available_count,
          })}
        </div>
      </div>

      <div className='mt-3 grid gap-2 sm:grid-cols-3'>
        <WalletStatItem
          label={t('Total earned')}
          value={`${props.resetOpportunity.earned_total}`}
        />
        <WalletStatItem
          label={t('Total used')}
          value={`${props.resetOpportunity.used_total}`}
        />
        <WalletStatItem
          label={t('This month status')}
          value={monthlyStatus}
        />
      </div>

      <div className='border-border/70 bg-background/72 text-muted-foreground mt-3 rounded-2xl border px-3 py-3 text-xs'>
        <div className='text-foreground font-medium'>
          {t('Applies to: {{subscription}}', {
            subscription:
              props.currentSubscriptionTitle || t('No active subscription'),
          })}
        </div>
        <div className='mt-1 leading-5'>
          {t(
            'Only clears used quota for the first active subscription in the billing order. It does not extend the expiry date or change subscription benefits.'
          )}
        </div>
        {props.resetOpportunity.used_this_month ? (
          <div className='text-warning mt-2'>
            {t('Already used this month. Try again next month.')}
          </div>
        ) : !hasCurrentSubscription ? (
          <div className='text-warning mt-2'>
            {t('Purchase and activate a plan to use a reset opportunity.')}
          </div>
        ) : props.resetOpportunity.available_count <= 0 ? (
          <div className='text-warning mt-2'>
            {t(
              'No reset opportunities are available. Invite a new user to purchase a monthly plan to earn one.'
            )}
          </div>
        ) : null}
      </div>

      <div className='mt-3 flex flex-wrap gap-2'>
        <Button
          className='flex-1'
          onClick={props.onUseResetOpportunity}
          disabled={
            !props.canUseResetOpportunity || props.usingResetOpportunity
          }
        >
          {props.usingResetOpportunity ? (
            <Loader2 className='mr-1 h-4 w-4 animate-spin' />
          ) : null}
          {t('Reset current subscription quota')}
        </Button>
        <Button
          variant='outline'
          className='flex-1'
          render={<Link to='/invite-rewards' />}
        >
          {t('View reset details')}
        </Button>
      </div>
    </div>
  )
}
