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
  await page.goto('/wallet')
  await page.getByLabel('兑换码', { exact: true }).fill('same-subscription-code')
  await page.getByRole('button', { name: '兑换', exact: true }).click()
  await expect(page.getByRole('status')).toHaveText(
    '已兑换订阅：迁入保留月卡（订阅 9007199254740993）',
  )
  await expect(page.getByRole('cell', { name: '开发者月卡', exact: true })).toBeVisible()
  await page.getByRole('button', { name: '兑换', exact: true }).click()
  await expect(page.getByRole('status')).toHaveCount(1)
  await expect(page.getByRole('status')).toHaveText(
    '已兑换订阅：迁入保留月卡（订阅 9007199254740993）',
  )
  expect(keys).toEqual(['same-subscription-code', 'same-subscription-code'])
  await expect(page.getByText('已兑换 0 credits', { exact: true })).toHaveCount(0)
})

test('blind-box redemption reports quantity and opens the refreshed inventory', async ({
  page,
}) => {
  let available = 2
  await page.route('**/api/commerce/redemptions/redeem', (route) => {
    available = 5
    return route.fulfill({
      json: {
        success: true,
        data: {
          redeem_type: 'blind_box',
          credits: 0,
          blind_box_quantity: 3,
          blind_box_order_id: '9007199254740993',
        },
      },
    })
  })
  await page.route('**/api/blind-box/self', (route) =>
    route.fulfill({
      json: { success: true, data: { available_count: available, pools: [], props: [] } },
    }),
  )
  await page.goto('/blind-box')
  await expect(page.locator('.balance-ledger dd')).toHaveText('2')
  await page.getByRole('link', { name: '钱包', exact: true }).click()
  await page.getByLabel('兑换码', { exact: true }).fill('blind-box-code')
  await page.getByRole('button', { name: '兑换', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('已兑换 3 个盲盒。')
  await expect(page.getByText('已兑换 0 credits', { exact: true })).toHaveCount(0)
  await page.getByRole('link', { name: '查看盲盒', exact: true }).click()
  await expect(page.getByRole('heading', { name: '盲盒', exact: true, level: 1 })).toBeVisible()
  await expect(page.locator('.balance-ledger dd')).toHaveText('5')
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

test('administrator limits box quantity and issues only the selected entitlement', async ({
  page,
}) => {
  await page.route('**/api/redemption/', (route) =>
    route.fulfill({
      json: {
        success: true,
        data:
          route.request().method() === 'GET'
            ? []
            : {
                id: 2,
                name: '盲盒兑换',
                credits: 0,
                redeem_type: 'blind_box',
                blind_box_quantity: 100,
                state: 'active',
                expires_at: null,
                key: 'issued-box-code',
              },
      },
    }),
  )
  await page.goto('/redemptions')
  await page.getByLabel('名称', { exact: true }).fill('盲盒兑换')
  await page.getByRole('combobox', { name: '兑换类型', exact: true }).selectOption('blind_box')
  await page.getByLabel('盲盒数量', { exact: true }).fill('101')
  await page.getByRole('button', { name: '发行兑换码', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('盲盒数量须为 1 至 100 的整数')
  await page.getByLabel('盲盒数量', { exact: true }).fill('1.5')
  await page.getByRole('button', { name: '发行兑换码', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('盲盒数量须为 1 至 100 的整数')
  await page.getByLabel('盲盒数量', { exact: true }).fill('100')
  const sent = page.waitForRequest(
    (request) =>
      new URL(request.url()).pathname === '/api/redemption/' && request.method() === 'POST',
  )
  await page.getByRole('button', { name: '发行兑换码', exact: true }).click()
  expect((await sent).postDataJSON()).toEqual({
    name: '盲盒兑换',
    credits: 0,
    expires_at: null,
    redeem_type: 'blind_box',
    blind_box_quantity: 100,
  })
  await expect(page.getByText('issued-box-code', { exact: true })).toBeVisible()
})
