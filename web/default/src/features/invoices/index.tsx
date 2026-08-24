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
import { useMemo, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  CircleCheckBig,
  FileClock,
  FilePlus2,
  ReceiptText,
  RefreshCw,
  ScrollText,
} from 'lucide-react'
import { useAuthStore } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { SectionPageLayout } from '@/components/layout'
import { getInvoiceEligibleOrders, getInvoiceRequests } from './api'
import { InvoiceAdminPanel } from './components/invoice-admin-panel'
import { InvoiceRequestDialog } from './components/invoice-request-dialog'
import {
  InvoiceRequestsTable,
  InvoiceStatusBadge,
} from './components/invoice-requests-table'
import type { InvoiceEligibleOrder, InvoiceRequest } from './types'

function formatMoney(order: InvoiceEligibleOrder | InvoiceRequest) {
  return order.currency + ' ' + order.order_amount.toFixed(2)
}

function formatDate(timestamp: number) {
  return timestamp
    ? new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium' }).format(
        timestamp * 1000
      )
    : '-'
}

function getOrderSourceLabel(sourceType: InvoiceEligibleOrder['source_type']) {
  return sourceType === 'subscription' ? '套餐订单' : '充值订单'
}

function buildRequestSummary(requests: InvoiceRequest[] | undefined) {
  return (
    requests?.reduce(
      (summary, request) => {
        summary.total += 1
        summary[request.status] += 1
        return summary
      },
      { total: 0, pending: 0, issued: 0, rejected: 0 }
    ) ?? { total: 0, pending: 0, issued: 0, rejected: 0 }
  )
}

function SummaryCard({
  title,
  value,
  hint,
  icon: Icon,
}: {
  title: string
  value: string
  hint: string
  icon: typeof ReceiptText
}) {
  return (
    <Card size='sm' className='bg-card/95'>
      <CardHeader className='gap-2'>
        <div className='flex items-center justify-between gap-3'>
          <div className='space-y-1'>
            <CardDescription>{title}</CardDescription>
            <CardTitle className='text-2xl font-semibold tracking-tight'>
              {value}
            </CardTitle>
          </div>
          <div className='bg-primary/10 text-primary flex size-10 items-center justify-center rounded-xl'>
            <Icon className='size-5' />
          </div>
        </div>
      </CardHeader>
      <CardContent>
        <p className='text-muted-foreground text-sm'>{hint}</p>
      </CardContent>
    </Card>
  )
}

