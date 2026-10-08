import { useState } from 'react'
import { useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import { resourceOptions } from '../lib/queries'
import { useTranslation } from '../lib/i18n'
import { date } from '../lib/format'
import { paymentAmount } from '../lib/commerce'
import type { Schema } from '../lib/types'
import { DataTable } from '../components/data-table'
import { Button, Drawer, PageHeader, Status, Tabs } from '../components/ui'
import { OrderDetail } from '../features/commerce-admin/order-detail'
import { PackagePaymentReviews } from '../features/commerce-admin/package-payment-reviews'
import { useNotificationTranslation } from '../features/notifications/messages'

type Order = Schema['Order']

const ordersOptions = (before: string) =>
  resourceOptions(
    'admin-orders',
    (signal) =>
      api
        .GET('/api/commerce/admin/orders', {
          signal,
          params: { query: { limit: 50, before: before || undefined } },
        })
        .then((result) => unwrap(result)),
    [before],
  )

function OrderList() {
  const { t } = useTranslation()
  const [before, setBefore] = useState('')
  const [selected, setSelected] = useState<Order | null>(null)
  const queryClient = useQueryClient()
  const { data } = useSuspenseQuery(ordersOptions(before))
  return (
    <>
      <div className="row-actions">
        <Button
          variant="quiet"
          onClick={() => void queryClient.invalidateQueries({ queryKey: ['admin-orders'] })}
        >
          {t('刷新')}
        </Button>
      </div>
      <DataTable
        rows={data ?? []}
        rowKey={(row) => row.id}
        onRowClick={(row) => setSelected(row)}
        columns={[
          { label: '订单号', render: (row) => <code>{row.trade_no}</code> },
          { label: '用户 ID', render: (row) => String(row.user_id) },
          { label: '类型', render: (row) => row.kind },
          { label: '状态', render: (row) => <Status value={row.state} /> },
          { label: '履约状态', render: (row) => row.fulfillment_state || '—', hideOnMobile: true },
          {
            label: '支付金额',
            render: (row) => paymentAmount(row.amount_minor, row.currency),
            numeric: true,
          },
          { label: '创建时间', render: (row) => date(row.created_at), hideOnMobile: true },
          {
            label: '操作',
            render: (row) => (
              <Button variant="quiet" onClick={() => setSelected(row)}>
                {t('查看详情')}
              </Button>
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
      <Drawer
        open={selected !== null}
        onOpenChange={(open) => !open && setSelected(null)}
        title="订单详情"
        description={selected?.trade_no}
      >
        {selected && <OrderDetail order={selected} />}
      </Drawer>
    </>
  )
}

export default function AdminOrdersPage() {
  const { nt } = useNotificationTranslation()
  return (
    <>
      <PageHeader title="订单审核" description={nt('paymentReviewQueueDescription')} />
      <Tabs
        label="订单审核"
        items={[
          { value: 'orders', label: '全部订单', content: <OrderList /> },
          { value: 'reviews', label: '人工审核', content: <PackagePaymentReviews /> },
        ]}
      />
    </>
  )
}
