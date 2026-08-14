import {
  ArrowRight,
  History,
  Loader2,
  LockKeyhole,
  Search,
  Send,
  ShieldCheck,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatUsdAmount } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import type { WalletTransferOverview, WalletTransferRecipient } from '../types'

export function WalletPeerTransferPanel(props: {
  overview: WalletTransferOverview
  locked: boolean
  recipientId: string
  recipient: WalletTransferRecipient | null
  recipientLoading: boolean
  amount: string
  balanceUSD: number
  minimumUSD: number
  canContinue: boolean
  onRecipientIdChange: (value: string) => void
  onLookupRecipient: () => void
  onAmountChange: (value: string) => void
  onOpenHistory?: () => void
  onOpenPassword: () => void
  onReview: () => void
}) {
  const { t } = useTranslation()

  return (
    <section className='app-page-shell p-4 sm:p-5'>
      <div className='flex flex-wrap items-start justify-between gap-3'>
        <div>
          <div className='flex items-center gap-2 text-sm font-semibold'>
            <Send className='text-primary size-4' />
            {t('Quota transfer')}
          </div>
          <p className='text-muted-foreground mt-1 max-w-2xl text-xs leading-5'>
            {t(
              'Send standard balance to another user by public ID. Claude quota, plans, and props cannot be transferred.'
            )}
          </p>
        </div>
        <div className='flex flex-wrap gap-2'>
          {props.onOpenHistory ? (
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={props.onOpenHistory}
            >
              <History className='size-4' />
              {t('Transfer records')}
            </Button>
          ) : null}
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={props.onOpenPassword}
          >
            <ShieldCheck className='size-4' />
            {props.overview.security.password_set
              ? t('Change password')
              : t('Set payment password')}
          </Button>
        </div>
      </div>

      {!props.overview.security.password_set || props.locked ? (
        <div className='border-border bg-muted/35 mt-4 flex items-start gap-3 rounded-md border p-3'>
          <LockKeyhole className='text-primary mt-0.5 size-4 shrink-0' />
          <div className='min-w-0 text-xs leading-5'>
            <p className='font-medium'>
              {props.locked
                ? t('Transfers are temporarily locked')
                : t('Set a payment password before your first transfer')}
            </p>
            <p className='text-muted-foreground'>
              {props.locked
                ? t('Try again after {{time}}.', {
                    time: new Date(
                      props.overview.security.locked_until * 1000
                    ).toLocaleString(),
                  })
                : t(
                    'Five failed password attempts lock transfers for 30 minutes.'
                  )}
            </p>
          </div>
        </div>
      ) : null}

      <div className='mt-5 grid gap-4 lg:grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)]'>
        <div className='space-y-2'>
          <Label htmlFor='wallet-transfer-recipient'>{t('Recipient ID')}</Label>
          <div className='flex gap-2'>
            <Input
              id='wallet-transfer-recipient'
              value={props.recipientId}
              maxLength={6}
              autoComplete='off'
              className='font-mono tracking-widest uppercase'
              placeholder='ABC123'
              onChange={(event) =>
                props.onRecipientIdChange(event.target.value.toUpperCase())
              }
              onKeyDown={(event) => {
                if (event.key === 'Enter') props.onLookupRecipient()
              }}
            />
            <Button
              type='button'
              variant='outline'
              size='icon'
              disabled={props.recipientLoading}
              aria-label={t('Find recipient')}
              title={t('Find recipient')}
              onClick={props.onLookupRecipient}
            >
              {props.recipientLoading ? (
                <Loader2 className='size-4 animate-spin' />
              ) : (
                <Search className='size-4' />
              )}
            </Button>
          </div>
          <p className='text-muted-foreground min-h-5 text-xs'>
            {props.recipient
              ? `${props.recipient.display_name_masked} · ${props.recipient.external_id}`
              : t('The recipient name is shown only after an exact ID match.')}
          </p>
        </div>

        <ArrowRight className='text-muted-foreground mx-auto hidden size-5 self-center lg:block' />

        <div className='space-y-2'>
          <div className='flex items-center justify-between gap-3'>
            <Label htmlFor='wallet-transfer-amount'>{t('Amount')}</Label>
            <span className='text-muted-foreground text-xs tabular-nums'>
              {t('Available')}: {formatUsdAmount(props.balanceUSD)}
            </span>
          </div>
          <div className='relative'>
            <span className='text-muted-foreground pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-sm'>
              $
            </span>
            <Input
              id='wallet-transfer-amount'
              type='number'
              min={props.minimumUSD}
              max={props.balanceUSD}
              step='0.01'
              inputMode='decimal'
              value={props.amount}
              className='pl-7 font-mono text-base tabular-nums'
              onChange={(event) => props.onAmountChange(event.target.value)}
            />
          </div>
          <div className='flex items-center justify-between gap-3'>
            <p className='text-muted-foreground text-xs'>
              {t('Minimum {{amount}}', {
                amount: formatUsdAmount(props.minimumUSD),
              })}
            </p>
            <Button
              type='button'
              variant='ghost'
              size='sm'
              onClick={() => props.onAmountChange(props.balanceUSD.toFixed(2))}
            >
              {t('Max')}
            </Button>
          </div>
        </div>
      </div>

      <div className='mt-5 flex flex-col gap-3 border-t pt-4 sm:flex-row sm:items-center sm:justify-between'>
        <p className='text-muted-foreground text-xs leading-5'>
          {t(
            'Recipient IDs and amounts are checked again by the server before the transfer is committed.'
          )}
        </p>
        <Button
          type='button'
          disabled={!props.canContinue}
          onClick={props.onReview}
        >
          <Send className='size-4' />
          {t('Review transfer')}
        </Button>
      </div>
    </section>
  )
}
