import { useCallback, useEffect, useState } from 'react'
import {
  ArrowDownLeft,
  ArrowUpRight,
  ChevronLeft,
  ChevronRight,
  Loader2,
  Send,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatQuota } from '@/lib/format'
import { Button } from '@/components/ui/button'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { getWalletTransfers, isApiSuccess } from '../api'
import type { WalletTransferHistoryPage } from '../types'

const PAGE_SIZE = 10

export function WalletTransferHistorySheet(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const [history, setHistory] = useState<WalletTransferHistoryPage | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const loadHistory = useCallback(
    async (targetPage: number) => {
      setLoading(true)
      setError('')
      try {
        const response = await getWalletTransfers(targetPage, PAGE_SIZE)
        if (!isApiSuccess(response) || !response.data) {
          throw new Error(
            response.message || t('Failed to load transfer records.')
          )
        }
        setHistory(response.data.history)
        setPage(targetPage)
      } catch (reason) {
        setError(
          reason instanceof Error
            ? reason.message
            : t('Failed to load transfer records.')
        )
      } finally {
        setLoading(false)
      }
    },
    [t]
  )

  useEffect(() => {
    if (!props.open) return
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void loadHistory(1)
  }, [loadHistory, props.open])

  const pageCount = Math.max(1, Math.ceil((history?.total || 0) / PAGE_SIZE))

  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent className='w-[calc(100vw-1rem)] sm:max-w-xl'>
        <SheetHeader className='border-b px-5 py-4 pr-14'>
          <SheetTitle className='flex items-center gap-2'>
            <Send className='text-primary size-5' />
            {t('Transfer records')}
          </SheetTitle>
          <SheetDescription>
            {t('Review incoming and outgoing standard-balance transfers.')}
          </SheetDescription>
        </SheetHeader>

        <div className='min-h-0 flex-1 overflow-y-auto px-5 py-4'>
          {loading ? (
            <div className='text-muted-foreground flex min-h-48 items-center justify-center gap-2 text-sm'>
              <Loader2 className='size-4 animate-spin' />
              {t('Loading transfer records...')}
            </div>
          ) : error ? (
            <div className='border-destructive/30 bg-destructive/5 text-destructive rounded-md border px-4 py-5 text-center text-sm'>
              {error}
            </div>
          ) : !history?.items.length ? (
            <div className='border-border text-muted-foreground rounded-md border border-dashed px-4 py-12 text-center text-sm'>
              {t('No quota transfer records yet.')}
            </div>
          ) : (
            <div className='divide-border divide-y'>
              {history.items.map((item) => {
                const incoming = item.direction === 'incoming'
                return (
                  <div
                    key={item.id}
                    className='flex items-start gap-3 py-4 first:pt-0 last:pb-0'
                  >
                    <div
                      className={
                        incoming
                          ? 'bg-success/10 text-success flex size-9 shrink-0 items-center justify-center rounded-md'
                          : 'bg-muted text-muted-foreground flex size-9 shrink-0 items-center justify-center rounded-md'
                      }
                    >
                      {incoming ? (
                        <ArrowDownLeft className='size-4' />
                      ) : (
                        <ArrowUpRight className='size-4' />
                      )}
                    </div>
                    <div className='min-w-0 flex-1'>
                      <div className='flex items-start justify-between gap-3'>
                        <div className='min-w-0'>
                          <p className='truncate text-sm font-medium'>
                            {incoming ? t('Received from') : t('Sent to')}{' '}
                            {item.counterparty_display_name_masked}
                          </p>
                          <p className='text-muted-foreground mt-0.5 font-mono text-xs tracking-wider'>
                            {item.counterparty_external_id}
                          </p>
                        </div>
                        <p
                          className={
                            incoming
                              ? 'text-success shrink-0 font-semibold tabular-nums'
                              : 'text-foreground shrink-0 font-semibold tabular-nums'
                          }
                        >
                          {incoming ? '+' : '-'}
                          {formatQuota(item.amount_quota)}
                        </p>
                      </div>
                      <div className='text-muted-foreground mt-2 flex flex-wrap justify-between gap-2 text-xs'>
                        <span>
                          {new Date(item.created_at * 1000).toLocaleString()}
                        </span>
                        <span>
                          {t('Balance after')} {formatQuota(item.balance_after)}
                        </span>
                      </div>
                    </div>
                  </div>
                )
              })}
            </div>
          )}
        </div>

        <div className='border-t px-5 py-3'>
          <div className='flex items-center justify-between gap-3'>
            <span className='text-muted-foreground text-xs'>
              {t('Page {{page}} of {{count}}', { page, count: pageCount })}
            </span>
            <div className='flex gap-2'>
              <Button
                type='button'
                variant='outline'
                size='icon'
                disabled={loading || page <= 1}
                aria-label={t('Previous page')}
                title={t('Previous page')}
                onClick={() => void loadHistory(page - 1)}
              >
                <ChevronLeft className='size-4' />
              </Button>
              <Button
                type='button'
                variant='outline'
                size='icon'
                disabled={loading || page >= pageCount}
                aria-label={t('Next page')}
                title={t('Next page')}
                onClick={() => void loadHistory(page + 1)}
              >
                <ChevronRight className='size-4' />
              </Button>
            </div>
          </div>
        </div>
      </SheetContent>
    </Sheet>
  )
}
