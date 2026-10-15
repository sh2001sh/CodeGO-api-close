import {
  Activity,
  ChevronDown,
  CreditCard,
  History,
  ReceiptText,
  Send,
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
  activeSubscriptionCount: number
  onSelectFunding: () => void
  onSelectTransfer: () => void
  onOpenBillingHistory: () => void
  onOpenTransferHistory: () => void
}) {
  const { t } = useTranslation()
  return (
    <section className='app-page-shell p-4 sm:p-5'>
      <div className='flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between'>
        <div className='min-w-0 lg:min-w-[28rem]'>
          <BalanceItem label={t('统一额度')} value={formatQuota(props.user?.quota ?? 0)} />
        </div>

        <div className='flex flex-col gap-3 lg:items-end'>
          <div className='text-muted-foreground flex flex-wrap gap-x-4 gap-y-1 text-xs'>
            <span>
              {t('Total spent')}:{' '}
              <strong className='text-foreground font-medium'>
                {formatQuota(props.user?.used_quota ?? 0)}
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
          <div className='grid grid-cols-2 gap-2 sm:flex'>
            <Button type='button' onClick={props.onSelectFunding}>
              <CreditCard className='size-4' />
              {t('Top up')}
            </Button>
            <Button
              type='button'
              variant='outline'
              onClick={props.onSelectTransfer}
            >
              <Send className='size-4' />
              {t('Transfer')}
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
                  <DropdownMenuItem onClick={props.onOpenTransferHistory}>
                    <Send className='size-4' />
                    {t('Transfer records')}
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

function BalanceItem(props: { label: string; value: string }) {
  return (
    <div className='min-w-0'>
      <div className='codego-stat-label'>{props.label}</div>
      <div className='codego-stat-value mt-3 truncate'>
        {props.value}
      </div>
    </div>
  )
}
