import { useEffect, useState } from 'react'
import { Loader2, Send, ShieldCheck } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import type { WalletTransferRecipient } from '../types'

export function WalletTransferConfirmDialog(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
  recipient: WalletTransferRecipient | null
  amountLabel: string
  feeLabel: string
  totalDebitLabel: string
  submitting: boolean
  retrying?: boolean
  error?: string
  onConfirm: (paymentPassword: string) => Promise<void>
}) {
  const { t } = useTranslation()
  const [paymentPassword, setPaymentPassword] = useState('')

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    if (props.open) setPaymentPassword('')
  }, [props.open])

  return (
    <Dialog
      open={props.open}
      onOpenChange={(open) => {
        if (!props.submitting) props.onOpenChange(open)
      }}
    >
      <DialogContent className='commerce-dialog max-h-[calc(100dvh-1.5rem)] overflow-y-auto max-sm:w-[calc(100vw-1.5rem)] sm:max-w-md'>
        <DialogHeader>
          <DialogTitle className='flex items-center gap-2'>
            <Send className='text-primary size-5' />
            {t('Confirm quota transfer')}
          </DialogTitle>
          <DialogDescription>
            {t('Transfers are credited immediately and cannot be reversed.')}
          </DialogDescription>
        </DialogHeader>

        <div className='space-y-4 py-2'>
          <dl className='commerce-transfer-review'>
            <dt className='text-muted-foreground'>{t('Recipient')}</dt>
            <dd className='text-right font-medium'>
              {props.recipient?.display_name_masked} ·{' '}
              {props.recipient?.external_id}
            </dd>
            <dt className='text-muted-foreground'>{t('Transfer amount')}</dt>
            <dd className='text-right font-semibold tabular-nums'>
              {props.amountLabel}
            </dd>
            <dt className='text-muted-foreground'>{t('手续费（1%）')}</dt>
            <dd className='text-right font-medium tabular-nums'>
              {props.feeLabel}
            </dd>
            <dt className='text-muted-foreground'>{t('通用额度总扣除')}</dt>
            <dd className='text-right font-semibold tabular-nums'>
              {props.totalDebitLabel}
            </dd>
          </dl>

          <div className='space-y-2'>
            <Label htmlFor='transfer-payment-password' className='flex gap-2'>
              <ShieldCheck className='size-4' />
              {t('Payment password')}
            </Label>
            <Input
              id='transfer-payment-password'
              type='password'
              value={paymentPassword}
              disabled={props.submitting}
              autoComplete='off'
              maxLength={64}
              autoFocus
              onChange={(event) => setPaymentPassword(event.target.value)}
            />
          </div>
        </div>

        {props.error ? (
          <p role='alert' className='text-destructive text-sm'>
            {props.error}
          </p>
        ) : null}

        <DialogFooter>
          <Button
            type='button'
            variant='outline'
            disabled={props.submitting}
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='button'
            className='commerce-transfer-submit'
            disabled={props.submitting || !paymentPassword}
            onClick={() => {
              const password = paymentPassword
              setPaymentPassword('')
              void props.onConfirm(password)
            }}
          >
            {props.submitting ? (
              <Loader2 className='size-4 animate-spin' />
            ) : (
              <Send className='size-4' />
            )}
            {props.retrying ? t('Retry') : t('Transfer now')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
