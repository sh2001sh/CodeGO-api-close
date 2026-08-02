import { useEffect, useState } from 'react'
import { ArrowRightLeft, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { getWalletQuotaConversions, isApiSuccess } from '../api'
import type { WalletQuotaConversionOverview } from '../types'
import { ConversionHistory } from './wallet-quota-conversion-parts'

export function WalletQuotaConversionHistorySheet(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const [overview, setOverview] =
    useState<WalletQuotaConversionOverview | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!props.open) return
    let active = true
    setLoading(true)
    setError('')
    void getWalletQuotaConversions()
      .then((response) => {
        if (!active) return
        if (!isApiSuccess(response) || !response.data) {
          throw new Error(
            response.message || t('Failed to load conversion data.')
          )
        }
        setOverview(response.data)
      })
      .catch((reason: unknown) => {
        if (!active) return
        setError(
          reason instanceof Error
            ? reason.message
            : t('Failed to load conversion data.')
        )
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [props.open, t])

  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent className='w-[calc(100vw-1rem)] sm:max-w-lg'>
        <SheetHeader className='border-b px-5 py-4 pr-14'>
          <SheetTitle className='flex items-center gap-2'>
            <ArrowRightLeft className='text-primary size-5' />
            {t('Conversion records')}
          </SheetTitle>
          <SheetDescription>
            {t('Review recent transfers between both wallet balances.')}
          </SheetDescription>
        </SheetHeader>
        <div className='min-h-0 flex-1 overflow-y-auto px-5 py-4'>
          {loading ? (
            <div className='text-muted-foreground flex min-h-48 items-center justify-center gap-2 text-sm'>
              <Loader2 className='size-4 animate-spin' />
              {t('Loading conversion records...')}
            </div>
          ) : error ? (
            <div className='border-destructive/30 bg-destructive/5 text-destructive rounded-lg border px-4 py-5 text-center text-sm'>
              {error}
            </div>
          ) : !overview?.recent_conversions.length ? (
            <div className='border-border text-muted-foreground rounded-lg border border-dashed px-4 py-10 text-center text-sm'>
              {t('No wallet conversion records yet.')}
            </div>
          ) : (
            <ConversionHistory overview={overview} standalone />
          )}
        </div>
        <div className='border-t px-5 py-3'>
          <Button
            type='button'
            variant='outline'
            className='w-full'
            onClick={() => props.onOpenChange(false)}
          >
            {t('Close')}
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  )
}
