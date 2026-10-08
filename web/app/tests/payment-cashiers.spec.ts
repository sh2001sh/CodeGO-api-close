import { test, expect } from '@playwright/test'
import { fixtureAPI, plan } from './fixtures'

test.beforeEach(async ({ page }) => {
  await fixtureAPI(page)
  await page.route('**/api/commerce/providers', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: [
          {
            provider: 'epay',
            currency: 'cny',
            credits_per_minor: 10000,
            payment_types: ['alipay', 'wxpay'],
          },
        ],
      },
    }),
  )
  await page.route('**/api/subscription/plans', (route) =>
    route.fulfill({ json: { success: true, data: [{ ...plan, currency: 'cny' }] } }),
  )
})

test('wallet sends the selected Epay cashier with exact CNY cents', async ({ page }) => {
  await page.route('**/api/commerce/orders', (route) =>
    route.fulfill({ json: { success: true, data: { payment_url: '/orders' } } }),
  )
  await page.goto('/wallet')
  await page.locator('#wallet-payment-type').selectOption('wxpay')
  await page.getByLabel('支付金额', { exact: true }).fill('1.23')
  const sent = page.waitForRequest(
    (request) =>
      new URL(request.url()).pathname === '/api/commerce/orders' && request.method() === 'POST',
  )
  await page.getByRole('button', { name: '前往支付', exact: true }).click()
  expect((await sent).postDataJSON()).toMatchObject({
    provider: 'epay',
    amount_minor: 123,
    checkout_selection: { payment_method: 'wxpay' },
  })
})

test('package checkout retains the cashier and only offers configured choices', async ({
  page,
}) => {
  await page.route('**/api/commerce/providers', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: [
          {
            provider: 'epay',
            currency: 'cny',
            credits_per_minor: 10000,
            payment_types: ['wxpay'],
          },
        ],
      },
    }),
  )
  await page.route('**/api/packages/purchase', (route) =>
    route.fulfill({ json: { success: true, data: { payment_url: '/orders' } } }),
  )
  await page.goto('/wallet')
  await expect(page.locator('#wallet-payment-type option')).toHaveCount(1)
  await expect(page.locator('#wallet-payment-type')).toHaveValue('wxpay')
  await expect(page.locator('#subscription-payment-type option')).toHaveCount(1)
  await page.getByRole('button', { name: '购买', exact: true }).click()
  const sent = page.waitForRequest('**/api/packages/purchase')
  await page.getByRole('button', { name: '创建订单', exact: true }).click()
  expect((await sent).postDataJSON()).toMatchObject({ provider: 'epay', payment_method: 'wxpay' })
})

test('fuel quote and purchase keep the selected cashier in the frozen request', async ({
  page,
}) => {
  const fuelPlan = {
    ...plan,
    currency: 'cny',
    fuel_enabled: true,
    fuel_min_credits: 1000000,
    fuel_credit_step: 1000000,
  }
  await page.route('**/api/subscription/plans', (route) =>
    route.fulfill({ json: { success: true, data: [fuelPlan] } }),
  )
  await page.route('**/api/subscription/self', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: [
          {
            id: 1,
            plan_id: 1,
            state: 'active',
            balance: 9000000,
            expires_at: '2026-10-30T08:00:00Z',
            plan_snapshot: fuelPlan,
          },
        ],
      },
    }),
  )
  await page.route('**/api/subscription/fuel/quote', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          subscription_id: 1,
          credits: 2000000,
          amount_minor: 200,
          currency: 'cny',
          expires_at: '2026-10-30T08:00:00Z',
          min_credits: 1000000,
          credit_step: 1000000,
        },
      },
    }),
  )
  await page.route('**/api/subscription/fuel/purchase', (route) =>
    route.fulfill({ json: { success: true, data: { payment_url: '/orders' } } }),
  )
  await page.goto('/wallet')
  await page.locator('#fuel-payment-type').selectOption('wxpay')
  await page.getByLabel('购买燃料 credits', { exact: true }).fill('2')
  await page.getByRole('button', { name: '获取燃料报价', exact: true }).click()
  const sent = page.waitForRequest('**/api/subscription/fuel/purchase')
  await page.getByRole('button', { name: '确认创建燃料订单', exact: true }).click()
  expect((await sent).postDataJSON()).toMatchObject({
    provider: 'epay',
    payment_method: 'wxpay',
    subscription_id: 1,
    credits: 2000000,
  })
})
