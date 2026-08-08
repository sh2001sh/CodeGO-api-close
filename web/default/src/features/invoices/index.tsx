/*
Copyright (C) 2026 codego-api contributors

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

*/
import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { FilePlus2, ReceiptText, RefreshCw } from 'lucide-react'
import { useAuthStore } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'
import { Button } from '@/components/ui/button'
import { SectionPageLayout } from '@/components/layout'
import { getInvoiceEligibleOrders, getInvoiceRequests } from './api'
import { InvoiceAdminPanel } from './components/invoice-admin-panel'
import { InvoiceRequestDialog } from './components/invoice-request-dialog'
import { InvoiceRequestsTable } from './components/invoice-requests-table'
import type { InvoiceEligibleOrder } from './types'

function formatTime(timestamp: number) {
  return timestamp
    ? new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium' }).format(
        timestamp * 1000
      )
    : '-'
}

export function Invoices() {
  const queryClient = useQueryClient()
  const user = useAuthStore((state) => state.auth.user)
  const [selectedOrder, setSelectedOrder] =
    useState<InvoiceEligibleOrder | null>(null)
  const orders = useQuery({
    queryKey: ['invoice-eligible-orders'],
    queryFn: getInvoiceEligibleOrders,
  })
  const requests = useQuery({
    queryKey: ['invoice-requests'],
    queryFn: getInvoiceRequests,
  })
  const refresh = () => {
    void queryClient.invalidateQueries({
      queryKey: ['invoice-eligible-orders'],
    })
    void queryClient.invalidateQueries({ queryKey: ['invoice-requests'] })
  }

  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>电子发票</SectionPageLayout.Title>
        <SectionPageLayout.Description>
          已支付的充值和套餐订单可申请电子发票；每笔订单仅可申请一次。
        </SectionPageLayout.Description>
        <SectionPageLayout.Actions>
          <Button
            variant='outline'
            onClick={refresh}
            title='刷新订单与申请状态'
          >
            <RefreshCw />
            刷新
          </Button>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <section>
            <div className='mb-3 flex items-center gap-2'>
              <ReceiptText className='text-primary size-5' />
              <h2 className='text-base font-semibold'>可开票订单</h2>
            </div>
            <div className='border-border overflow-hidden rounded-lg border'>
              {orders.isLoading ? (
                <p className='text-muted-foreground py-8 text-center text-sm'>
                  正在加载订单...
                </p>
              ) : null}
              {orders.isError ? (
                <p className='text-destructive py-8 text-center text-sm'>
                  订单数据加载失败，请刷新重试
                </p>
              ) : null}
              {orders.data?.length === 0 ? (
                <p className='text-muted-foreground py-8 text-center text-sm'>
                  暂无符合条件的已支付订单
                </p>
              ) : null}
              {orders.data?.map((order) => (
                <div
                  key={`${order.source_type}-${order.trade_no}`}
                  className='border-border flex flex-col gap-3 border-b px-4 py-3 last:border-b-0 sm:flex-row sm:items-center sm:justify-between'
                >
                  <div className='min-w-0'>
                    <div className='font-medium'>{order.order_title}</div>
                    <div className='text-muted-foreground mt-1 text-xs'>
                      {order.currency} {order.order_amount.toFixed(2)} ·{' '}
                      {formatTime(order.paid_at)} · {order.trade_no}
                    </div>
                  </div>
                  <Button
                    size='sm'
                    variant='outline'
                    disabled={order.requested}
                    onClick={() => setSelectedOrder(order)}
                  >
                    <FilePlus2 />
                    {order.requested ? '已申请' : '申请发票'}
                  </Button>
                </div>
              ))}
            </div>
          </section>

          <section className='border-border mt-8 border-t pt-6'>
            <h2 className='mb-3 text-base font-semibold'>申请记录</h2>
            <div className='border-border overflow-hidden rounded-lg border'>
              {requests.isLoading ? (
                <p className='text-muted-foreground py-8 text-center text-sm'>
                  正在加载申请记录...
                </p>
              ) : null}
              {requests.isError ? (
                <p className='text-destructive py-8 text-center text-sm'>
                  申请记录加载失败
                </p>
              ) : null}
              {requests.data ? (
                <InvoiceRequestsTable requests={requests.data.items} />
              ) : null}
            </div>
          </section>

          {user?.role !== undefined && user.role >= ROLE.ADMIN ? (
            <InvoiceAdminPanel />
          ) : null}
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <InvoiceRequestDialog
        order={selectedOrder}
        open={Boolean(selectedOrder)}
        onOpenChange={(open) => !open && setSelectedOrder(null)}
        onSubmitted={refresh}
      />
    </>
  )
}
