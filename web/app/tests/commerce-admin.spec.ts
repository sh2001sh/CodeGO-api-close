import { test, expect } from '@playwright/test'
import { fixtureAPI } from './fixtures'

const pool = {
  id: 1,
  name: '日常盲盒',
  enabled: true,
  price_micro: 1000000,
  daily_limit: 5,
  monthly_limit: 20,
  daily_open_limit: 7,
  scope: 'standard',
  rewards: [
    {
      kind: 'credits',
      title: '浮动额度',
      weight: 1,
      amount_micro: 0,
      minimum_micro: 1000000,
      maximum_micro: 2000000,
      step_micro: 100000,
      multiplier_ppm: 0,
      duration_seconds: 0,
      plan_id: 0,
      wallet_type: 'bonus',
      reward_tier: 'rare',
      legacy_reward_type: 'credits',
    },
  ],
  guarantees: {
    first: [
      {
        kind: 'topup_discount',
        title: '折扣券',
        weight: 1,
        amount_micro: 0,
        multiplier_ppm: 0,
        duration_seconds: 0,
        plan_id: 0,
        discount_rate_ppm: 900000,
        max_discount_micro: 50000000,
        prop_type: 'topup_discount_90',
      },
    ],
  },
  standard_policy: {
    enabled: true,
    subscription_probability_ppb: 100,
    subscription_plan_id: 3,
    first_purchase_minimum_micro: 1000000,
    pity_minimum_micro: 1000000,
    low_reward_threshold_micro: 500000,
    pity_after: 10,
  },
}

test.beforeEach(async ({ page }) => {
  await fixtureAPI(page)
  await page.route('**/api/blind-box/admin/pools', (route) =>
    route.fulfill({
      json: { success: true, data: route.request().method() === 'GET' ? [pool] : pool },
    }),
  )
})

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

test('blind-box pool editor saves exact micro credits and reward payload', async ({ page }) => {
  const sent = page.waitForRequest(
    (request) =>
      new URL(request.url()).pathname === '/api/blind-box/admin/pools' &&
      request.method() === 'PUT',
  )
  await page.route('**/api/blind-box/admin/pools', (route) =>
    route.request().method() === 'GET'
      ? route.fulfill({ json: { success: true, data: [pool] } })
      : route.fulfill({
          json: {
            success: true,
            data: {
              id: 1,
              name: '日常盲盒',
              enabled: true,
              price_micro: 2000000,
              daily_limit: 5,
              monthly_limit: 0,
              daily_open_limit: 0,
              standard_policy: {
                enabled: false,
                subscription_probability_ppb: 0,
                subscription_plan_id: 0,
                first_purchase_minimum_micro: 0,
                pity_minimum_micro: 0,
                low_reward_threshold_micro: 0,
                pity_after: 0,
              },
              rewards: [
                {
                  kind: 'credits',
                  title: '2 credits',
                  weight: 1,
                  amount_micro: 2000000,
                  multiplier_ppm: 0,
                  duration_seconds: 0,
                  plan_id: 0,
                },
              ],
              guarantees: {},
              scope: 'credits',
            },
          },
        }),
  )
  await page.goto('/admin/blind-box')
  await page.getByRole('button', { name: '编辑', exact: true }).click()
  await page.getByLabel('单个购买价格 credits', { exact: true }).fill('2')
  await page.getByRole('button', { name: '保存盲盒池', exact: true }).click()
  const request = await sent
  expect(request.postDataJSON()).toMatchObject({
    id: 1,
    name: '日常盲盒',
    price_micro: 2000000,
    rewards: pool.rewards,
    guarantees: pool.guarantees,
    standard_policy: pool.standard_policy,
    monthly_limit: 20,
    daily_open_limit: 7,
  })
})

test('disabled blind-box pools remain selectable and can be re-enabled without changing rewards', async ({
  page,
}) => {
  await page.route('**/api/blind-box/admin/pools', (route) =>
    route.fulfill({
      json: {
        success: true,
        data:
          route.request().method() === 'GET'
            ? [{ ...pool, enabled: false }]
            : { ...pool, enabled: true },
      },
    }),
  )
  await page.goto('/admin/blind-box')
  await expect(page.getByRole('cell', { name: '日常盲盒' })).toBeVisible()
  await page.getByRole('button', { name: '编辑', exact: true }).click()
  await page.getByLabel('启用此池').check()
  const sent = page.waitForRequest(
    (request) =>
      new URL(request.url()).pathname === '/api/blind-box/admin/pools' &&
      request.method() === 'PUT',
  )
  await page.getByRole('button', { name: '保存盲盒池', exact: true }).click()
  expect((await sent).postDataJSON()).toMatchObject({ ...pool, enabled: true })
})

