import { test, expect, type Route } from '@playwright/test'
import { fixtureAPI } from './fixtures'

test.beforeEach(async ({ page }) => fixtureAPI(page))

test('wallet loads topup without fetching inactive financial sections', async ({ page }) => {
  const inactiveRequests: string[] = []
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname
    if (
      path === '/api/wallet/refunds/eligible' ||
      path === '/api/subscription/plans' ||
      path === '/api/subscription/self' ||
      path === '/api/subscription/self/preference'
    )
      inactiveRequests.push(path)
  })
  await page.goto('/wallet')
  await expect(page.getByRole('region', { name: '钱包余额', exact: true })).toContainText(
    '123.456789 credits',
  )
  await expect(page.getByRole('button', { name: '前往支付', exact: true })).toBeEnabled()
  await expect(page.getByRole('tab', { name: '充值', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  expect(inactiveRequests).toEqual([])
})

test('a stalled refunds request cannot block return to wallet topup', async ({ page }) => {
  let refundRoute: Route | undefined
  await page.route('**/api/wallet/refunds/eligible', (route) => {
    refundRoute = route
  })
  await page.goto('/wallet')
  await expect(page.getByRole('button', { name: '前往支付', exact: true })).toBeEnabled()
  const request = page.waitForRequest('**/api/wallet/refunds/eligible')
  await page.getByRole('tab', { name: '退款', exact: true }).click()
  await request
  await expect(page.getByRole('button', { name: '查询退款', exact: true })).toBeVisible()
  await page.getByRole('tab', { name: '充值', exact: true }).click()
  await expect(page).toHaveURL(/\?section=topup$/)
  await expect(page.getByRole('button', { name: '前往支付', exact: true })).toBeEnabled()
  await expect(page.getByLabel('支付金额', { exact: true })).toBeEditable()
  expect(refundRoute).toBeDefined()
  await refundRoute!.abort()
})

test('a refunds failure stays within its tab and leaves topup usable', async ({ page }) => {
  await page.route('**/api/wallet/refunds/eligible', (route) =>
    route.fulfill({ status: 503, json: { success: false, message: '退款暂时不可用' } }),
  )
  await page.goto('/wallet?section=refunds')
  await expect(page.getByRole('alert')).toHaveText('退款暂时不可用')
  await page.getByRole('tab', { name: '充值', exact: true }).click()
  await expect(page.getByRole('button', { name: '前往支付', exact: true })).toBeEnabled()
  await expect(page.getByRole('alert')).toHaveCount(0)
})

test('package aliases resolve to wallet and retired navigation is absent', async ({ page }) => {
  await page.goto('/packages')
  await expect(page).toHaveURL(/\/wallet\?section=plans$/)
  await expect(page.getByRole('tab', { name: '购买套餐', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await expect(page.getByRole('button', { name: '购买', exact: true })).toBeEnabled()
  await expect(
    page.locator('a[href="/packages"], a[href="/blind-box"], a[href="/admin/blind-box"]'),
  ).toHaveCount(0)
  await page.goto('/blind-box')
  await expect(page).toHaveURL(/\/dashboard$/)
})
