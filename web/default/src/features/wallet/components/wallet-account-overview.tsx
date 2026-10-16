import { getIntlLocale } from '@/i18n/format'
import {
  ChevronDown,
  History,
  ReceiptText,
  Send,
  WalletCards,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatQuota } from '@/lib/format'
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
  activeSubscriptionCount?: number
  creditPackBalance?: number
  onOpenBillingHistory: () => void
  onOpenTransferHistory: () => void
}) {
  const { t } = useTranslation()
  const renderBalance = (quota: number | undefined) => {
    if (quota === undefined) return '--'
    const value = formatQuota(quota)
    const parts = value.match(/^(.*?)( credits)$/)
    if (!parts) return value
    return (
      <>
        <span>{parts[1]}</span>
        <span className='commerce-balance-unit'>{parts[2].trim()}</span>
      </>
    )
  }
  return (
    <section className='commerce-wallet-summary' aria-label={t('Wallet')}>
      <div className='commerce-wallet-vault'>
        <div className='commerce-wallet-optic' aria-hidden='true'>
          <span />
          <span />
          <span />
        </div>
        <dl className='commerce-wallet-summary-values' aria-live='polite'>
          <div className='commerce-wallet-summary-balance'>
            <dt>
              {t('统一额度')}
              <WalletCards aria-hidden='true' />
            </dt>
            <dd>{renderBalance(props.user?.quota)}</dd>
          </div>
          <div className='commerce-wallet-summary-balance'>
            <dt>
              {t('新套餐余额')}
              <WalletCards aria-hidden='true' />
            </dt>
            <dd>{renderBalance(props.creditPackBalance)}</dd>
          </div>
        </dl>
        <DropdownMenu modal={false}>
          <DropdownMenuTrigger
            render={
              <Button
                type='button'
                variant='ghost'
                className='commerce-wallet-records'
              />
            }
          >
            <History className='size-4' aria-hidden='true' />
            {t('Records')}
            <ChevronDown className='size-3.5' aria-hidden='true' />
          </DropdownMenuTrigger>
          <DropdownMenuContent align='end' className='w-44'>
            <DropdownMenuGroup>
              <DropdownMenuItem onClick={props.onOpenBillingHistory}>
                <ReceiptText className='size-4' />
                {t('Top-up records')}
              </DropdownMenuItem>
              <DropdownMenuItem onClick={props.onOpenTransferHistory}>
                <Send className='size-4' />
                {t('Transfer records')}
              </DropdownMenuItem>
            </DropdownMenuGroup>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      <dl className='commerce-wallet-activity' aria-live='polite'>
        {' '}
        <div>
          <dt>{t('Total spent')}</dt>
          <dd>{props.user ? formatQuota(props.user.used_quota) : '--'}</dd>
        </div>
        <div>
          <dt>{t('API requests')}</dt>
          <dd>
            {props.user
              ? props.user.request_count.toLocaleString(getIntlLocale())
              : '--'}
          </dd>
        </div>
        <div>
          <dt>{t('Active subscriptions')}</dt>
          <dd>{props.activeSubscriptionCount ?? '--'}</dd>
        </div>
      </dl>
    </section>
  )
}
