import { Activity, WalletCards } from 'lucide-react'
import { motion, useReducedMotion } from 'motion/react'
import { useTranslation } from 'react-i18next'
import { formatUsdAmount, quotaUnitsToUsd } from '@/lib/format'
import { MOTION_TRANSITION } from '@/lib/motion'
import type { UserWalletData } from '../types'
import { WalletStatItem } from './wallet-panel-primitives'

interface WalletSummarySidebarProps {
  user: UserWalletData | null
  activeSubscriptionCount: number
}

export function WalletSummarySidebar(props: WalletSummarySidebarProps) {
  const reduced = Boolean(useReducedMotion())
  const { t } = useTranslation()

  return (
    <motion.aside
      className='space-y-4 lg:sticky lg:top-4'
      initial={reduced ? false : { opacity: 0, y: 16 }}
      animate={{ opacity: 1, y: 0 }}
      transition={MOTION_TRANSITION.slow}
    >
      <div className='overview-glass-card rounded-2xl p-4'>
        <div className='text-foreground flex items-center gap-2 text-sm font-semibold'>
          <WalletCards className='text-primary h-4 w-4' />
          {t('Wallet balance')}
        </div>
        <div className='mt-3 grid grid-cols-2 gap-2'>
          <div className='overview-soft-card min-w-0 px-3 py-3'>
            <div className='text-muted-foreground text-[11px] font-medium tracking-wide uppercase'>
              {t('Standard balance')}
            </div>
            <div className='text-foreground mt-1.5 truncate font-mono text-xl font-bold tracking-tight tabular-nums'>
              {formatUsdAmount(quotaUnitsToUsd(props.user?.quota ?? 0))}
            </div>
          </div>
          <div className='overview-soft-card min-w-0 px-3 py-3'>
            <div className='text-muted-foreground text-[11px] font-medium tracking-wide uppercase'>
              {t('Claude balance')}
            </div>
            <div className='text-foreground mt-1.5 truncate font-mono text-xl font-bold tracking-tight tabular-nums'>
              {formatUsdAmount(quotaUnitsToUsd(props.user?.claude_quota ?? 0))}
            </div>
          </div>
        </div>
        <div className='mt-2 grid gap-2'>
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
            value={`${props.activeSubscriptionCount}`}
          />
        </div>
      </div>
    </motion.aside>
  )
}
