import { test, expect } from '@playwright/test'
import { fixtureAPI, plan } from './fixtures'

test.beforeEach(async ({ page }) => fixtureAPI(page))

test('selects a configured provider and sends the smallest currency amount', async ({ page }) => {
  await page.route('**/api/commerce/providers', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: [{ provider: 'xunhu', currency: 'usd', credits_per_minor: 100000 }],
      },
    }),
  )
  await page.route('**/api/commerce/orders', (route) =>
    route.request().method() === 'POST'
      ? route.fulfill({ json: { success: true, data: { payment_url: '/orders' } } })
      : route.fulfill({ json: { success: true, data: [] } }),
  )
  await page.goto('/wallet')
  await page.getByLabel('支付金额', { exact: true }).fill('1.23')
  const sent = page.waitForRequest(
    (request) =>
      new URL(request.url()).pathname === '/api/commerce/orders' && request.method() === 'POST',
  )
  await page.getByRole('button', { name: '前往支付', exact: true }).click()
  expect((await sent).postDataJSON()).toMatchObject({ provider: 'xunhu', amount_minor: 123 })
  await expect(page.getByRole('heading', { name: '订单', exact: true })).toBeVisible()
})

test('redemption failure permits a corrected code', async ({ page }) => {
  let count = 0
  await page.route('**/api/commerce/redemptions/redeem', (route) =>
    route.fulfill(
      ++count === 1
        ? { status: 404, json: { success: false, message: '兑换码不存在' } }
        : { json: { success: true, data: { redeem_type: 'credits', credits: 1234567 } } },
    ),
  )
  await page.goto('/wallet')
  await page.getByLabel('兑换码', { exact: true }).fill('invalid-code')
  await page.getByRole('button', { name: '兑换', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('兑换码不存在')
  await page.getByLabel('兑换码', { exact: true }).fill('valid-code')
  await page.getByRole('button', { name: '兑换', exact: true }).click()
  await expect(page.getByRole('status')).toHaveText('已兑换 1.234567 credits')
})

test('payment-password correction retries the same transfer', async ({ page }) => {
  const bodies: { request_id: string; amount_micro: number; payment_password: string }[] = []
  await page.route('**/api/wallet/transfers/recipients/ABC123', (route) =>
    route.fulfill({
      json: { success: true, data: { external_id: 'ABC123', display_name_masked: '收**' } },
    }),
  )
  await page.route('**/api/wallet/transfers', async (route) => {
    if (route.request().method() === 'GET') return route.fallback()
    bodies.push(route.request().postDataJSON())
    await route.fulfill(
      bodies.length === 1
        ? { status: 400, json: { success: false, message: '支付密码错误' } }
        : { json: { success: true, data: {} } },
    )
  })
  await page.goto('/transfers')
  await page.getByLabel('收款人 ID', { exact: true }).fill('ABC123')
  await page.getByLabel('转账 credits', { exact: true }).fill('10')
  await page.getByLabel('支付密码', { exact: true }).fill('wrong-password')
  await page.getByRole('button', { name: '核对收款人', exact: true }).click()
  await page.getByRole('button', { name: '确认转账', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('支付密码错误')
  await page.getByLabel('支付密码', { exact: true }).fill('correct-password')
  await page.getByRole('button', { name: '重试同一笔转账', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: '转账成功。' })).toHaveText('转账成功。')
  expect(bodies.map((body) => body.amount_micro)).toEqual([10000000, 10000000])
  expect(bodies[0].request_id).toBe(bodies[1].request_id)
  expect(bodies[1].payment_password).toBe('correct-password')
})

test('market selection binds an owned key to the chosen group', async ({ page }) => {
  await page.goto('/channel-market')
  await page.getByRole('button', { name: '选择渠道', exact: true }).click()
  await page.getByRole('combobox', { name: 'API Key', exact: true }).selectOption('1')
  const sent = page.waitForRequest('**/api/marketplace/groups/group-test/bind-token')
  await page.getByRole('button', { name: '绑定 Key', exact: true }).click()
  expect((await sent).postDataJSON()).toEqual({ token_id: 1 })
  await expect(page.getByRole('status')).toHaveText('分组已绑定，可前往对话测试。')
})

test('owner multiplier sends the selected channel and exact user ID', async ({ page }) => {
  await page.goto('/my-channels')
  await page.getByRole('tab', { name: '渠道与访问', exact: true }).click()
  await page.getByRole('button', { name: '访问管理', exact: true }).click()
  await page.getByLabel('用户 ID', { exact: true }).fill('9223372036854775807')
  await page.getByLabel('专属倍率', { exact: true }).fill('0.75')
  const sent = page.waitForRequest('**/api/marketplace/channels/349/user-multiplier')
  await page.getByRole('button', { name: '设置用户倍率', exact: true }).click()
  expect((await sent).postData()).toBe('{"user_id":9223372036854775807,"multiplier":0.75}')
})

test('administrator saves exact plan credits and calendar duration', async ({ page }) => {
  await page.goto('/subscriptions')
  await page.getByRole('button', { name: '新增套餐', exact: true }).click()
  await page.getByLabel('套餐名称', { exact: true }).fill('季度方案')
  await page.getByLabel('套餐 credits', { exact: true }).fill('123.000001')
  await page.getByRole('combobox', { name: '有效期单位', exact: true }).selectOption('month')
  await page.getByLabel('有效期数量', { exact: true }).fill('3')
  const sent = page.waitForRequest(
    (request) =>
      new URL(request.url()).pathname === '/api/subscription/admin/plans' &&
      request.method() === 'POST',
  )
  await page.getByRole('button', { name: '保存', exact: true }).click()
  expect((await sent).postDataJSON()).toMatchObject({
    credits: 123000001,
    duration_unit: 'month',
    duration_value: 3,
    reset_period: 'never',
  })
})

test('legacy community URL leads to the external community', async ({ page }) => {
  await page.route('https://community.codegoai.com/**', (route) =>
    route.fulfill({ contentType: 'text/html', body: '<h1>CodeGo community</h1>' }),
  )
  await page.goto('/community')
  await expect(page).toHaveURL('https://community.codegoai.com/')
  await expect(page.getByRole('heading', { name: 'CodeGo community' })).toBeVisible()
})

test('editing a plan preserves zero credits fields and exact group rewards', async ({ page }) => {
  await page.goto('/subscriptions')
  await page.getByRole('button', { name: '编辑', exact: true }).click()
  await page.getByLabel('二人团奖励 credits', { exact: true }).fill('2.000001')
  const sent = page.waitForRequest(
    (request) =>
      new URL(request.url()).pathname === '/api/subscription/admin/plans/1' &&
      request.method() === 'PUT',
  )
  await page.getByRole('button', { name: '保存', exact: true }).click()
  expect((await sent).postDataJSON()).toMatchObject({
    period_credits: 0,
    fuel_unit_price_micro: 0,
    group_buy_bonus2_micro: 2000001,
  })
})

test('discount props convert on confirmation and cannot be manually activated', async ({
  page,
}) => {
  await page.route('**/api/blind-box/self', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          available_count: 0,
          pools: [],
          props: [
            {
              id: '9223372036854775807',
              kind: 'subscription_discount',
              title: '订阅优惠卡',
              status: 'available',
              multiplier_ppm: 1000000,
              remaining_seconds: 0,
              prop_type: 'subscription_discount_50',
              discount_rate_ppm: 500000,
              max_discount_micro: 0,
              used_discount_micro: 0,
            },
            {
              id: 2,
              kind: 'subscription',
              title: '已使用月卡',
              status: 'used',
              multiplier_ppm: 1000000,
              remaining_seconds: 0,
              prop_type: '',
              discount_rate_ppm: 0,
              max_discount_micro: 0,
              used_discount_micro: 0,
            },
          ],
        },
      },
    }),
  )
  await page.goto('/blind-box')
  await expect(page.getByText('购买套餐时自动使用', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '使用', exact: true })).toBeDisabled()
  const sent = page.waitForRequest('**/api/blind-box/props/9223372036854775807/convert')
  await page.getByRole('button', { name: '转换为九折充值卡', exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: '确认', exact: true }).click()
  expect((await sent).postDataJSON()).toEqual({ target_type: 'topup_discount_90' })
})

