import { useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import { resourceOptions } from '../../lib/queries'
import { paymentAmount } from '../../lib/commerce'
import { date } from '../../lib/format'
import { useTranslation } from '../../lib/i18n'
import { Button, Dialog, EmptyState, ErrorMessage, Loading, Pagination } from '../../components/ui'
import {
  correctOrderInvoice,
  downloadCreditNote,
  downloadInvoiceDocument,
  type InvoiceBuyer,
} from './download-invoice'
import { InvoiceBuyerDialog } from './invoice-buyer-dialog'
import './invoice-documents.css'

/** Fetch document history only when requested. Buyer details never enter persistent browser storage. */
export function InvoiceDocuments({ trade, onClose }: { trade: string; onClose: () => void }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const [correcting, setCorrecting] = useState(false)
  const [draft, setDraft] = useState<InvoiceBuyer>()
  const retry = useRef<{ payload: string; id: string } | undefined>(undefined)
  const documents = useQuery(
    resourceOptions(
      'order-invoice-documents',
      (signal) =>
        api
          .GET('/api/commerce/orders/{trade_no}/invoice/documents', {
            signal,
            params: { path: { trade_no: trade }, query: { page, page_size: 20 } },
          })
          .then(unwrap),
      [trade, page],
    ),
  )
  const current = documents.data?.items.find(
    (item) => item.document_type === 'invoice' && item.status === 'current',
  )
  const refresh = async () => {
    await queryClient.invalidateQueries({ queryKey: ['order-invoice-documents', trade] })
  }
  const download = useMutation({
    mutationFn: (number: string) => downloadInvoiceDocument(trade, number),
  })
  const credit = useMutation({ mutationFn: () => downloadCreditNote(trade), onSuccess: refresh })
  const correction = useMutation({
    mutationFn: ({ buyer, reason }: { buyer: InvoiceBuyer; reason: string }) => {
      if (!current) throw new Error('当前发票不可更正，请刷新记录')
      const input = { ...buyer, previous_number: current.number, reason }
      const payload = JSON.stringify(input)
      if (retry.current?.payload !== payload) retry.current = { payload, id: crypto.randomUUID() }
      return correctOrderInvoice(trade, { ...input, request_id: retry.current.id })
    },
    onSuccess: async () => {
      setCorrecting(false)
      retry.current = undefined
      await refresh()
    },
  })
  const pending = correction.isPending || download.isPending || credit.isPending
  return (
    <>
      <Dialog
        open
        size="lg"
        title="发票记录"
        description="原票和历史版本永久保留。退款确认后可下载关联贷项单，不会再次执行退款。"
        onOpenChange={(open) => {
          if (!open && !pending && !correcting) onClose()
        }}
      >
        <code>{trade}</code>
        <ErrorMessage error={documents.error ?? download.error ?? credit.error} />
        {documents.isPending && <Loading />}
        <div className="row-actions invoice-documents-actions">
          <Button
            variant="secondary"
            disabled={pending || !current}
            onClick={() => {
              if (!current) return
              setDraft({
                buyer_name: current.buyer_name,
                buyer_address: current.buyer_address,
                buyer_country: current.buyer_country,
                buyer_tax_id: current.buyer_tax_id,
              })
              correction.reset()
              setCorrecting(true)
            }}
          >
            {t('更正购买方资料')}
          </Button>
          <Button
            variant="secondary"
            disabled={pending || !documents.data?.items.length}
            onClick={() => credit.mutate()}
          >
            {t('下载退款贷项单')}
          </Button>
          <Button
            variant="quiet"
            disabled={pending || documents.isFetching}
            onClick={() => {
              void refresh()
            }}
          >
            {t('刷新记录')}
          </Button>
        </div>
        <p className="muted">{t('贷项单仅根据已确认退款生成；退款中或失败时不可开具。')}</p>
        <div className="invoice-document-list" role="region" aria-label={t('订单单据记录')}>
          {documents.data?.items.map((row) => (
            <article className="invoice-document-record" key={row.number} aria-label={row.number}>
              <div className="invoice-document-heading">
                <code>{row.number}</code>
                <strong>
                  {t(
                    row.document_type === 'credit_note'
                      ? '退款贷项单'
                      : row.status === 'current'
                        ? '当前发票'
                        : '已被更正',
                  )}
                </strong>
              </div>
              <dl className="invoice-document-details">
                <div>
                  <dt>{t('金额')}</dt>
                  <dd className="invoice-document-amount">
                    {paymentAmount(row.amount_minor, row.currency)}
                  </dd>
                </div>
                <div>
                  <dt>{t('开具时间')}</dt>
                  <dd>{date(row.issued_at)}</dd>
                </div>
                {row.related_number && (
                  <div>
                    <dt>{t('关联单据')}</dt>
                    <dd>
                      <code>{row.related_number}</code>
                    </dd>
                  </div>
                )}
                {row.reason && (
                  <div>
                    <dt>{t('更正原因')}</dt>
                    <dd>{row.reason}</dd>
                  </div>
                )}
              </dl>
              <Button
                variant="secondary"
                disabled={pending}
                onClick={() => download.mutate(row.number)}
              >
                {t('下载 PDF')}
              </Button>
            </article>
          ))}
          {documents.data?.items.length === 0 && (
            <EmptyState title="尚未开具发票，请先从账单下载入口填写购买方资料" />
          )}
        </div>
        {documents.data && (
          <Pagination
            page={page}
            pageSize={20}
            total={BigInt(documents.data.total)}
            pending={documents.isFetching || pending}
            onChange={setPage}
          />
        )}
        <div className="row-actions">
          <Button variant="secondary" disabled={pending || correcting} onClick={onClose}>
            {t('关闭')}
          </Button>
        </div>
      </Dialog>
      <InvoiceBuyerDialog
        trade={correcting ? trade : null}
        correction
        draft={draft}
        pending={correction.isPending}
        error={correction.error}
        onClose={() => {
          setCorrecting(false)
          correction.reset()
        }}
        onIssue={(buyer, reason) => {
          setDraft(buyer)
          correction.mutate({ buyer, reason: reason ?? '' })
        }}
      />
    </>
  )
}
