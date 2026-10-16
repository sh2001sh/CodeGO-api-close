import assert from 'node:assert/strict'
import { test } from 'node:test'
import { api } from '@/lib/api'
import { getInvoiceEligibleOrdersPage } from './api'

test('eligible invoices request only the selected server page and global search', async () => {
  const previousAdapter = api.defaults.adapter
  const requests: URL[] = []
  const controller = new AbortController()
  api.defaults.adapter = async (config) => {
    const request = new URL(api.getUri(config), 'http://localhost')
    requests.push(request)
    assert.equal(config.signal, controller.signal)
    assert.equal(config.skipErrorHandler, true)
    return {
      data: {
        success: true,
        data: {
          items: [{ source_type: 'topup', trade_no: 'oldest-order' }],
          total: 50000,
          page: 1000,
          page_size: 50,
        },
      },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  try {
    const page = await getInvoiceEligibleOrdersPage(
      1000,
      '订单 & CNY',
      controller.signal
    )
    assert.equal(requests.length, 1)
    assert.equal(requests[0].searchParams.get('p'), '1000')
    assert.equal(requests[0].searchParams.get('page_size'), '50')
    assert.equal(requests[0].searchParams.get('keyword'), '订单 & CNY')
    assert.equal(page.total, 50000)
    assert.equal(page.items[0].trade_no, 'oldest-order')
  } finally {
    api.defaults.adapter = previousAdapter
  }
})

test('eligible invoice business failures remain query errors', async () => {
  const previousAdapter = api.defaults.adapter
  api.defaults.adapter = async (config) => ({
    data: { success: false, message: 'invoice.ordersFailed', data: null },
    status: 200,
    statusText: 'OK',
    headers: {},
    config,
  })
  try {
    await assert.rejects(
      getInvoiceEligibleOrdersPage(2, 'missing'),
      /invoice.ordersFailed/
    )
  } finally {
    api.defaults.adapter = previousAdapter
  }
})
