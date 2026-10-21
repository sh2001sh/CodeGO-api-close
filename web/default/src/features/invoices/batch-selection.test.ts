import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  canMergeInvoiceOrder,
  invoiceOrderKey,
  invoiceSelectionTotal,
} from './batch-selection'
import type { InvoiceEligibleOrder } from './types'

const paid: InvoiceEligibleOrder = {
  source_type: 'topup',
  trade_no: 'paid',
  order_title: 'Top-up',
  order_amount: 0.1,
  currency: 'CNY',
  paid_at: 1792000000,
  requested: false,
}

test('merge selection excludes previously requested, refunded, unpaid and unknown-currency orders', () => {
  assert.equal(canMergeInvoiceOrder(paid), true)
  assert.equal(canMergeInvoiceOrder({ ...paid, refund_status: 'failed' }), true)
  for (const patch of [
    { requested: true },
    { refund_status: 'processing' },
    { refund_status: 'success' },
    { currency: '' },
    { paid_at: 0 },
    { order_amount: 0 },
    { order_amount: Number.NaN },
  ]) {
    assert.equal(canMergeInvoiceOrder({ ...paid, ...patch }), false)
  }
})

test('merge previews use currency precision instead of floating-point addition', () => {
  assert.equal(
    invoiceSelectionTotal([paid, { ...paid, order_amount: 0.2 }]),
    0.3
  )
  assert.equal(
    invoiceSelectionTotal([
      { ...paid, currency: 'USDT', order_amount: 1.123456 },
      { ...paid, currency: 'USDT', order_amount: 2.000001 },
    ]),
    3.123457
  )
  assert.equal(invoiceSelectionTotal([]), 0)
  assert.notEqual(
    invoiceOrderKey(paid),
    invoiceOrderKey({ ...paid, source_type: 'subscription' })
  )
})