test('subscription renewal retries one request and shows the server order amount', async ({
  page,
}) => {
  const bodies: { request_id: string; target_subscription_id: number; plan_id: number }[] = []
  await page.route('**/api/packages/renew', async (route) => {
    bodies.push(route.request().postDataJSON())
    await route.fulfill(
      bodies.length === 1
        ? { status: 503, json: { success: false, message: '支付服务暂不可用' } }
        : {
            json: {
              success: true,
              data: {
                trade_no: 'renew-test',
                amount_minor: 456,
                currency: 'usd',
                state: 'created',
                payment_url: '/orders',
              },
            },
          },
    )
  })
  await page.goto('/wallet')
  await page.getByRole('combobox', { name: '需要续期或升级的订阅', exact: true }).selectOption('1')
  await page.getByRole('button', { name: '续期', exact: true }).click()
  await page.getByRole('button', { name: '创建订单', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('支付服务暂不可用')
  await page.getByRole('button', { name: '重试同一笔订单', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('USD 4.56')
  expect(bodies.map((body) => body.target_subscription_id)).toEqual([1, 1])
  expect(bodies[0].request_id).toBe(bodies[1].request_id)
})

test('owned subscriptions retain server quote and conversion controls after their plan is delisted', async ({
  page,
}) => {
  await page.route('**/api/subscription/plans', (route) =>
    route.fulfill({ json: { success: true, data: [] } }),
  )
  await page.route('**/api/subscription/fuel/quote', (route) =>
    route.fulfill({ status: 400, json: { success: false, message: '该月卡不支持燃料' } }),
  )
  await page.goto('/wallet')
  await expect(page.getByRole('combobox', { name: '需要转换的月卡', exact: true })).toBeVisible()
  await page.getByLabel('购买燃料 credits', { exact: true }).fill('10')
  const sent = page.waitForRequest('**/api/subscription/fuel/quote')
  await page.getByRole('button', { name: '获取燃料报价', exact: true }).click()
  expect((await sent).postDataJSON()).toMatchObject({ subscription_id: 1, credits: 10000000 })
  await expect(page.getByRole('alert')).toHaveText('该月卡不支持燃料')
})

test('refund processing can synchronize to its completed result', async ({ page }) => {
  await page.route('**/api/wallet/refunds/eligible', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          items: [
            {
              trade_no: 'refund-order',
              order_type: 'topup',
              remaining_quota: 10000000,
              refund_amount_minor: 980,
              refund_status: '',
              refundable: true,
              created_at: 1790755200,
            },
          ],
        },
      },
    }),
  )
  await page.route('**/api/wallet/refunds', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: { refund_no: 'refund-test', status: 'processing', refund_amount_minor: 980 },
      },
    }),
  )
  await page.route('**/api/wallet/refunds/refund-test/sync', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: { refund_no: 'refund-test', status: 'success', refund_amount_minor: 980 },
      },
    }),
  )
  await page.goto('/wallet')
  await page.getByRole('button', { name: '申请退款', exact: true }).click()
  const sent = page.waitForRequest('**/api/wallet/refunds')
  await page.getByRole('button', { name: '确认申请退款', exact: true }).click()
  expect((await sent).postDataJSON()).toEqual({ order_type: 'topup', trade_no: 'refund-order' })
  await expect(page.getByRole('status')).toContainText('退款处理中')
  await page.getByRole('button', { name: '查询支付平台结果', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('退款完成')
})

