import type { InvoiceEligibleOrder } from './types'

export const invoiceBatchLimit = 100

export function invoiceOrderKey(order: InvoiceEligibleOrder): string {
  return `${order.source_type}:${order.trade_no}`
}

export function canMergeInvoiceOrder(order: InvoiceEligibleOrder): boolean {
  return (
    !order.requested &&
    (!order.refund_status || order.refund_status === 'failed') &&
    Boolean(order.currency) &&
    order.paid_at > 0 &&
    Number.isFinite(order.order_amount) &&
    order.order_amount > 0
  )
}

export function invoiceSelectionTotal(orders: InvoiceEligibleOrder[]): number {
  const scale = orders[0]?.currency === 'USDT' ? 1_000_000 : 100
  return (
    orders.reduce(
      (total, order) => total + Math.round(order.order_amount * scale),
      0
    ) / scale
  )
}
