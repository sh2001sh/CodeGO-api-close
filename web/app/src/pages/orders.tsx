import { useState } from 'react'
import { useMutation, useSuspenseQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import { resourceOptions, sessionOptions } from '../lib/queries'
import { credits, date } from '../lib/format'
import { followPayment, paymentAmount, type Order } from '../lib/commerce'
import { useTranslation } from '../lib/i18n'
import { DataTable } from '../components/data-table'
import { Button, ErrorMessage, PageHeader, Status } from '../components/ui'
import { ReferralOrderTerms } from '../features/commerce/referral-order-terms'

export default function OrdersPage() {
  const { t } = useTranslation()
  const user = useSuspenseQuery(sessionOptions()).data
  const [before, setBefore] = useState('')
  const [all, setAll] = useState(false)
  const queryClient = useQueryClient()
  const admin = user.role === 'admin' || user.role === 'root'
  const path = all ? '/api/commerce/admin/orders' : '/api/commerce/orders'
  const { data } = useSuspenseQuery(
    resourceOptions(
      'orders',
      (signal) =>
        api
          .GET(path, { signal, params: { query: { limit: 50, before: before || undefined } } })
          .then((result) => unwrap(result)),
      [all, before],
    ),
  )
  const [localError, setLocalError] = useState<Error | null>(null)
  const cancel = useMutation({
    mutationFn: (trade: string) =>
      api.POST('/api/commerce/orders/{trade_no}/cancel', { params: { path: { trade_no: trade } } }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['orders'] }),
  })
  const pay = (order: Order) => {
    try {
      followPayment(order.payment_url)
    } catch (error) {
      setLocalError(error as Error)
    }
  }
  return (
    <>
      <PageHeader
        title="订单"
        action={
          <Button
            variant="quiet"
            onClick={() => {
              setBefore('')
              void queryClient.invalidateQueries({ queryKey: ['orders'] })
            }}
          >
            {t('刷新')}
          </Button>
        }
      />
      {admin && (
        <div className="filters">
          <Button
            variant="quiet"
            onClick={() => {
              setAll(!all)
              setBefore('')
            }}
          >
            {t(all ? '我的订单' : '全部订单')}
          </Button>
        </div>
      )}
      <ErrorMessage error={cancel.error ?? localError} />
      <DataTable
        rows={data ?? []}
        rowKey={(row) => row.id}
        columns={[
          { label: '订单号', render: (row) => <code>{row.trade_no}</code> },
          { label: '状态', render: (row) => <Status value={row.state} /> },
          {
            label: '支付金额',
            render: (row) => paymentAmount(row.amount_minor, row.currency),
            numeric: true,
          },
          { label: '额度', render: (row) => credits(row.credits), numeric: true },
          { label: '创建时间', render: (row) => date(row.created_at) },
          { label: '订单邀请条款', render: (row) => <ReferralOrderTerms order={row} /> },
          {
            label: '操作',
            render: (row) =>
              row.state === 'created' || row.state === 'pending' ? (
                <div className="row-actions">
                  <Button variant="quiet" onClick={() => pay(row)}>
                    {t('支付')}
                  </Button>
                  <Button
                    variant="quiet"
                    disabled={cancel.isPending}
                    onClick={() => cancel.mutate(row.trade_no)}
                  >
                    {t('取消')}
                  </Button>
                </div>
              ) : (
                '—'
              ),
          },
        ]}
      />
      <div className="filters section">
        <Button variant="quiet" disabled={!before} onClick={() => setBefore('')}>
          {t('返回最新')}
        </Button>
        <Button
          variant="quiet"
          disabled={(data?.length ?? 0) < 50}
          onClick={() => setBefore(String(data.at(-1)?.id ?? ''))}
        >
          {t('下一页')}
        </Button>
      </div>
    </>
  )
}
