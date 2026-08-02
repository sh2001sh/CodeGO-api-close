import { LockKeyhole, WalletCards } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Skeleton } from '@/components/ui/skeleton'
import { formatBountyAmount, walletLabel } from '../lib/bounty-format'
import type { BountyBalance } from '../types'

interface BountyBalanceSummaryProps {
  balances: BountyBalance[]
  loading: boolean
  error?: boolean
}

export function BountyBalanceSummary(props: BountyBalanceSummaryProps) {
  const { t } = useTranslation()
  return (
    <section
      className='border-border/70 bg-card/70 grid gap-0 overflow-hidden rounded-xl border sm:grid-cols-2 lg:grid-cols-4'
      aria-label={t('Quota balance summary')}
    >
      {props.error ? (
        <div
          className='text-destructive col-span-full p-4 text-sm'
          role='alert'
        >
          {t('Unable to load quota balances. Refresh and try again.')}
        </div>
      ) : props.loading ? (
        Array.from({ length: 4 }).map((_, index) => (
          <div
            key={index}
            className='border-border/60 flex min-h-24 flex-col justify-center gap-2 border-b px-4 py-3 last:border-b-0 sm:border-e sm:last:border-e-0 lg:border-b-0'
          >
            <Skeleton className='h-3 w-24' />
            <Skeleton className='h-6 w-28' />
          </div>
        ))
      ) : (
        props.balances.flatMap((balance) => [
          <div
            key={`${balance.wallet_type}-available`}
            className='border-border/60 flex min-h-24 items-center gap-3 border-b px-4 py-3 sm:border-e lg:border-b-0'
          >
            <span className='bg-primary/10 text-primary flex size-9 shrink-0 items-center justify-center rounded-lg'>
              <WalletCards className='size-4' aria-hidden='true' />
            </span>
            <div className='min-w-0'>
              <div className='text-muted-foreground text-xs'>
                {walletLabel(balance.wallet_type, t)} · {t('Available')}
              </div>
              <div className='font-mono text-lg font-semibold tabular-nums'>
                {formatBountyAmount(balance.available_balance)}
              </div>
            </div>
          </div>,
          <div
            key={`${balance.wallet_type}-reserved`}
            className='border-border/60 flex min-h-24 items-center gap-3 border-b px-4 py-3 last:border-b-0 lg:border-b-0'
          >
            <span className='bg-warning/12 text-warning flex size-9 shrink-0 items-center justify-center rounded-lg'>
              <LockKeyhole className='size-4' aria-hidden='true' />
            </span>
            <div className='min-w-0'>
              <div className='text-muted-foreground text-xs'>
                {walletLabel(balance.wallet_type, t)} · {t('Frozen')}
              </div>
              <div className='font-mono text-lg font-semibold tabular-nums'>
                {formatBountyAmount(balance.reserved_balance)}
              </div>
            </div>
          </div>,
        ])
      )}
    </section>
  )
}