export function Invoices() {
  const queryClient = useQueryClient()
  const user = useAuthStore((state) => state.auth.user)
  const [selectedOrderKeys, setSelectedOrderKeys] = useState<string[]>([])
  const [dialogOrders, setDialogOrders] = useState<InvoiceEligibleOrder[]>([])
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

  const eligibleOrders = useMemo(() => orders.data ?? [], [orders.data])
  const requestItems = requests.data?.items
  const invoiceRequests = requestItems ?? []
  const requestSummary = useMemo(
    () => buildRequestSummary(requestItems),
    [requestItems]
  )
  const readyToApplyCount = eligibleOrders.filter(
    (order) => !order.requested
  ).length
  const selectedOrders = useMemo(
    () =>
      eligibleOrders.filter(
        (order) =>
          !order.requested &&
          selectedOrderKeys.includes(order.source_type + '-' + order.trade_no)
      ),
    [eligibleOrders, selectedOrderKeys]
  )
  const selectedTotal = selectedOrders.reduce(
    (sum, order) => sum + order.order_amount,
    0
  )
  const toggleOrder = (order: InvoiceEligibleOrder) => {
    const key = order.source_type + '-' + order.trade_no
    setSelectedOrderKeys((current) =>
      current.includes(key)
        ? current.filter((item) => item !== key)
        : [...current, key]
    )
  }

  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>电子发票</SectionPageLayout.Title>
        <SectionPageLayout.Description>
          统一查看可开票订单、提交申请，并跟踪开票与交付状态。
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
          <div className='space-y-4'>
            <div className='grid gap-4 md:grid-cols-3'>
              <SummaryCard
                title='可申请订单'
                value={String(readyToApplyCount)}
                hint='已支付且未申请发票的订单数量'
                icon={ReceiptText}
              />
              <SummaryCard
                title='待处理申请'
                value={String(requestSummary.pending)}
                hint='已提交，等待管理员开具或驳回'
                icon={FileClock}
              />
              <SummaryCard
                title='已完成开票'
                value={String(requestSummary.issued)}
                hint='已开具并通过邮箱或链接交付'
                icon={CircleCheckBig}
              />
            </div>

            <div className='grid gap-4 xl:grid-cols-[minmax(0,1.25fr)_minmax(320px,0.75fr)]'>
              <Card className='bg-card/95'>
                <CardHeader className='gap-3 border-b'>
                  <div className='flex flex-wrap items-start justify-between gap-3'>
                    <div className='space-y-1'>
                      <CardTitle className='flex items-center gap-2'>
                        <ReceiptText className='text-primary size-4.5' />
                        可开票订单
                      </CardTitle>
                      <CardDescription>
                        每笔已支付订单仅可申请一次。已申请订单会保留在列表中，方便核对状态。
                      </CardDescription>
                    </div>
                    <CardAction className='static'>
                      <div className='flex flex-wrap items-center gap-2'>
                        <Badge variant='secondary'>
                          {readyToApplyCount} 笔待申请
                        </Badge>
                        {selectedOrders.length > 0 ? (
                          <span className='text-muted-foreground text-xs tabular-nums'>
                            已选 {selectedOrders.length} 笔 ·{' '}
                            {selectedOrders[0].currency}{' '}
                            {selectedTotal.toFixed(2)}
                          </span>
                        ) : null}
                        <Button
                          size='sm'
                          disabled={selectedOrders.length === 0}
                          onClick={() => setDialogOrders(selectedOrders)}
                        >
                          <FilePlus2 />
                          {selectedOrders.length > 1
                            ? `合并开票（${selectedOrders.length}）`
                            : '申请发票'}
                        </Button>
                      </div>
                    </CardAction>
                  </div>
                </CardHeader>
                <CardContent className='space-y-3'>
                  {orders.isLoading ? (
                    <p className='text-muted-foreground py-10 text-center text-sm'>
                      正在加载订单...
                    </p>
                  ) : null}
                  {orders.isError ? (
                    <p className='text-destructive py-10 text-center text-sm'>
                      订单数据加载失败，请刷新重试
                    </p>
                  ) : null}
                  {eligibleOrders.length === 0 ? (
                    <div className='bg-muted/20 flex min-h-56 flex-col items-center justify-center gap-2 rounded-2xl border border-dashed px-6 text-center'>
                      <ReceiptText className='text-muted-foreground size-5' />
                      <p className='font-medium'>暂无符合条件的已支付订单</p>
                      <p className='text-muted-foreground max-w-md text-sm'>
                        充值或套餐支付完成后，符合开票条件的订单会出现在这里。
                      </p>
                    </div>
                  ) : null}
                  {eligibleOrders.map((order) => (
                    <div
                      key={order.source_type + '-' + order.trade_no}
                      className='bg-background/75 border-border/80 flex flex-col gap-3 rounded-2xl border px-4 py-4 sm:flex-row sm:items-start sm:justify-between'
                    >
                      <Checkbox
                        checked={selectedOrderKeys.includes(
                          order.source_type + '-' + order.trade_no
                        )}
                        disabled={order.requested}
                        onCheckedChange={() => toggleOrder(order)}
                        aria-label={`选择${order.order_title}`}
                      />
                      <div className='min-w-0 space-y-2'>
                        <div className='flex flex-wrap items-center gap-2'>
                          <h3 className='font-medium'>{order.order_title}</h3>
                          <Badge
                            variant={order.requested ? 'secondary' : 'default'}
                          >
                            {order.requested ? '已申请' : '可申请'}
                          </Badge>
                          <Badge variant='outline'>
                            {getOrderSourceLabel(order.source_type)}
                          </Badge>
                        </div>
                        <div className='text-muted-foreground flex flex-wrap items-center gap-x-3 gap-y-1 text-sm'>
                          <span>{formatMoney(order)}</span>
                          <span>{formatDate(order.paid_at)}</span>
                          <span className='truncate font-mono text-xs'>
                            {order.trade_no}
                          </span>
                        </div>
                      </div>
                      <Button
                        size='sm'
                        variant={order.requested ? 'outline' : 'default'}
                        disabled={order.requested}
                        onClick={() => setDialogOrders([order])}
                        className='sm:min-w-28'
                      >
                        <FilePlus2 />
                        {order.requested ? '已提交申请' : '申请发票'}
                      </Button>
                    </div>
                  ))}
                </CardContent>
              </Card>

              <div className='space-y-4'>
                <Card className='bg-card/95'>
                  <CardHeader className='gap-3 border-b'>
                    <CardTitle className='flex items-center gap-2'>
                      <ScrollText className='text-primary size-4.5' />
                      申请说明
                    </CardTitle>
                    <CardDescription>
                      先确认订单，再填写抬头与接收邮箱；开票完成后会通过邮箱或下载链接交付。
                    </CardDescription>
                  </CardHeader>
                  <CardContent className='space-y-3'>
                    <div className='bg-background/75 border-border/80 rounded-2xl border px-4 py-3'>
                      <div className='text-sm font-medium'>1. 选择订单</div>
                      <p className='text-muted-foreground mt-1 text-sm leading-6'>
                        仅展示已支付且符合开票条件的充值或套餐订单。
                      </p>
                    </div>
                    <div className='bg-background/75 border-border/80 rounded-2xl border px-4 py-3'>
                      <div className='text-sm font-medium'>2. 填写开票信息</div>
                      <p className='text-muted-foreground mt-1 text-sm leading-6'>
                        企业发票需填写抬头与税号；个人发票只需抬头和接收邮箱。
                      </p>
                    </div>
                    <div className='bg-background/75 border-border/80 rounded-2xl border px-4 py-3'>
                      <div className='text-sm font-medium'>3. 跟踪处理结果</div>
                      <p className='text-muted-foreground mt-1 text-sm leading-6'>
                        待处理、已开具、已驳回都会展示在申请记录中，驳回原因也会保留。
                      </p>
                    </div>
                  </CardContent>
                </Card>

                <Card className='bg-card/95'>
                  <CardHeader className='gap-3 border-b'>
                    <div className='flex flex-wrap items-start justify-between gap-3'>
                      <div className='space-y-1'>
                        <CardTitle>申请记录</CardTitle>
                        <CardDescription>
                          已提交申请的处理状态、发票号码和交付方式都会在这里更新。
                        </CardDescription>
                      </div>
                      {invoiceRequests.length > 0 ? (
                        <div className='flex flex-wrap gap-2'>
                          <InvoiceStatusBadge status='pending' />
                          <InvoiceStatusBadge status='issued' />
                          <InvoiceStatusBadge status='rejected' />
                        </div>
                      ) : null}
                    </div>
                  </CardHeader>
                  <CardContent>
                    {requests.isLoading ? (
                      <p className='text-muted-foreground py-10 text-center text-sm'>
                        正在加载申请记录...
                      </p>
                    ) : null}
                    {requests.isError ? (
                      <p className='text-destructive py-10 text-center text-sm'>
                        申请记录加载失败
                      </p>
                    ) : null}
                    {requests.data ? (
                      <InvoiceRequestsTable requests={invoiceRequests} />
                    ) : null}
                  </CardContent>
                </Card>
              </div>
            </div>

            {user?.role !== undefined && user.role >= ROLE.ADMIN ? (
              <InvoiceAdminPanel />
            ) : null}
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <InvoiceRequestDialog
        key={dialogOrders
          .map((order) => order.source_type + '-' + order.trade_no)
          .join('|')}
        orders={dialogOrders}
        open={dialogOrders.length > 0}
        onOpenChange={(open) => !open && setDialogOrders([])}
        onSubmitted={refresh}
      />
    </>
  )
}
