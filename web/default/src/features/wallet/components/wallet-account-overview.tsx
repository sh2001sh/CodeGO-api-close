import {
  Activity,
  ArrowRightLeft,
  ChevronDown,
  CreditCard,
  History,
  ReceiptText,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatUsdAmount, quotaUnitsToUsd } from '@/lib/format'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import type { UserWalletData } from '../types'

export function WalletAccountOverview(props: {
  user: UserWalletData | null
  activeSubscriptionCount: number
  onSelectFunding: () => void
  onSelectConversion: () => void
  onOpenBillingHistory: () => void
  onOpenConversionHistory: () => void
}) {
  const { t } = useTranslation()
  return (
    <section className='app-page-shell p-4 sm:p-5'>
      <div className='flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between'>
        <div className='grid min-w-0 gap-3 sm:grid-cols-2 lg:min-w-[28rem]'>
          <BalanceItem
            label={t('Standard balance')}
            value={formatUsdAmount(quotaUnitsToUsd(props.user?.quota ?? 0))}
            description={t('For non-Claude models')}
          />
          <BalanceItem
            label={t('Claude quota')}
            value={formatUsdAmount(
              quotaUnitsToUsd(props.user?.claude_quota ?? 0)
            )}
            description={t('For Claude models only')}
          />
        </div>

        <div className='flex flex-col gap-3 lg:items-end'>
          <div className='text-muted-foreground flex flex-wrap gap-x-4 gap-y-1 text-xs'>
            <span>
              {t('Total spent')}:{' '}
              <strong className='text-foreground font-medium'>
                {formatUsdAmount(quotaUnitsToUsd(props.user?.used_quota ?? 0))}
              </strong>
            </span>
            <span className='inline-flex items-center gap-1'>
              <Activity className='size-3.5' />
              {(props.user?.request_count ?? 0).toLocaleString()}{' '}
              {t('API requests')}
            </span>
            <span>
              {t('Active subscriptions')} {props.activeSubscriptionCount}
            </span>
          </div>
          <div className='grid grid-cols-3 gap-2 sm:flex'>
            <Button type='button' onClick={props.onSelectFunding}>
              <CreditCard className='size-4' />
              {t('Top up')}
            </Button>
            <Button
              type='button'
              variant='outline'
              onClick={props.onSelectConversion}
            >
              <ArrowRightLeft className='size-4' />
              {t('Convert')}
            </Button>
            <DropdownMenu modal={false}>
              <DropdownMenuTrigger
                render={<Button type='button' variant='outline' />}
              >
                <History className='size-4' />
                {t('Records')}
                <ChevronDown className='size-3.5' />
              </DropdownMenuTrigger>
              <DropdownMenuContent align='end' className='w-44'>
                <DropdownMenuGroup>
                  <DropdownMenuItem onClick={props.onOpenBillingHistory}>
                    <ReceiptText className='size-4' />
                    {t('Top-up records')}
                  </DropdownMenuItem>
                  <DropdownMenuItem onClick={props.onOpenConversionHistory}>
                    <ArrowRightLeft className='size-4' />
                    {t('Conversion records')}
                  </DropdownMenuItem>
                </DropdownMenuGroup>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>
      </div>
    </section>
  )
}

function BalanceItem(props: {
  label: string
  value: string
  description: string
}) {
  return (
    <div className='min-w-0'>
      <div className='text-muted-foreground text-xs font-medium'>
        {props.label}
      </div>
      <div className='text-foreground mt-1 truncate text-2xl font-semibold tracking-tight tabular-nums'>
        {props.value}
      </div>
      <div className='text-muted-foreground mt-0.5 text-xs'>
        {props.description}
      </div>
    </div>
  )
}
