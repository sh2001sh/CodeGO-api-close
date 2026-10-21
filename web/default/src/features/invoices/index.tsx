import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { getIntlLocale } from '@/i18n/format'
import {
  ChevronLeft,
  ChevronRight,
  Download,
  History,
  RefreshCw,
  X,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { COMPANY } from '@/lib/company'
import { formatTimestamp } from '@/lib/format'
import { useDebounce } from '@/hooks/use-debounce'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import '@/features/wallet/commerce.css'
import { getInvoiceEligibleOrdersPage, getInvoiceRequests } from './api'
import {
  canMergeInvoiceOrder,
  invoiceBatchLimit,
  invoiceOrderKey,
  invoiceSelectionTotal,
} from './batch-selection'
import {
  downloadInvoice,
  getInvoiceDocuments,
  InvoiceError,
  type InvoiceBuyer,
  type InvoiceDocument,
  type InvoiceIssueInput,
} from './commercial-api'
import { InvoiceRequestsTable } from './components/invoice-requests-table'
import type { InvoiceEligibleOrder } from './types'

type BuyerDraft = {
  order: InvoiceEligibleOrder
  orders?: InvoiceEligibleOrder[]
  previous?: InvoiceDocument
  requestId?: string
}

export function OrderInvoices() {
  const { t, i18n } = useTranslation()
  const queryClient = useQueryClient()
  const userId = useAuthStore((state) => state.auth.user?.id ?? 0)
  const [orderSearch, setOrderSearch] = useState('')
  const keyword = useDebounce(orderSearch.trim(), 250)
  const [pagination, setPagination] = useState({ scope: '', page: 1 })
  const page = pagination.scope === keyword ? pagination.page : 1
  const orders = useQuery({
    queryKey: ['invoice-eligible-orders', userId, page, keyword],
    queryFn: ({ signal }) =>
      getInvoiceEligibleOrdersPage(page, keyword, signal),
    enabled: userId > 0,
  })
  const historical = useQuery({
    queryKey: ['invoice-historical-requests', userId],
    queryFn: ({ signal }) => getInvoiceRequests(signal),
    enabled: userId > 0,
  })
  const [draft, setDraft] = useState<BuyerDraft | null>(null)
  const [history, setHistory] = useState<InvoiceEligibleOrder | null>(null)
  const pageCount = Math.max(1, Math.ceil((orders.data?.total ?? 0) / 50))
  if (pagination.scope !== keyword) {
    setPagination({ scope: keyword, page: 1 })
  } else if (orders.data && page > pageCount) {
    setPagination({ scope: keyword, page: pageCount })
  }
  const pageOrders = orders.data?.items ?? []
  const [selection, setSelection] = useState<{
    userId: number
    items: InvoiceEligibleOrder[]
  }>({ userId: 0, items: [] })
  const selected = (selection.userId === userId ? selection.items : [])
    .map(
      (item) =>
        pageOrders.find(
          (current) => invoiceOrderKey(current) === invoiceOrderKey(item)
        ) ?? item
    )
    .filter(canMergeInvoiceOrder)
  const selectedKeys = new Set(selected.map(invoiceOrderKey))
  const selectedCurrency = selected[0]?.currency
  const selectionTotal = invoiceSelectionTotal(selected)
  const formatAmount = (amount: number, currency: string) =>
    amount.toLocaleString(getIntlLocale(), {
      minimumFractionDigits: 2,
      maximumFractionDigits: currency === 'USDT' ? 6 : 2,
    })
  const historyRefundStatus =
    history?.invoice_refund_status || history?.refund_status
  const toggleOrder = (order: InvoiceEligibleOrder, checked: boolean) => {
    const items = selected.filter(
      (item) => invoiceOrderKey(item) !== invoiceOrderKey(order)
    )
    if (
      checked &&
      canMergeInvoiceOrder(order) &&
      (!selectedCurrency || selectedCurrency === order.currency) &&
      items.length < invoiceBatchLimit
    )
      items.push(order)
    setSelection({ userId, items })
  }
  const setPage = (value: number) => {
    if (!Number.isFinite(value)) return
    setPagination({
      scope: keyword,
      page: Math.max(1, Math.min(pageCount, Math.trunc(value))),
    })
  }
  const documents = useQuery({
    queryKey: ['order-invoice-documents', userId, history],
    queryFn: ({ signal }) => getInvoiceDocuments(history!, signal),
    enabled: userId > 0 && Boolean(history),
  })
  const download = useMutation({
    mutationFn: (input: {
      order: InvoiceEligibleOrder
      action?: 'document' | 'corrections' | 'credit-note'
      body?: InvoiceIssueInput
      number?: string
      orders?: InvoiceEligibleOrder[]
    }) =>
      downloadInvoice(
        input.order,
        input.action,
        input.body,
        input.number,
        input.orders
      ),
    onSuccess: (_, input) => {
      setDraft(null)
      if (input.orders) setSelection({ userId, items: [] })
      void queryClient.invalidateQueries({
        queryKey: ['order-invoice-documents'],
      })
      void queryClient.invalidateQueries({
        queryKey: ['invoice-eligible-orders'],
      })
    },
    onError: (error, input) => {
      if (
        error instanceof InvoiceError &&
        error.status === 428 &&
        !input.body
      ) {
        setDraft({ order: input.order })
        return
      }
      toast.error(t(error.message))
    },
  })
  return (
    <section
      className='commerce-workspace commerce-invoices space-y-6'
      aria-busy={orders.isFetching}
    >
      <header className='commerce-section-heading'>
        <h2 className='text-2xl font-semibold'>{t('invoice.commercial')}</h2>
        <Button
          variant='outline'
          size='icon'
          aria-label={t('Refresh')}
          onClick={() => void orders.refetch()}
        >
          <RefreshCw size={16} />
        </Button>
      </header>
      <div className='commerce-invoice-issuer flex flex-wrap gap-x-6 gap-y-2'>
        <span>{COMPANY.name}</span>
        <span>
          {i18n.language === 'zh'
            ? COMPANY.simplifiedName
            : COMPANY.traditionalName}
        </span>
        <span>BRN {COMPANY.registrationNumber}</span>
      </div>
      {orders.isError ? (
        <div role='alert' className='commerce-error'>
          {t('invoice.ordersFailed')}{' '}
          <Button variant='outline' onClick={() => void orders.refetch()}>
            {t('Retry')}
          </Button>
        </div>
      ) : null}
      {orders.isLoading ? <div role='status'>{t('Loading')}</div> : null}
      {orders.data?.total === 0 && !keyword ? (
        <div role='status' className='commerce-empty'>
          {t('invoice.noOrders')}
        </div>
      ) : null}
      <div className='flex flex-wrap items-center justify-between gap-4'>
        <Label htmlFor='invoice-order-search' className='sr-only'>
          {t('Search')}
        </Label>
        <Input
          id='invoice-order-search'
          type='search'
          className='w-full sm:max-w-80'
          value={orderSearch}
          onChange={(event) => setOrderSearch(event.target.value)}
          placeholder={t('Search')}
        />
        {pageCount > 1 && (
          <nav
            className='commerce-invoice-pagination flex flex-wrap items-center gap-3'
            aria-label={t('pagination')}
          >
            <span className='text-muted-foreground text-sm'>
              {t('Total')}{' '}
              {(orders.data?.total ?? 0).toLocaleString(getIntlLocale())}
            </span>
            <div className='flex flex-wrap items-center gap-2'>
              <Button
                variant='outline'
                disabled={page === 1 || orders.isFetching}
                onClick={() => setPage(page - 1)}
              >
                <ChevronLeft aria-hidden='true' />
                {t('上一页')}
              </Button>
              <label className='flex items-center gap-2 text-sm'>
                <span className='sr-only'>{t('Page')}</span>
                <Input
                  type='number'
                  className='w-[68px] text-center tabular-nums'
                  min={1}
                  max={pageCount}
                  value={page}
                  aria-label={t('Page')}
                  disabled={orders.isFetching}
                  onChange={(event) => setPage(Number(event.target.value))}
                />
                <span>/ {pageCount.toLocaleString(getIntlLocale())}</span>
              </label>
              <Button
                variant='outline'
                disabled={page === pageCount || orders.isFetching}
                onClick={() => setPage(page + 1)}
              >
                {t('下一页')}
                <ChevronRight aria-hidden='true' />
              </Button>
            </div>
          </nav>
        )}
      </div>
      {orders.data?.total === 0 && Boolean(keyword) && (
        <div role='status' className='commerce-empty'>
          {t('No results found')}
        </div>
      )}
      <div className='bg-muted/25 space-y-3 rounded-xl border p-4'>
        <div className='flex flex-wrap items-center justify-between gap-3'>
          <div className='space-y-1'>
            <h3 className='font-semibold'>{t('invoice.mergeInvoices')}</h3>
            <p className='text-muted-foreground text-sm'>
              {t('invoice.mergeHint')}
            </p>
          </div>
          <Button
            disabled={selected.length < 2 || download.isPending}
            onClick={() => setDraft({ order: selected[0], orders: selected })}
          >
            <Download size={15} aria-hidden='true' />
            {t('invoice.mergeInvoices')}
          </Button>
        </div>
        <div className='flex flex-wrap items-center gap-3'>
          <Button
            variant='outline'
            size='sm'
            disabled={download.isPending || orders.isFetching}
            onClick={() => {
              const items = [...selected]
              let currency = selectedCurrency
              for (const order of pageOrders) {
                if (items.length >= invoiceBatchLimit) break
                if (
                  selectedKeys.has(invoiceOrderKey(order)) ||
                  !canMergeInvoiceOrder(order) ||
                  (currency && currency !== order.currency)
                )
                  continue
                currency = order.currency
                items.push(order)
              }
              setSelection({ userId, items })
            }}
          >
            {t('invoice.selectPage')}
          </Button>
          <Button
            variant='ghost'
            size='sm'
            disabled={selected.length === 0 || download.isPending}
            onClick={() => setSelection({ userId, items: [] })}
          >
            {t('invoice.clearSelection')}
          </Button>
          <span
            role='status'
            aria-live='polite'
            className='text-sm tabular-nums'
          >
            {t('invoice.selectionSummary', {
              count: selected.length,
              amount: formatAmount(selectionTotal, selectedCurrency ?? ''),
              currency: selectedCurrency ?? '',
            })}
          </span>
        </div>
      </div>
      <div className='commerce-invoice-list'>
        {pageOrders.map((order) => (
          <article
            key={`${order.source_type}:${order.trade_no}`}
            className='commerce-invoice-order'
          >
            <div className='commerce-invoice-summary'>
              <Checkbox
                aria-label={t('invoice.selectOrder', { trade: order.trade_no })}
                checked={selectedKeys.has(invoiceOrderKey(order))}
                disabled={
                  download.isPending ||
                  !canMergeInvoiceOrder(order) ||
                  (!selectedKeys.has(invoiceOrderKey(order)) &&
                    (selected.length >= invoiceBatchLimit ||
                      Boolean(
                        selectedCurrency && selectedCurrency !== order.currency
                      )))
                }
                onCheckedChange={(checked) => toggleOrder(order, checked)}
              />
              <div className='commerce-invoice-identity min-w-0 space-y-1'>
                <code className='block'>{order.trade_no}</code>
                <div className='text-muted-foreground text-base'>
                  {t(order.order_title)} · {formatTimestamp(order.paid_at)}
                </div>
                {(order.invoice_order_count ?? 0) > 1 ? (
                  <div className='text-muted-foreground text-sm'>
                    {t('invoice.mergedOrders', {
                      count: order.invoice_order_count,
                    })}{' '}
                    · {order.invoice_number}
                  </div>
                ) : null}
              </div>
              <div className='commerce-invoice-amount'>
                {order.order_amount.toLocaleString(getIntlLocale(), {
                  minimumFractionDigits: 2,
                  maximumFractionDigits: 2,
                })}{' '}
                {order.currency}
              </div>
            </div>
            <div className='commerce-invoice-actions'>
              <Button
                variant='outline'
                disabled={download.isPending}
                onClick={() => download.mutate({ order })}
              >
                <Download size={15} />
                {t('invoice.downloadPdf')}
              </Button>
              <Button
                variant='ghost'
                disabled={download.isPending}
                onClick={() => setHistory(order)}
              >
                <History size={15} />
                {t('invoice.records')}
              </Button>
            </div>
          </article>
        ))}
      </div>
      {historical.isError ? (
        <div role='alert'>
          {t('invoice.recordsFailed')}{' '}
          <Button variant='outline' onClick={() => void historical.refetch()}>
            {t('Retry')}
          </Button>
        </div>
      ) : null}
      {historical.data?.items.length ? (
        <section className='space-y-3'>
          <h3 className='font-semibold'>{t('invoice.legacyRecords')}</h3>
          <InvoiceRequestsTable requests={historical.data.items} />
        </section>
      ) : null}
      <InvoiceBuyerDialog
        key={`${draft?.order.trade_no}:${draft?.orders?.length}:${draft?.previous?.number}`}
        draft={draft}
        pending={download.isPending}
        onClose={() => setDraft(null)}
        onIssue={(body, reason) => {
          if (!draft) return
          download.mutate({
            order: draft.order,
            orders: draft.orders,
            action: draft.previous ? 'corrections' : 'document',
            body: {
              ...body,
              ...(draft.previous
                ? {
                    previous_number: draft.previous.number,
                    reason,
                    request_id: draft.requestId,
                  }
                : {}),
            },
          })
        }}
      />
      <Dialog
        open={Boolean(history)}
        onOpenChange={(open) => {
          if (!open) setHistory(null)
        }}
      >
        <DialogContent
          showCloseButton={false}
          className='commerce-dialog commerce-workspace max-h-[85dvh] overflow-y-auto sm:max-w-xl'
        >
          <DialogHeader>
            <DialogTitle>{t('invoice.records')}</DialogTitle>
          </DialogHeader>
          <Button
            className='absolute top-2 right-2'
            variant='ghost'
            size='icon'
            aria-label={t('Close')}
            onClick={() => setHistory(null)}
          >
            <X size={16} />
          </Button>
          {documents.isLoading ? <div>{t('Loading')}</div> : null}
          {documents.isError ? (
            <div role='alert'>
              {t('invoice.recordsFailed')}{' '}
              <Button
                variant='outline'
                onClick={() => void documents.refetch()}
              >
                {t('Retry')}
              </Button>
            </div>
          ) : null}
          {documents.data?.length === 0 ? (
            <div>{t('invoice.noRecords')}</div>
          ) : null}
          {documents.data?.map((document) => (
            <article
              key={document.number}
              className='commerce-document space-y-3'
            >
              <div className='flex flex-wrap justify-between gap-2'>
                <code>{document.number}</code>
                <span>
                  {t(
                    document.document_type === 'credit_note'
                      ? 'invoice.creditNote'
                      : 'invoice.commercial'
                  )}
                </span>
              </div>
              <div className='text-muted-foreground text-sm'>
                {formatTimestamp(document.issued_at)}
              </div>
              <div className='flex flex-wrap gap-2'>
                <Button
                  variant='outline'
                  disabled={download.isPending}
                  onClick={() =>
                    history &&
                    download.mutate({ order: history, number: document.number })
                  }
                >
                  {t('invoice.downloadPdf')}
                </Button>
                {(!historyRefundStatus || historyRefundStatus === 'failed') &&
                document ===
                  documents.data
                    ?.filter((item) => item.document_type === 'invoice')
                    .at(-1) ? (
                  <Button
                    variant='outline'
                    disabled={download.isPending}
                    onClick={() => {
                      if (history) {
                        setDraft({
                          order: history,
                          previous: document,
                          requestId: crypto.randomUUID(),
                        })
                        setHistory(null)
                      }
                    }}
                  >
                    {t('invoice.correctBuyer')}
                  </Button>
                ) : null}
              </div>
            </article>
          ))}
          {history &&
          historyRefundStatus === 'success' &&
          documents.data?.length ? (
            <Button
              variant='outline'
              disabled={download.isPending}
              onClick={() =>
                download.mutate({ order: history, action: 'credit-note' })
              }
            >
              {t('invoice.refundDocument')}
            </Button>
          ) : null}
        </DialogContent>
      </Dialog>
    </section>
  )
}

function InvoiceBuyerDialog(props: {
  draft: BuyerDraft | null
  pending: boolean
  onClose: () => void
  onIssue: (buyer: InvoiceBuyer, reason?: string) => void
}) {
  const { t } = useTranslation()
  const buyer = props.draft?.previous?.buyer
  let title = 'invoice.issue'
  if (props.draft?.previous) title = 'invoice.correctBuyer'
  else if (props.draft?.orders) title = 'invoice.mergeTitle'
  return (
    <Dialog
      open={Boolean(props.draft)}
      onOpenChange={(open) => {
        if (!open && !props.pending) props.onClose()
      }}
    >
      <DialogContent
        showCloseButton={false}
        className='commerce-dialog commerce-workspace max-h-[85dvh] overflow-y-auto sm:max-w-[720px]'
      >
        <DialogHeader>
          <DialogTitle>{t(title)}</DialogTitle>
        </DialogHeader>
        {props.draft?.orders ? (
          <details className='rounded-lg border p-3 text-sm'>
            <summary className='cursor-pointer font-medium'>
              {t('invoice.selectionSummary', {
                count: props.draft.orders.length,
                amount: invoiceSelectionTotal(
                  props.draft.orders
                ).toLocaleString(getIntlLocale(), {
                  minimumFractionDigits: 2,
                  maximumFractionDigits:
                    props.draft.order.currency === 'USDT' ? 6 : 2,
                }),
                currency: props.draft.order.currency,
              })}
            </summary>
            <ul className='mt-3 max-h-48 space-y-2 overflow-y-auto'>
              {props.draft.orders.map((order) => (
                <li
                  key={invoiceOrderKey(order)}
                  className='flex flex-wrap justify-between gap-x-3 gap-y-1'
                >
                  <code className='min-w-0 break-all'>{order.trade_no}</code>
                  <span className='tabular-nums'>
                    {order.order_amount.toLocaleString(getIntlLocale(), {
                      minimumFractionDigits: 2,
                      maximumFractionDigits: order.currency === 'USDT' ? 6 : 2,
                    })}{' '}
                    {order.currency}
                  </span>
                </li>
              ))}
            </ul>
          </details>
        ) : null}
        <form
          className='commerce-invoice-form'
          onSubmit={(event) => {
            event.preventDefault()
            const data = new FormData(event.currentTarget)
            props.onIssue(
              {
                buyer_name: String(data.get('buyer_name')).trim(),
                buyer_address: String(data.get('buyer_address')).trim(),
                buyer_country: String(data.get('buyer_country')).trim(),
                buyer_tax_id: String(data.get('buyer_tax_id')).trim(),
              },
              String(data.get('reason') ?? '').trim()
            )
          }}
        >
          <div className='space-y-2'>
            <Label htmlFor='invoice-buyer-name'>{t('invoice.buyerName')}</Label>
            <Input
              id='invoice-buyer-name'
              name='buyer_name'
              required
              maxLength={200}
              defaultValue={buyer?.buyer_name}
              disabled={props.pending}
              autoComplete='organization'
            />
          </div>
          <div className='space-y-2'>
            <Label htmlFor='invoice-buyer-address'>
              {t('invoice.buyerAddress')}
            </Label>
            <Textarea
              id='invoice-buyer-address'
              name='buyer_address'
              required
              maxLength={600}
              rows={4}
              defaultValue={buyer?.buyer_address}
              disabled={props.pending}
              autoComplete='street-address'
            />
          </div>
          <div className='space-y-2'>
            <Label htmlFor='invoice-buyer-country'>
              {t('invoice.buyerCountry')}
            </Label>
            <Input
              id='invoice-buyer-country'
              name='buyer_country'
              maxLength={100}
              defaultValue={buyer?.buyer_country}
              disabled={props.pending}
              autoComplete='country-name'
            />
          </div>
          <div className='space-y-2'>
            <Label htmlFor='invoice-buyer-tax'>{t('invoice.buyerTax')}</Label>
            <Input
              id='invoice-buyer-tax'
              name='buyer_tax_id'
              maxLength={64}
              defaultValue={buyer?.buyer_tax_id}
              disabled={props.pending}
            />
          </div>
          {props.draft?.previous ? (
            <div className='space-y-2'>
              <Label htmlFor='invoice-reason'>{t('invoice.reason')}</Label>
              <Textarea
                id='invoice-reason'
                name='reason'
                required
                maxLength={300}
                disabled={props.pending}
              />
            </div>
          ) : null}
          <div className='commerce-invoice-form-actions'>
            <Button
              type='button'
              variant='outline'
              disabled={props.pending}
              onClick={props.onClose}
            >
              {t('Cancel')}
            </Button>
            <Button type='submit' disabled={props.pending}>
              {t(props.pending ? 'invoice.issuing' : 'invoice.issueDownload')}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}
