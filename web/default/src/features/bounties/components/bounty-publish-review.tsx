import { Link } from '@tanstack/react-router'
import { ArrowLeft, Check, LockKeyhole } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatUsdAmount } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { DrawerFooter } from '@/components/ui/drawer'
import { Skeleton } from '@/components/ui/skeleton'
import type { BountyFormValues } from '../lib/bounty-form'
import {
  bountyUsdToQuota,
  formatBountyAmount,
  walletLabel,
} from '../lib/bounty-format'
import type { BountyBalance } from '../types'

interface BountyPublishReviewProps {
  values: BountyFormValues
  onSaveDraft: () => void
  onBackToEdit: () => void
  onSubmit: () => void
  saving: boolean
  publishing: boolean
  balances: BountyBalance[]
  balancesError: boolean
  balancesLoading: boolean
  error: Error | null
}

export function BountyPublishReview(props: BountyPublishReviewProps) {
  const { t } = useTranslation()
  const selectedBalance = props.balances.find(
    (balance) => balance.wallet_type === props.values.reward_wallet_type
  )
  const availableBalance = selectedBalance?.available_balance ?? 0
  const rewardAmountInQuota = bountyUsdToQuota(props.values.reward_amount)
  const balanceInsufficient =
    !props.balancesLoading &&
    !props.balancesError &&
    rewardAmountInQuota > availableBalance
  return (
    <div className='flex min-h-0 flex-1 flex-col overflow-y-auto'>
      <div className='space-y-5 p-5'>
        {props.error ? (
          <div
            className='border-destructive/30 bg-destructive/5 text-destructive rounded-lg border p-3 text-sm leading-6'
            role='alert'
          >
            {props.error.message ||
              t('Publishing failed. No quota was frozen.')}
          </div>
        ) : null}
        <div className='border-border/70 bg-muted/30 space-y-4 rounded-xl border p-4'>
          <div className='text-muted-foreground text-xs font-medium'>
            {t('Review before publishing')}
          </div>
          <div>
            <div className='text-muted-foreground text-xs'>{t('Title')}</div>
            <div className='mt-1 text-sm font-semibold'>
              {props.values.title}
            </div>
          </div>
          <div>
            <div className='text-muted-foreground text-xs'>{t('GitHub')}</div>
            <div className='mt-1 truncate text-sm'>{props.values.repo_url}</div>
          </div>
          <div className='grid gap-4 sm:grid-cols-2'>
            <div>
              <div className='text-muted-foreground text-xs'>{t('Reward')}</div>
              <div className='mt-1 font-mono text-lg font-semibold'>
                {formatUsdAmount(props.values.reward_amount)}{' '}
                <span className='text-muted-foreground font-sans text-xs font-normal'>
                  {walletLabel(props.values.reward_wallet_type, t)}
                </span>
              </div>
            </div>
            <div>
              <div className='text-muted-foreground text-xs'>
                {t('Deadline')}
              </div>
              <div className='mt-1 text-sm'>
                {new Date(props.values.deadline_at).toLocaleString()}
              </div>
            </div>
          </div>
        </div>
        <div className='border-warning/30 bg-warning/8 text-foreground flex items-start gap-3 rounded-xl border p-4 text-sm leading-6'>
          <LockKeyhole
            className='text-warning mt-0.5 size-4 shrink-0'
            aria-hidden='true'
          />
          <p>
            {t(
              'Publishing will freeze {{amount}} {{wallet}}. It is paid after acceptance and released when the task is cancelled, expires, or is resolved in your favor.',
              {
                amount: formatUsdAmount(props.values.reward_amount),
                wallet: walletLabel(props.values.reward_wallet_type, t),
              }
            )}
          </p>
        </div>
        <div className='border-border/70 bg-card/60 space-y-3 rounded-xl border p-4'>
          <div className='text-sm font-medium'>
            {t('Selected quota balance')}
          </div>
          {props.balancesLoading ? (
            <Skeleton className='h-5 w-48' />
          ) : props.balancesError ? (
            <p className='text-destructive text-sm leading-6'>
              {t('Unable to verify quota balance. Refresh and try again.')}
            </p>
          ) : (
            <div className='space-y-3'>
              <div className='grid gap-2 sm:grid-cols-2'>
                {props.balances.map((balance) => (
                  <div
                    key={balance.wallet_type}
                    className='border-border/60 bg-background/45 rounded-lg border p-3 text-sm'
                  >
                    <div className='text-muted-foreground text-xs'>
                      {walletLabel(balance.wallet_type, t)}
                    </div>
                    <div className='mt-1 font-mono font-semibold tabular-nums'>
                      {t('Available {{amount}} · Frozen {{reserved}}', {
                        amount: formatBountyAmount(balance.available_balance),
                        reserved: formatBountyAmount(balance.reserved_balance),
                      })}
                    </div>
                  </div>
                ))}
              </div>
              <div className='flex justify-end'>
                <Button
                  variant='outline'
                  size='sm'
                  render={<Link to='/wallet' />}
                >
                  {t('Go to wallet')}
                </Button>
              </div>
            </div>
          )}
          {balanceInsufficient ? (
            <p className='text-destructive text-sm leading-6'>
              {t(
                'Reward exceeds available balance. Top up or choose another quota type.'
              )}
            </p>
          ) : null}
        </div>
      </div>
      <DrawerFooter className='border-border/70 border-t'>
        <Button
          type='button'
          variant='ghost'
          onClick={props.onSaveDraft}
          disabled={props.saving}
        >
          {props.saving ? t('Saving…') : t('Save draft')}
        </Button>
        <Button type='button' variant='outline' onClick={props.onBackToEdit}>
          <ArrowLeft aria-hidden='true' />
          {t('Back to edit')}
        </Button>
        <Button
          type='button'
          onClick={props.onSubmit}
          disabled={
            props.publishing ||
            props.balancesLoading ||
            props.balancesError ||
            balanceInsufficient
          }
        >
          <Check aria-hidden='true' />
          {props.publishing ? t('Publishing…') : t('Confirm and freeze quota')}
        </Button>
      </DrawerFooter>
    </div>
  )
}
