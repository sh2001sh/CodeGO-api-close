import { useTranslation } from '../lib/i18n'
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import type { Schema } from '../lib/types'
import { resourceOptions, sessionOptions } from '../lib/queries'
import { date } from '../lib/format'
import { DataTable } from '../components/data-table'
import { Button, ErrorMessage, Loading, PageHeader, Status } from '../components/ui'
import { InvoiceForm, InvoiceReview } from '../features/commerce/invoice-form'
import { invoicePayment } from '../features/commerce/amounts'

export const eligibleInvoicesOptions = () =>
  resourceOptions('invoice-eligible', (signal) =>
    api.GET('/api/invoices/eligible-orders', { signal }).then((result) => unwrap(result)),
  )
export const invoicesOptions = (page = 1, all = false, status = '') =>
  resourceOptions(
    'invoices',
    (signal) =>
      api
        .GET(all ? '/api/invoices/admin/requests' : '/api/invoices/requests', {
          signal,
          params: { query: { p: page, page_size: 20, status } },
        })
        .then((result) => unwrap(result)),
    [page, all, status],
  )

export default function InvoicesPage() {
  const { t } = useTranslation()
  const user = useSuspenseQuery(sessionOptions()).data
  const eligible = useSuspenseQuery(eligibleInvoicesOptions()).data
  useSuspenseQuery(invoicesOptions())
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const [all, setAll] = useState(false)
  const [status, setStatus] = useState('')
  const [editing, setEditing] = useState<Schema['InvoiceRequest'] | null>(null)
  const [submitted, setSubmitted] = useState(0)
  const list = useQuery(invoicesOptions(page, all, status))
  const admin = user.role === 'admin' || user.role === 'root'
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['invoices'] })
    void queryClient.invalidateQueries({ queryKey: ['invoice-eligible'] })
  }
  const create = useMutation({
    mutationFn: (body: Schema['CreateInvoiceRequestInput']) =>
      api.POST('/api/invoices/requests', { body }).then((result) => unwrap(result)),
    onSuccess: () => {
      setSubmitted((value) => value + 1)
      refresh()
    },
  })
  const review = useMutation({
    mutationFn: ({
      id,
      body,
    }: {
      id: Schema['InvoiceRequest']['id']
      body: Schema['UpdateInvoiceRequestInput']
    }) =>
      api
        .PUT('/api/invoices/admin/requests/{id}', { params: { path: { id } }, body })
        .then((result) => unwrap(result)),
    onSuccess: () => {
      setEditing(null)
      refresh()
    },
  })
  return (
    <>
      <PageHeader
        title="发票"
        action={
          <Button
            variant="quiet"
            disabled={list.isFetching}
            onClick={() => {
              refresh()
              void list.refetch()
            }}
          >
            {t('刷新')}
          </Button>
        }
      />
      <ErrorMessage error={create.error ?? review.error} />
      {create.isSuccess && <p role="status">{t('发票申请已提交。')}</p>}
      {!all && (
        <InvoiceForm
          key={submitted}
          orders={eligible ?? []}
          pending={create.isPending}
          onSave={(body) => create.mutate(body)}
        />
      )}
      <section className="section">
        <h2>{all ? t('全部发票申请') : t('我的发票申请')}</h2>
        <div className="filters">
          {admin && (
            <Button
              variant="quiet"
              onClick={() => {
                setAll(!all)
                setPage(1)
                setEditing(null)
              }}
            >
              {all ? t('我的申请') : t('管理全部申请')}
            </Button>
          )}
          <label className="field" htmlFor="invoice-filter">
            <span>{t('状态')}</span>
            <select
              id="invoice-filter"
              value={status}
              onChange={(event) => {
                setStatus(event.target.value)
                setPage(1)
              }}
            >
              <option value="">{t('全部')}</option>
              <option value="pending">{t('待处理')}</option>
              <option value="issued">{t('已开票')}</option>
              <option value="rejected">{t('已拒绝')}</option>
            </select>
          </label>
        </div>
        {editing && (
          <InvoiceReview
            key={String(editing.id)}
            invoice={editing}
            pending={review.isPending}
            onCancel={() => setEditing(null)}
            onSave={(body) => review.mutate({ id: editing.id, body })}
          />
        )}
        <ErrorMessage error={list.error} />
        {list.isFetching && <Loading />}
        <DataTable
          rows={list.data?.items ?? []}
          rowKey={(row) => row.id}
          empty="暂无发票申请。选择已支付订单即可申请。"
          columns={[
            { label: '申请编号', render: (row) => String(row.id) },
            {
              label: '抬头',
              render: (row) => (
                <>
                  <strong>{row.title}</strong>
                  <div className="muted">{row.email}</div>
                  {row.tax_number && <div className="muted">{row.tax_number}</div>}
                </>
              ),
            },
            {
              label: '订单',
              render: (row) =>
                String(row.order_title) + '（' + String(row.order_count) + t(' 笔）'),
            },
            {
              label: '金额',
              render: (row) => invoicePayment(row.order_amount_minor, row.currency),
              numeric: true,
            },
            {
              label: '状态',
              render: (row) => (
                <Status
                  value={
                    row.status === 'issued'
                      ? '已开票'
                      : row.status === 'rejected'
                        ? '已拒绝'
                        : row.status === 'pending'
                          ? '待处理'
                          : row.status
                  }
                />
              ),
            },
            { label: '发票号码', render: (row) => row.invoice_number || '—' },
            { label: '处理备注', render: (row) => row.admin_note || '—' },
            { label: '申请时间', render: (row) => date(Number(row.created_at) * 1000) },
            ...(all
              ? [
                  {
                    label: '操作',
                    render: (row: Schema['InvoiceRequest']) =>
                      row.status === 'pending' ? (
                        <Button
                          variant="quiet"
                          onClick={() => {
                            review.reset()
                            setEditing(row)
                          }}
                        >
                          {t('处理')}
                        </Button>
                      ) : (
                        '—'
                      ),
                  },
                ]
              : []),
          ]}
        />
        <nav className="pagination" aria-label={t('发票分页')}>
          <Button
            variant="quiet"
            disabled={page <= 1 || list.isFetching}
            onClick={() => setPage(page - 1)}
          >
            {t('上一页')}
          </Button>
          <span>
            {t('第')} {page} {t('页')}
          </span>
          <Button
            variant="quiet"
            disabled={!list.data || BigInt(page * 20) >= BigInt(list.data.total) || list.isFetching}
            onClick={() => setPage(page + 1)}
          >
            {t('下一页')}
          </Button>
        </nav>
      </section>
    </>
  )
}
