import { Activity, WalletCards } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatUsdAmount, quotaUnitsToUsd } from '@/lib/format'
import { Skeleton } from '@/components/ui/skeleton'
import type {
  PlanRecord,
  SelfSubscriptionData,
} from '@/features/subscriptions/types'
import type { UserWalletData } from '../types'
import { ResetOpportunityEntryCard } from './reset-opportunity-entry-card'
import { WalletStatItem } from './wallet-panel-primitives'

interface WalletStatsCardProps {
  user: UserWalletData | null
  plans: PlanRecord[]
  loading?: boolean
  subscriptionData?: SelfSubscriptionData | null
  subscriptionLoading?: boolean
  onSubscriptionRefresh?: () => Promise<void>
}

export function WalletStatsCard(props: WalletStatsCardProps) {
  const { t } = useTranslation()
  const activeSubscriptions = props.subscriptionData?.subscriptions || []
  const resetOpportunity = props.subscriptionData?.reset_opportunity ?? {
    available_count: 0,
    earned_total: 0,
    used_total: 0,
    used_this_month: false,
    current_month: '',
    last_used_month: '',
  }

  if (props.loading) {
    return (
      <aside className='space-y-4 lg:sticky lg:top-4'>
        {Array.from({ length: 2 }).map((_, index) => (
          <div key={index} className='app-page-shell p-4'>
            <Skeleton className='h-5 w-28' />
            <Skeleton className='mt-3 h-10 w-full' />
            <Skeleton className='mt-3 h-10 w-full' />
          </div>
        ))}
      </aside>
    )
  }

  return (
    <aside className='space-y-4 lg:sticky lg:top-4'>
      <div className='app-page-shell p-4'>
        <div className='text-foreground flex items-center gap-2 text-sm font-semibold'>
          <WalletCards className='text-primary h-4 w-4' />
          {t('Wallet balance')}
        </div>
        <div className='text-foreground mt-3 font-mono text-3xl font-bold tracking-tight tabular-nums'>
          {formatUsdAmount(quotaUnitsToUsd(props.user?.quota ?? 0))}
        </div>
        <div className='text-muted-foreground mt-1 text-xs'>
          {t('通用额度')}
        </div>
        <div className='mt-4 grid gap-2'>
          <WalletStatItem
            label={t('Total spent')}
            value={formatUsdAmount(
              quotaUnitsToUsd(props.user?.used_quota ?? 0)
            )}
          />
          <WalletStatItem
            label={t('API requests')}
            value={(props.user?.request_count ?? 0).toLocaleString()}
            icon={<Activity className='text-muted-foreground h-4 w-4' />}
          />
          <WalletStatItem
            label={t('Active subscriptions')}
            value={`${activeSubscriptions.length}`}
          />
        </div>
      </div>

      <ResetOpportunityEntryCard
        resetOpportunity={resetOpportunity}
        compact
        title={t('Plan quota reset')}
        description={t(
          'Invite a new user to make a first purchase to earn a reset opportunity. Your current status is shown here.'
        )}
      />
    </aside>
  )
}