test('fuel uses a server quote and rejects an amount outside the configured step', async ({
  page,
}) => {
  await page.route('**/api/subscription/plans', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: [
          {
            ...plan,
            id: 1,
            name: '燃料月卡',
            enabled: true,
            credits: 100000000,
            period_credits: 0,
            price_minor: 1000,
            currency: 'usd',
            duration_unit: 'month',
            duration_value: 1,
            plan_type: 'monthly',
            fuel_enabled: true,
            fuel_min_credits: 1000000,
            fuel_credit_step: 1000000,
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
          min_credits: 1000000,
          credit_step: 1000000,
          amount_minor: 17,
          currency: 'usd',
          expires_at: '2026-10-30T08:00:00Z',
        },
      },
    }),
  )
  await page.route('**/api/subscription/fuel/purchase', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          trade_no: 'fuel-test',
          amount_minor: 17,
          currency: 'usd',
          state: 'created',
          payment_url: '/orders',
        },
      },
    }),
  )
  await page.goto('/wallet')
  await page.getByLabel('购买燃料 credits', { exact: true }).fill('1.5')
  await page.getByRole('button', { name: '获取燃料报价', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('步长')
  await page.getByLabel('购买燃料 credits', { exact: true }).fill('2')
  await page.getByRole('button', { name: '获取燃料报价', exact: true }).click()
  await expect(page.getByText(/订阅 1 增加 2 credits，应付 USD 0.17/)).toBeVisible()
  const sent = page.waitForRequest('**/api/subscription/fuel/purchase')
  await page.getByRole('button', { name: '确认创建燃料订单', exact: true }).click()
  expect((await sent).postDataJSON()).toMatchObject({
    subscription_id: 1,
    credits: 2000000,
    provider: 'stripe',
  })
})

test('monthly conversion retries the same operation and reports distinct credited value', async ({
  page,
}) => {
  const bodies: { request_id: string; conversion_percent: number }[] = []
  await page.route('**/api/subscription/self/claude-conversions', async (route) => {
    if (route.request().method() === 'GET')
      return route.fulfill({ json: { success: true, data: [] } })
    bodies.push(route.request().postDataJSON())
    await route.fulfill(
      bodies.length === 1
        ? { status: 409, json: { success: false, message: '消费结算尚未完成' } }
        : { json: { success: true, data: { source_credits: 1000000, target_credits: 100000 } } },
    )
  })
  await page.goto('/wallet')
  await page.getByLabel('转换比例 %', { exact: true }).fill('1')
  await page.getByRole('button', { name: '核对额度转换', exact: true }).click()
  await page.getByRole('button', { name: '确认转换额度', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('消费结算尚未完成')
  await page.getByRole('button', { name: '重试同一次转换', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: '转换完成' })).toContainText(
    '月卡减少 1 credits，钱包到账 0.1 credits',
  )
  expect(bodies[0].request_id).toBe(bodies[1].request_id)
})
