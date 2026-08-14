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
  submitting: boolean
  onConfirm: (paymentPassword: string) => Promise<void>
}) {
  const { t } = useTranslation()
  const [paymentPassword, setPaymentPassword] = useState('')

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    if (props.open) setPaymentPassword('')
  }, [props.open])

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='max-h-[calc(100dvh-1.5rem)] overflow-y-auto max-sm:w-[calc(100vw-1.5rem)] sm:max-w-md'>
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
          <div className='border-border bg-muted/35 grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 rounded-md border p-3 text-sm'>
            <span className='text-muted-foreground'>{t('Recipient')}</span>
            <strong className='text-right font-medium'>
              {props.recipient?.display_name_masked} ·{' '}
              {props.recipient?.external_id}
            </strong>
            <span className='text-muted-foreground'>
              {t('Transfer amount')}
            </span>
            <strong className='text-right font-semibold tabular-nums'>
              {props.amountLabel}
            </strong>
          </div>

          <div className='space-y-2'>
            <Label htmlFor='transfer-payment-password' className='flex gap-2'>
              <ShieldCheck className='size-4' />
              {t('Payment password')}
            </Label>
            <Input
              id='transfer-payment-password'
              type='password'
              value={paymentPassword}
              autoComplete='off'
              maxLength={64}
              autoFocus
              onChange={(event) => setPaymentPassword(event.target.value)}
            />
          </div>
        </div>

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
            disabled={props.submitting || !paymentPassword}
            onClick={() => void props.onConfirm(paymentPassword)}
          >
            {props.submitting ? (
              <Loader2 className='size-4 animate-spin' />
            ) : (
              <Send className='size-4' />
            )}
            {t('Transfer now')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
