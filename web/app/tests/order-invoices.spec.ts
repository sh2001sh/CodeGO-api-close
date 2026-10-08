import { readFile } from 'node:fs/promises'
import { test, expect, type Page } from '@playwright/test'
import { fixtureAPI } from './fixtures'

const order = {
  id: 1,
  trade_no: 'paid-order',
  kind: 'subscription',
  plan_id: 1,
  state: 'paid',
  amount_minor: '9223372036854775807',
  currency: 'hkd',
  credits: 100000000,
  created_at: '2026-09-30T08:00:00Z',
  paid_at: '2026-09-30T08:01:00Z',
}

async function invoiceFixture(page: Page) {
  await fixtureAPI(page)
  await page.route('**/api/billing/balance', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: { account_id: 1, balance_micro: 0, version: 0, balance_micro_credits: '0' },
      },
    }),
  )
  await page.route('**/api/billing/entries**', (route) =>
    route.fulfill({ json: { success: true, data: { items: [] } } }),
  )
  await page.route('**/api/commerce/orders?*', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: [
          order,
          { ...order, id: 2, trade_no: 'unpaid-order', state: 'pending' },
          { ...order, id: 3, trade_no: 'refunded-order', state: 'refunded' },
          { ...order, id: 4, trade_no: 'free-order', amount_minor: 0 },
        ],
      },
    }),
  )
  await page.goto('/billing')
  await page.getByRole('tab', { name: '发票', exact: true }).click()
  await expect(page.getByRole('heading', { name: '商业发票', exact: true })).toBeVisible()
}

test('paid orders download PDF with exact amounts, own scope and no manual request', async ({
  page,
}) => {
  const forbidden: string[] = []
  page.on('request', (request) => {
    if (
      request.url().includes('/api/commerce/admin/orders') ||
      (request.url().includes('/api/invoices/requests') && request.method() === 'POST')
    )
      forbidden.push(request.url())
  })
  const pdf = Buffer.from('%PDF-1.4\nCodeGo test invoice\n%%EOF\n')
  await invoiceFixture(page)
  await page.route('**/api/commerce/orders/paid-order/invoice', (route) => {
    expect(route.request().headers()['accept']).toBe('application/pdf')
    return route.fulfill({
      contentType: 'application/pdf',
      headers: { 'Content-Disposition': 'attachment; filename="CG-2026-000000000001.pdf"' },
      body: pdf,
    })
  })
  const table = page.getByRole('region', { name: '已支付订单发票', exact: true })
  await expect(table.getByRole('cell', { name: 'HKD 92,233,720,368,547,758.07' })).toBeVisible()
  await expect(table.getByRole('button', { name: '下载 PDF' })).toHaveCount(1)
  await expect(table.getByText('unpaid-order')).toHaveCount(0)
  await expect(table.getByText('refunded-order')).toHaveCount(0)
  await expect(table.getByText('free-order')).toHaveCount(0)
  const downloaded = page.waitForEvent('download')
  await table.getByRole('button', { name: '下载 PDF' }).click()
  const download = await downloaded
  expect(download.suggestedFilename()).toBe('CG-2026-000000000001.pdf')
  expect(await readFile((await download.path())!)).toEqual(pdf)
  expect(forbidden).toEqual([])
})

test('refund rejection remains visible and allows retry without a false download', async ({
  page,
}) => {
  await invoiceFixture(page)
  await page.route('**/api/commerce/orders/paid-order/invoice', (route) =>
    route.fulfill({ status: 409, json: { success: false, message: '订单状态冲突' } }),
  )
  const downloads: string[] = []
  page.on('download', (download) => downloads.push(download.suggestedFilename()))
  await page.getByRole('button', { name: '下载 PDF' }).click()
  await expect(page.getByRole('alert')).toHaveText('订单状态冲突')
  await expect(page.getByRole('button', { name: '下载 PDF' })).toBeEnabled()
  expect(downloads).toEqual([])
})

test('expired download session refreshes once and then downloads', async ({ page }) => {
  await invoiceFixture(page)
  let attempts = 0
  let refreshes = 0
  await page.route('**/api/user/refresh', (route) => {
    refreshes++
    return route.fulfill({ json: { success: true, data: {} } })
  })
  await page.route('**/api/commerce/orders/paid-order/invoice', (route) => {
    attempts++
    return route.fulfill(
      attempts === 1
        ? { status: 401, json: { success: false, message: '身份验证失败' } }
        : { contentType: 'application/pdf', body: '%PDF-1.4\n%%EOF\n' },
    )
  })
  const downloaded = page.waitForEvent('download')
  await page.getByRole('button', { name: '下载 PDF' }).click()
  await downloaded
  expect(attempts).toBe(2)
  expect(refreshes).toBe(1)
})

