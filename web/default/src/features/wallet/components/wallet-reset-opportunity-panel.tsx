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
  hideDetailsLink?: boolean
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
            {t('历史刷新权益')}
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
        <WalletStatItem label={t('This month status')} value={monthlyStatus} />
      </div>

      <div className='border-border/70 bg-background/72 text-muted-foreground mt-3 rounded-2xl border px-3 py-3 text-xs'>
        <div className='text-foreground font-medium'>
          {t('Applies to: {{subscription}}', {
            subscription:
              props.currentSubscriptionTitle || t('No active subscription'),
          })}
        </div>
        {props.resetOpportunity.used_this_month ? (
          <div className='text-warning mt-2'>{t('Used this month')}</div>
        ) : !hasCurrentSubscription ? (
          <div className='text-warning mt-2'>{t('暂无可刷新的旧套餐')}</div>
        ) : props.resetOpportunity.available_count <= 0 ? (
          <div className='text-warning mt-2'>
            {t('No opportunity available')}
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
          {t('刷新旧套餐额度')}
        </Button>
        {!props.hideDetailsLink && (
          <Button
            variant='outline'
            className='flex-1'
            render={<Link to='/invite-rewards' />}
          >
            {t('View reset details')}
          </Button>
        )}
      </div>
    </div>
  )
}
