import { Activity, WalletCards } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatUsdAmount, quotaUnitsToUsd } from '@/lib/format'
import type { UserWalletData } from '../types'
import { RedemptionCodePanel } from './redemption-code-panel'
import { WalletStatItem } from './wallet-panel-primitives'

interface WalletBalancePanelsProps {
  user: UserWalletData | null
  topupLink?: string
  redemptionCode: string
  onRedemptionCodeChange: (code: string) => void
  onRedeem: () => void
  redeeming: boolean
}

export function WalletBalancePanels(props: WalletBalancePanelsProps) {
  const { t } = useTranslation()

  return (
    <div className='grid gap-4 xl:grid-cols-[minmax(0,1.15fr)_minmax(320px,0.85fr)]'>
      <div className='app-page-shell p-4'>
        <div className='text-foreground flex items-center gap-2 text-sm font-semibold'>
          <WalletCards className='text-primary h-4 w-4' />
          {t('Wallet balance')}
        </div>
        <div className='text-muted-foreground mt-1 text-xs leading-5'>
          {t(
            '官方 GPT 专属额度仅用于官方 GPT 分组；通用额度用于 Claude、DeepSeek、GLM 以及全部第三方市场分组。'
          )}
        </div>
        <div className='mt-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-4'>
          <WalletStatItem
            label={t('通用额度')}
            value={formatUsdAmount(
              quotaUnitsToUsd(props.user?.claude_quota ?? 0)
            )}
          />
          <WalletStatItem
            label={t('官方 GPT 专属额度')}
            value={formatUsdAmount(quotaUnitsToUsd(props.user?.quota ?? 0))}
          />
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
        </div>
      </div>

      <RedemptionCodePanel
        topupLink={props.topupLink}
        redemptionCode={props.redemptionCode}
        onRedemptionCodeChange={props.onRedemptionCodeChange}
        onRedeem={props.onRedeem}
        redeeming={props.redeeming}
        compact
      />
    </div>
  )
}
