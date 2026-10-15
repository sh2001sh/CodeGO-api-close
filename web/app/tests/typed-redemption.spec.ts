import { test, expect } from '@playwright/test'
import { fixtureAPI } from './fixtures'

test.beforeEach(async ({ page }) => fixtureAPI(page))

test('subscription redemption reports the entitlement and repeats the same server receipt', async ({
  page,
}) => {
  const keys: string[] = []
  let claimed = false
  await page.route('**/api/subscription/self', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: claimed
          ? [
              {
                id: '9007199254740993',
                plan_id: 1,
                state: 'active',
                balance: 1000000,
                expires_at: '2027-01-01T00:00:00Z',
              },
            ]
          : [],
      },
    }),
  )
  await page.route('**/api/commerce/redemptions/redeem', (route) => {
    keys.push(route.request().postDataJSON().key)
    claimed = true
    return route.fulfill({
      json: {
        success: true,
        data: {
          redeem_type: 'subscription',
          credits: 0,
          plan_id: 1,
          plan_title: '迁入保留月卡',
          user_subscription_id: '9007199254740993',
        },
      },
    })
  })
  await page.goto('/wallet?section=redemption')
  await page.getByLabel('兑换码', { exact: true }).fill('same-subscription-code')
  await page.getByRole('button', { name: '兑换', exact: true }).click()
  await expect(page.getByRole('status')).toHaveText(
    '已兑换订阅：迁入保留月卡（订阅 9007199254740993）',
  )
  await page.getByRole('button', { name: '兑换', exact: true }).click()
  await expect(page.getByRole('status')).toHaveCount(1)
  await expect(page.getByRole('status')).toHaveText(
    '已兑换订阅：迁入保留月卡（订阅 9007199254740993）',
  )
  expect(keys).toEqual(['same-subscription-code', 'same-subscription-code'])
  await expect(page.getByText('已兑换 0 credits', { exact: true })).toHaveCount(0)
  await page.getByRole('tab', { name: '我的订阅', exact: true }).click()
  await expect(page.getByRole('cell', { name: '开发者月卡', exact: true })).toBeVisible()
})

test('administrator issues a subscription code with exact plan ID and zero monetary credits', async ({
  page,
}) => {
  await page.route('**/api/subscription/admin/plans', (route) =>
    route.fulfill({
      json: { success: true, data: [{ id: '9007199254740993', name: '内部保留套餐' }] },
    }),
  )
  await page.route('**/api/redemption/', (route) =>
    route.fulfill({
      json: {
        success: true,
        data:
          route.request().method() === 'GET'
            ? []
            : {
                id: 1,
                name: '订阅兑换',
                credits: 0,
                redeem_type: 'subscription',
                plan_id: '9007199254740993',
                plan_title: '内部保留套餐',
                state: 'active',
                expires_at: null,
                key: 'issued-subscription-code',
              },
      },
    }),
  )
  await page.goto('/redemptions')
  await expect(
    page.getByRole('combobox', { name: '兑换类型', exact: true }).locator('option'),
  ).toHaveText(['钱包额度', '订阅套餐'])
  await page.getByLabel('名称', { exact: true }).fill('订阅兑换')
  await page.getByRole('combobox', { name: '兑换类型', exact: true }).selectOption('subscription')
  await page
    .getByRole('combobox', { name: '兑换套餐', exact: true })
    .selectOption('9007199254740993')
  const sent = page.waitForRequest(
    (request) =>
      new URL(request.url()).pathname === '/api/redemption/' && request.method() === 'POST',
  )
  await page.getByRole('button', { name: '发行兑换码', exact: true }).click()
  const raw = (await sent).postData()
  expect(raw).toContain('"plan_id":9007199254740993')
  expect(raw).toContain('"credits":0')
  expect(raw).toContain('"redeem_type":"subscription"')
  expect(raw).not.toContain('blind_box_quantity')
  await expect(page.getByText('issued-subscription-code', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '发行兑换码', exact: true })).toBeDisabled()
})