test('invalid blind-box JSON blocks saving and server errors keep the draft editable', async ({
  page,
}) => {
  let saves = 0
  await page.route('**/api/blind-box/admin/pools', (route) => {
    if (route.request().method() === 'GET')
      return route.fulfill({ json: { success: true, data: [pool] } })
    saves++
    return route.fulfill({ status: 503, json: { success: false, message: '保存暂不可用' } })
  })
  await page.goto('/admin/blind-box')
  await page.getByRole('button', { name: '编辑', exact: true }).click()
  const rewardJSON = page.getByRole('textbox', { name: '奖励配置 JSON', exact: true })
  const original = await rewardJSON.inputValue()
  await rewardJSON.fill('[')
  await page.getByRole('button', { name: '保存盲盒池', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('配置 JSON 格式无效')
  expect(saves).toBe(0)
  await rewardJSON.fill(original)
  await page.getByLabel('池名称', { exact: true }).fill('保留草稿')
  await page.getByRole('button', { name: '保存盲盒池', exact: true }).click()
  await expect(page.getByRole('alert').first()).toContainText('保存暂不可用')
  await expect(page.getByLabel('池名称', { exact: true })).toHaveValue('保留草稿')
  await expect(rewardJSON).toHaveValue(original)
  expect(saves).toBe(1)
})

test('blind-box user grants require a count and revoke goes through confirmation', async ({
  page,
}) => {
  await page.route('**/api/blind-box/admin/users/7/overview', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          available_count: 3,
          pools: [],
          props: [],
          pity: { opened: 0, small_progress: 0, big_progress: 0 },
          pity_states: {},
          zero_hour: {
            current_probability: 0,
            max_probability: 0,
            points: 0,
            point_cap: 0,
            active: false,
            active_until: 0,
          },
        },
      },
    }),
  )
  const granted = page.waitForRequest(
    (request) =>
      new URL(request.url()).pathname === '/api/blind-box/admin/users/7/grants' &&
      request.method() === 'POST',
  )
  await page.route('**/api/blind-box/admin/users/7/grants', (route) =>
    route.fulfill({
      json: { success: true, data: { id: 1, quantity: 2, unit_price_micro: 0, total_micro: 0 } },
    }),
  )
  const revoked = page.waitForRequest(
    (request) =>
      new URL(request.url()).pathname === '/api/blind-box/admin/users/7/revoke' &&
      request.method() === 'POST',
  )
  await page.route('**/api/blind-box/admin/users/7/revoke', (route) =>
    route.fulfill({ json: { success: true, data: [1, 2] } }),
  )
  await page.goto('/admin/blind-box')
  await page.getByRole('tab', { name: '用户发放', exact: true }).click()
  await page.getByLabel('用户 ID', { exact: true }).fill('7')
  await page.getByRole('button', { name: '查询', exact: true }).click()
  await page.getByLabel('发放数量', { exact: true }).fill('2')
  await page.getByRole('button', { name: '发放盲盒', exact: true }).click()
  expect((await granted).postDataJSON()).toMatchObject({ count: 2 })
  await expect(page.getByRole('status').filter({ hasText: '已发放' })).toBeVisible()
  await page.getByLabel('撤销数量', { exact: true }).fill('2')
  await page.getByRole('button', { name: '撤销盲盒', exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: '确认撤销', exact: true }).click()
  expect((await revoked).postDataJSON()).toMatchObject({ count: 2 })
  await expect(page.getByRole('status').filter({ hasText: '已撤销' })).toBeVisible()
})

test('packages page is a read-only overview that links back to the wallet checkout flow', async ({
  page,
}) => {
  await page.route('**/api/packages/public', (route) =>
    route.fulfill({ json: { success: true, data: [] } }),
  )
  await page.route('**/api/packages/my-subscription', (route) =>
    route.fulfill({ json: { success: true, data: [] } }),
  )
  await page.goto('/packages')
  await expect(page.getByRole('heading', { name: '套餐包', exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: '前往钱包购买', exact: true })).toHaveAttribute(
    'href',
    '/wallet',
  )
})
