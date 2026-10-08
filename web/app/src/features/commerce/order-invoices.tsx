import { useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { Download } from 'lucide-react'
import { api, APIError, unwrap } from '../../lib/api'
import { resourceOptions } from '../../lib/queries'
import { paymentAmount } from '../../lib/commerce'
import { date } from '../../lib/format'
import { useTranslation } from '../../lib/i18n'
import { DataTable } from '../../components/data-table'
import { Button, ErrorMessage, Loading, Pagination, Status } from '../../components/ui'
import { downloadOrderInvoice, type InvoiceBuyer } from './download-invoice'
import { InvoiceBuyerDialog } from './invoice-buyer-dialog'

function HistoricalInvoiceRequests() {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const history = useQuery(
    resourceOptions(
      'invoice-history',
      (signal) =>
        api
          .GET('/api/invoices/requests', { signal, params: { query: { p: page, page_size: 20 } } })
          .then(unwrap),
      [page],
    ),
  )
  if (history.isPending) return <Loading />
  if (history.error) return <ErrorMessage error={history.error} />
  if (!history.data?.items?.length) return null
  return (
    <details className="section">
      <summary>{t('历史发票申请')}</summary>
      <DataTable
        caption="历史发票申请"
        rows={history.data.items}
        rowKey={(row) => row.id}
        columns={[
          { label: '抬头', render: (row) => row.title },
          { label: '订单', render: (row) => row.order_title },
          {
            label: '金额',
            render: (row) => paymentAmount(row.order_amount_minor, row.currency),
            numeric: true,
          },
          {
            label: '状态',
            render: (row) => <Status value={row.status === 'pending' ? '待处理' : row.status} />,
          },
          { label: '发票号码', render: (row) => row.invoice_number || '—' },
          { label: '处理备注', render: (row) => row.admin_note || '—', hideOnMobile: true },
        ]}
      />
      <Pagination
        page={page}
        pageSize={20}
        total={BigInt(history.data.total)}
        pending={history.isFetching}
        onChange={setPage}
      />
    </details>
  )
}

export function OrderInvoices() {
  const { t } = useTranslation()
  const [cursors, setCursors] = useState<string[]>([])
  const [issueTrade, setIssueTrade] = useState<string | null>(null)
  const [draft, setDraft] = useState<InvoiceBuyer | undefined>()
  const list = useQuery(
    resourceOptions(
      'invoice-orders',
      (signal) =>
        api
          .GET('/api/commerce/orders', {
            signal,
            params: { query: { limit: 50, before: cursors.at(-1) || undefined } },
          })
          .then(unwrap),
      [cursors.at(-1) ?? ''],
    ),
  )
  const download = useMutation({
    mutationFn: ({ trade, buyer }: { trade: string; buyer?: InvoiceBuyer }) =>
      downloadOrderInvoice(trade, buyer),
    onError: (error, input) => {
      if (error instanceof APIError && error.status === 428 && !input.buyer) {
        setIssueTrade(input.trade)
      }
    },
    onSuccess: () => setIssueTrade(null),
  })
  const orders = list.data ?? []
  return (
    <>
      <div className="section">
        <h2>{t('商业发票')}</h2>
        <p className="muted">
          CodeGo AI Limited · {t('码高智能有限公司')} · {t('香港')}
        </p>
        <p className="muted">
          {t('已支付订单可自助开具商业发票。未支付、退款中或已退款的订单不可开具或下载。')}
        </p>
        <p className="muted">{t('境外报销要求可能不同，请向报销机构确认。')}</p>
      </div>
      <ErrorMessage
        error={
          list.error ??
          (issueTrade || (download.error instanceof APIError && download.error.status === 428)
            ? null
            : download.error)
        }
      />
      {list.isFetching && <Loading />}
      <DataTable
        caption="已支付订单发票"
        rows={orders.filter((order) => order.state === 'paid' && BigInt(order.amount_minor) > 0n)}
        rowKey={(row) => row.id}
        empty="本页暂无已支付订单"
        columns={[
          { label: '订单号', render: (row) => <code>{row.trade_no}</code> },
          {
            label: '订单',
            render: (row) =>
              t(
                row.kind === 'subscription'
                  ? '套餐购买'
                  : row.kind === 'fuel'
                    ? '套餐加量'
                    : '余额充值',
              ),
          },
          {
            label: '支付金额',
            render: (row) => paymentAmount(row.amount_minor, row.currency),
            numeric: true,
          },
          {
            label: '支付时间',
            render: (row) => (row.paid_at ? date(row.paid_at) : '—'),
            hideOnMobile: true,
          },
          {
            label: '发票',
            render: (row) => (
              <Button
                variant="quiet"
                disabled={download.isPending}
                onClick={() => download.mutate({ trade: row.trade_no })}
              >
                <Download size={15} aria-hidden />
                {t(
                  download.isPending && download.variables?.trade === row.trade_no
                    ? '下载中…'
                    : '下载 PDF',
                )}
              </Button>
            ),
          },
        ]}
      />
      <div className="filters section">
        <Button
          variant="quiet"
          disabled={!cursors.length || list.isFetching}
          onClick={() => setCursors(cursors.slice(0, -1))}
        >
          {t('上一页')}
        </Button>
        <Button
          variant="quiet"
          disabled={orders.length < 50 || list.isFetching}
          onClick={() => setCursors([...cursors, String(orders.at(-1)?.id ?? '')])}
        >
          {t('下一页')}
        </Button>
      </div>
      <HistoricalInvoiceRequests />
      <InvoiceBuyerDialog
        trade={issueTrade}
        draft={draft}
        pending={download.isPending}
        error={download.variables?.buyer ? download.error : null}
        onClose={() => {
          setIssueTrade(null)
          download.reset()
        }}
        onIssue={(buyer) => {
          if (!issueTrade) return
          setDraft(buyer)
          download.mutate({ trade: issueTrade, buyer })
        }}
      />
    </>
  )
}