test('invoice deep links open the invoice tab and reject JSON success masquerading as PDF', async ({
  page,
}) => {
  await invoiceFixture(page)
  await page.goto('/billing#invoices')
  await expect(page.getByRole('tab', { name: '发票', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await page.route('**/api/commerce/orders/paid-order/invoice', (route) =>
    route.fulfill({ json: { success: true, data: 'invalid-pdf' } }),
  )
  const downloads: string[] = []
  page.on('download', (download) => downloads.push(download.suggestedFilename()))
  await page.getByRole('button', { name: '下载 PDF' }).click()
  await expect(page.getByRole('alert')).toHaveText('服务器未返回有效的 PDF 发票')
  expect(downloads).toEqual([])
})

test('historical requests remain readable without the manual application form', async ({
  page,
}) => {
  await fixtureAPI(page)
  await page.route('**/api/invoices/requests?*', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          page: 1,
          page_size: 20,
          total: 1,
          items: [
            {
              id: 7,
              title: '历史公司抬头',
              order_title: '旧版套餐',
              order_amount_minor: 2000,
              currency: 'hkd',
              status: 'issued',
              invoice_number: 'LEGACY-7',
              admin_note: '',
            },
          ],
        },
      },
    }),
  )
  // Set up the financial endpoints without overwriting the historical request route.
  await page.route('**/api/billing/balance', (route) =>
    route.fulfill({
      json: { success: true, data: { account_id: 1, balance_micro_credits: '0', version: 0 } },
    }),
  )
  await page.goto('/billing')
  await page.getByRole('tab', { name: '发票', exact: true }).click()
  await page.getByText('历史发票申请', { exact: true }).first().click()
  await expect(page.getByRole('cell', { name: 'LEGACY-7' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '申请发票' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: /提交申请/ })).toHaveCount(0)
})

test('first self-service issue requires real purchaser details and subsequent downloads use the frozen PDF', async ({
  page,
}) => {
  await invoiceFixture(page)
  let issued = false
  let issuedCount = 0
  let submitted: unknown
  const pdf = Buffer.from('%PDF-1.4\nIssued CodeGo commercial invoice\n%%EOF\n')
  await page.route('**/api/commerce/orders/paid-order/invoice', (route) => {
    if (route.request().method() === 'POST') {
      issuedCount++
      submitted = route.request().postDataJSON()
      issued = true
    }
    return route.fulfill(
      issued
        ? {
            contentType: 'application/pdf',
            body: pdf,
            headers: { 'Content-Disposition': 'attachment; filename="CG-2026-000000000001.pdf"' },
          }
        : {
            status: 428,
            json: { success: false, message: '请先填写发票抬头和购买方地址' },
          },
    )
  })
  await page.getByRole('button', { name: '下载 PDF' }).click()
  const dialog = page.getByRole('dialog', { name: '开具商业发票' })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByLabel('发票抬头', { exact: true })).toHaveValue('')
  await expect(dialog.getByLabel(/税号/)).toHaveCount(0)
  await dialog.getByLabel('发票抬头', { exact: true }).fill('  Hong Kong Buyer Limited  ')
  await dialog
    .getByLabel('购买方地址', { exact: true })
    .fill('  Unit 7, Example Street\nHong Kong  ')
  const downloaded = page.waitForEvent('download')
  await dialog.getByRole('button', { name: '开具并下载' }).click()
  const first = await downloaded
  expect(await readFile((await first.path())!)).toEqual(pdf)
  expect(submitted).toEqual({
    buyer_name: 'Hong Kong Buyer Limited',
    buyer_address: 'Unit 7, Example Street\nHong Kong',
  })
  await expect(dialog).toHaveCount(0)
  const downloadedAgain = page.waitForEvent('download')
  await page.getByRole('button', { name: '下载 PDF' }).click()
  const second = await downloadedAgain
  expect(await readFile((await second.path())!)).toEqual(pdf)
  expect(issuedCount).toBe(1)
  expect(await page.evaluate(() => JSON.stringify(localStorage))).not.toContain('Hong Kong Buyer')
})

test('blank purchaser details are rejected locally without issuing an invoice', async ({
  page,
}) => {
  await invoiceFixture(page)
  let issuedCount = 0
  await page.route('**/api/commerce/orders/paid-order/invoice', (route) => {
    if (route.request().method() === 'POST') issuedCount++
    return route.fulfill({
      status: 428,
      json: { success: false, message: '请先填写发票抬头和购买方地址' },
    })
  })
  await page.getByRole('button', { name: '下载 PDF' }).click()
  const dialog = page.getByRole('dialog', { name: '开具商业发票' })
  await dialog.getByLabel('发票抬头', { exact: true }).fill('   ')
  await dialog.getByLabel('购买方地址', { exact: true }).fill('   ')
  await dialog.getByRole('button', { name: '开具并下载' }).click()
  await expect(dialog.getByRole('alert')).toHaveText('请填写完整抬头和购买方地址')
  expect(issuedCount).toBe(0)
})

for (const [status, message] of [
  [503, '开票主体地址未配置，请联系平台'],
  [409, '发票已开具，抬头和地址不可更改'],
] as const) {
  test(`self-service issue shows ${status} explicitly and preserves details for retry`, async ({
    page,
  }) => {
    await invoiceFixture(page)
    await page.route('**/api/commerce/orders/paid-order/invoice', (route) =>
      route.fulfill(
        route.request().method() === 'POST'
          ? { status, json: { success: false, message } }
          : {
              status: 428,
              json: { success: false, message: '请先填写发票抬头和购买方地址' },
            },
      ),
    )
    const downloads: string[] = []
    page.on('download', (download) => downloads.push(download.suggestedFilename()))
    await page.getByRole('button', { name: '下载 PDF' }).click()
    const dialog = page.getByRole('dialog', { name: '开具商业发票' })
    await dialog.getByLabel('发票抬头', { exact: true }).fill('Test Buyer')
    await dialog.getByLabel('购买方地址', { exact: true }).fill('Test purchaser address, Hong Kong')
    await dialog.getByRole('button', { name: '开具并下载' }).click()
    await expect(dialog.getByRole('alert')).toHaveText(message)
    await expect(dialog.getByLabel('发票抬头', { exact: true })).toHaveValue('Test Buyer')
    await expect(dialog.getByRole('button', { name: '开具并下载' })).toBeEnabled()
    expect(downloads).toEqual([])
  })
}
