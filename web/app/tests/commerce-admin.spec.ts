import { test, expect } from '@playwright/test'
import { fixtureAPI } from './fixtures'

test.beforeEach(async ({ page }) => fixtureAPI(page))

test('admin orders lists orders and resolving a package payment review requires confirmation', async ({
  page,
}) => {
  await page.route('**/api/commerce/admin/package-payment-reviews', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: [
          {
            order_id: 42,
            user_id: 7,
            trade_no: 'review-trade',
            provider: 'stripe',
            amount_minor: 999,
            currency: 'usd',
            reason: 'provider refund after converted entitlement consumption',
            created_at: '2026-09-30T08:00:00Z',
          },
        ],
      },
    }),
  )
  const resolved = page.waitForRequest(
    (request) =>
      new URL(request.url()).pathname ===
        '/api/commerce/admin/package-payment-reviews/42/resolve' && request.method() === 'POST',
  )
  await page.route('**/api/commerce/admin/package-payment-reviews/42/resolve', (route) =>
    route.fulfill({ json: { success: true, data: null } }),
  )
  await page.goto('/admin/orders')
  await expect(page.getByRole('cell', { name: 'v3_example' })).toBeVisible()
  await page.getByRole('tab', { name: '人工审核', exact: true }).click()
  await expect(page.getByRole('cell', { name: 'review-trade' })).toBeVisible()
  await page.getByRole('button', { name: '标记已处理', exact: true }).click()
  await page
    .getByRole('dialog')
    .getByRole('button', { name: '确认已退款，标记完成', exact: true })
    .click()
  await resolved
  await expect(page.getByRole('status').filter({ hasText: '已处理' })).toBeVisible()
})

test('admin orders opens the detail drawer for a selected order', async ({ page }) => {
  await page.goto('/admin/orders')
  await page.getByRole('button', { name: '查看详情', exact: true }).click()
  const drawer = page.getByRole('dialog')
  await expect(drawer).toContainText('v3_example')
  await expect(drawer.getByText('已支付')).toBeVisible()
})

test('retired packages page redirects to the wallet subscription checkout', async ({ page }) => {
  await page.route('**/api/packages/purchase', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          trade_no: 'wallet-subscription-checkout',
          amount_minor: 1000,
          currency: 'usd',
          state: 'created',
          payment_url: '/orders',
        },
      },
    }),
  )
  await page.goto('/packages')
  await expect(page).toHaveURL(/\/wallet\?section=plans$/)
  await expect(page.getByRole('heading', { name: '钱包', exact: true, level: 1 })).toBeVisible()
  await expect(page.getByRole('heading', { name: '套餐包', exact: true })).toHaveCount(0)
  await expect(page.getByRole('tab', { name: '购买套餐', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await page.getByRole('button', { name: '购买', exact: true }).click()
  const sent = page.waitForRequest('**/api/packages/purchase')
  await page.getByRole('button', { name: '创建订单', exact: true }).click()
  expect((await sent).postDataJSON()).toMatchObject({ plan_id: 1, provider: 'stripe' })
  await expect(page.getByRole('status')).toContainText('wallet-subscription-checkout')
})
