/*
Copyright (C) 2023-2026 QuantumNous

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

For commercial licensing, please contact support@quantumnous.com
*/
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { getIntlLocale } from '@/i18n/format'
import { LoaderCircle, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatQuota } from '@/lib/format'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { ConfirmDialog } from '@/components/confirm-dialog'
import {
  createAdminRefund,
  getAdminRefundableOrders,
  syncAdminRefund,
} from '../../api'
import type { RefundableOrder, RefundResult, User } from '../../types'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  user: Pick<User, 'id' | 'username'>
}

const PAGE_SIZE = 10

export function UserRefundDialog(props: Props) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [confirmOrder, setConfirmOrder] = useState<RefundableOrder | null>(null)
  const [page, setPage] = useState(1)
  const queryKey = ['admin-user-refunds', props.user.id]
  const updateRefundStatus = (result: RefundResult) => {
    queryClient.setQueryData<RefundableOrder[]>(queryKey, (current) =>
      current?.map((order) =>
        order.order_type === result.order_type &&
        order.trade_no === result.trade_no
          ? {
              ...order,
              refund_status: result.status,
              refund_no: result.refund_no,
              refundable: result.status === 'failed',
            }
          : order
      )
    )
  }
  const orders = useQuery({
    queryKey,
    enabled: props.open,
    retry: false,
    refetchOnWindowFocus: false,
    queryFn: async () => {
      const response = await getAdminRefundableOrders(props.user.id)
      if (!response.success || !response.data) {
        throw new Error(response.message)
      }
      return response.data.items.sort((a, b) => b.created_at - a.created_at)
    },
  })
  const refund = useMutation({
    mutationFn: async (order: RefundableOrder) => {
      const response = await createAdminRefund(props.user.id, {
        order_type: order.order_type,
        trade_no: order.trade_no,
      })
      if (!response.success || !response.data) {
        throw new Error(response.message || t('Refund request failed'))
      }
      return response.data
    },
    onSuccess: async (result) => {
      updateRefundStatus(result)
      setConfirmOrder(null)
      toast.success(t('Refund request submitted'))
      await queryClient.invalidateQueries({ queryKey })
    },
    onError: (error) => {
      setConfirmOrder(null)
      toast.error(error.message || t('Refund request failed'))
      void queryClient.invalidateQueries({ queryKey })
    },
  })
  const sync = useMutation({
    mutationFn: async (refundNo: string) => {
      const response = await syncAdminRefund(props.user.id, refundNo)
      if (!response.success || !response.data) {
        throw new Error(response.message || t('Refund sync failed'))
      }
      return response.data
    },
    onSuccess: async (result) => {
      updateRefundStatus(result)
      toast.success(t('Refund status updated'))
      await queryClient.invalidateQueries({ queryKey })
    },
    onError: (error) => toast.error(error.message || t('Refund sync failed')),
  })
  const busy = refund.isPending || sync.isPending
  const items = orders.data || []
  const totalPages = Math.max(1, Math.ceil(items.length / PAGE_SIZE))
  const currentPage = Math.min(page, totalPages)
  const visibleItems = items.slice(
    (currentPage - 1) * PAGE_SIZE,
    currentPage * PAGE_SIZE
  )
  const money = (amount: number) =>
    new Intl.NumberFormat(getIntlLocale(), {
      style: 'currency',
      currency: 'CNY',
    }).format(amount)
  const statusLabel = (order: RefundableOrder) => {
    if (order.refund_status === 'processing') return t('Refund is processing')
    if (order.refund_status === 'success') return t('Refund completed')
    if (order.refund_status === 'failed') return t('Refund failed')
    if (order.refundable) return t('Refundable')
    return t('No refundable amount')
  }

  return (
    <>
      <Dialog
        open={props.open}
        onOpenChange={(open) => !busy && props.onOpenChange(open)}
      >
        <DialogContent
          className='solaris-admin-dialog flex max-h-[calc(100dvh-2rem)] flex-col sm:max-w-5xl'
          aria-describedby={undefined}
          showCloseButton={!busy}
        >
          <DialogHeader>
            <DialogTitle>
              {t('Refund management')} · {props.user.username}
            </DialogTitle>
          </DialogHeader>
          <div className='flex justify-end'>
            <Button
              variant='outline'
              size='sm'
              disabled={orders.isFetching || busy}
              onClick={() => void orders.refetch()}
            >
              <RefreshCw className='size-4' aria-hidden='true' />
              {t('Refresh')}
            </Button>
          </div>
          {orders.isPending ? (
            <div role='status' className='flex justify-center py-12'>
              <LoaderCircle
                className='size-5 animate-spin'
                aria-hidden='true'
              />
              <span className='sr-only'>{t('Loading...')}</span>
            </div>
          ) : orders.isError ? (
            <div role='alert' className='space-y-3 py-8 text-center'>
              <p>{t('Failed to load refund information')}</p>
              <Button variant='outline' onClick={() => void orders.refetch()}>
                {t('Retry')}
              </Button>
            </div>
          ) : items.length === 0 ? (
            <p className='text-muted-foreground py-12 text-center'>
              {t('No refundable orders')}
            </p>
          ) : (
            <div className='min-h-0 overflow-auto'>
              <Table className='min-w-[720px]'>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('订单号')}</TableHead>
                    <TableHead className='text-right'>{t('Paid')}</TableHead>
                    <TableHead className='text-right'>
                      {t('Used quota')}
                    </TableHead>
                    <TableHead className='text-right'>
                      {t('Refundable amount')}
                    </TableHead>
                    <TableHead>{t('Status')}</TableHead>
                    <TableHead className='text-right'>{t('Actions')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {visibleItems.map((order) => (
                    <TableRow key={`${order.order_type}:${order.trade_no}`}>
                      <TableCell className='max-w-64 break-all whitespace-normal'>
                        {order.trade_no}
                      </TableCell>
                      <TableCell className='text-right tabular-nums'>
                        {money(order.paid_amount)}
                      </TableCell>
                      <TableCell className='text-right tabular-nums'>
                        {formatQuota(order.used_quota)}
                      </TableCell>
                      <TableCell className='text-right tabular-nums'>
                        {money(order.refund_amount)}
                      </TableCell>
                      <TableCell className='max-w-48 whitespace-normal'>
                        <div>{statusLabel(order)}</div>
                        {order.unavailable_reason && !order.refundable && (
                          <div className='text-muted-foreground text-xs'>
                            {order.unavailable_reason}
                          </div>
                        )}
                      </TableCell>
                      <TableCell className='text-right'>
                        {order.refund_status === 'processing' ? (
                          <Button
                            variant='outline'
                            size='sm'
                            disabled={busy || !order.refund_no}
                            onClick={() =>
                              order.refund_no && sync.mutate(order.refund_no)
                            }
                          >
                            {t('Sync refund')}
                          </Button>
                        ) : (
                          <Button
                            variant='outline'
                            size='sm'
                            disabled={
                              busy ||
                              orders.isFetching ||
                              !order.refundable ||
                              order.refund_status === 'success'
                            }
                            onClick={() => setConfirmOrder(order)}
                          >
                            {t('Refund')}
                          </Button>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
          {!orders.isError && items.length > PAGE_SIZE && (
            <div className='flex items-center justify-end gap-3'>
              <Button
                variant='outline'
                size='sm'
                disabled={currentPage <= 1 || busy}
                onClick={() => setPage(currentPage - 1)}
              >
                {t('上一页')}
              </Button>
              <span className='tabular-nums'>
                {currentPage} / {totalPages}
              </span>
              <Button
                variant='outline'
                size='sm'
                disabled={currentPage >= totalPages || busy}
                onClick={() => setPage(currentPage + 1)}
              >
                {t('下一页')}
              </Button>
            </div>
          )}
        </DialogContent>
      </Dialog>
      <ConfirmDialog
        className='solaris-admin-dialog'
        open={!!confirmOrder}
        onOpenChange={(open) =>
          !refund.isPending && !open && setConfirmOrder(null)
        }
        title={t('Confirm refund of {{amount}}?', {
          amount: money(confirmOrder?.refund_amount || 0),
        })}
        desc={confirmOrder?.trade_no || ''}
        confirmText={t('Refund')}
        destructive
        isLoading={refund.isPending}
        handleConfirm={() => confirmOrder && refund.mutate(confirmOrder)}
      />
    </>
  )
}
