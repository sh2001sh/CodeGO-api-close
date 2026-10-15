/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useState } from 'react'
import { Check, Copy, ExternalLink } from 'lucide-react'
import { QRCodeCanvas } from 'qrcode.react'
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
import type { NowPaymentsPaymentResponse } from '../../types'

interface NowPaymentsPaymentDialogProps {
  payment: NowPaymentsPaymentResponse['data']
  onClose: () => void
}

export function NowPaymentsPaymentDialog({
  payment,
  onClose,
}: NowPaymentsPaymentDialogProps) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState<'amount' | 'address' | null>(null)

  if (!payment) return null

  const currency = (payment.pay_currency || 'USDT').toUpperCase()
  const network = getNetworkName(payment.pay_currency || 'USDT')

  const copy = async (value: string, field: 'amount' | 'address') => {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(field)
      window.setTimeout(() => setCopied(null), 1500)
    } catch {
      // Clipboard access can be blocked by browser permissions; the address remains selectable.
    }
  }

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('Pay with USDT')}</DialogTitle>
          <DialogDescription>
            {t('Scan the QR code or copy the address to complete payment')}
          </DialogDescription>
        </DialogHeader>

        <div className='space-y-4'>
          <div className='mx-auto w-fit rounded-xl border bg-white p-3'>
            <QRCodeCanvas value={payment.pay_address} size={196} />
          </div>

          <div className='bg-muted/30 rounded-lg border p-3'>
            <div className='flex items-center justify-between gap-3'>
              <div>
                <p className='text-muted-foreground text-xs'>
                  {t('Payment amount')}
                </p>
                <p className='mt-1 text-lg font-semibold'>
                  {payment.pay_amount} {currency}
                </p>
              </div>
              <Button
                variant='outline'
                size='icon-sm'
                aria-label={t('Copy payment amount')}
                onClick={() => void copy(payment.pay_amount, 'amount')}
              >
                {copied === 'amount' ? <Check /> : <Copy />}
              </Button>
            </div>
          </div>

          <div className='bg-muted/30 rounded-lg border p-3'>
            <div className='flex items-start justify-between gap-3'>
              <div className='min-w-0'>
                <p className='text-muted-foreground text-xs'>
                  {t('Receiving address')} · {network}
                </p>
                <p className='mt-1 font-mono text-xs leading-5 break-all'>
                  {payment.pay_address}
                </p>
              </div>
              <Button
                variant='outline'
                size='icon-sm'
                className='shrink-0'
                aria-label={t('Copy receiving address')}
                onClick={() => void copy(payment.pay_address, 'address')}
              >
                {copied === 'address' ? <Check /> : <Copy />}
              </Button>
            </div>
          </div>

          <p className='text-muted-foreground text-xs leading-5'>
            {t(
              'Send only {{currency}} through the {{network}} network. Using another asset or network may result in permanent loss.',
              { currency, network }
            )}
          </p>
        </div>

        <DialogFooter>
          <Button variant='outline' onClick={onClose}>
            {t('Close')}
          </Button>
          {payment.pay_url ? (
            <Button
              onClick={() =>
                window.open(payment.pay_url, '_blank', 'noopener,noreferrer')
              }
            >
              <ExternalLink />
              {t('Open payment page')}
            </Button>
          ) : null}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function getNetworkName(payCurrency: string) {
  const normalized = payCurrency.toLowerCase()
  if (normalized.includes('trc20')) return 'TRON（TRC20）'
  if (normalized.includes('erc20')) return 'Ethereum（ERC20）'
  if (normalized.includes('bsc')) return 'BNB Smart Chain（BEP20）'
  return payCurrency.toUpperCase()
}
