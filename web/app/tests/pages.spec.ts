import { test, expect } from '@playwright/test'
import { fixtureAPI } from './fixtures'

test.beforeEach(async ({ page }) => fixtureAPI(page))

for (const [path, heading] of [
  ['/sign-in', '登录'],
  ['/sign-up', '注册'],
  ['/dashboard', '仪表板'],
  ['/keys', 'API Key'],
  ['/wallet', '钱包'],
  ['/usage-logs', '使用日志'],
  ['/orders', '订单'],
  ['/group-buy', '拼团'],
  ['/blind-box', '盲盒'],
  ['/channels', '渠道'],
  ['/users', '用户'],
  ['/settings', '系统设置'],
  ['/community', '社区'],
  ['/profile', '个人资料'],
  ['/channel-market', '渠道市场'],
  ['/my-channels', '我的渠道'],
  ['/market-admin', '渠道市场审核'],
  ['/transfers', '钱包转账'],
  ['/invoices', '发票'],
  ['/redemptions', '兑换码管理'],
  ['/subscriptions', '套餐管理'],
]) {
  test(`${path} loads and fits the viewport`, async ({ page }) => {
    const errors: string[] = []
    page.on('pageerror', (error) => errors.push(error.message))
    await page.goto(path)
    await expect(page.getByRole('heading', { name: heading, exact: true, level: 1 })).toBeVisible()
    expect(errors).toEqual([])
    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth > window.innerWidth,
    )
    expect(overflow).toBe(false)
    if (process.env.CAPTURE_UI === '1')
      await page.screenshot({ path: test.info().outputPath('screen.png'), fullPage: true })
  })
}

test('authentication failure returns to sign in', async ({ page }) => {
  await page.route('**/api/user/self', (route) =>
    route.fulfill({ status: 401, json: { success: false, message: '身份验证失败' } }),
  )
  await page.route('**/api/user/refresh', (route) =>
    route.fulfill({ status: 401, json: { success: false, message: '身份验证失败' } }),
  )
  await page.goto('/dashboard')
  await expect(page.getByRole('heading', { name: '登录', exact: true })).toBeVisible()
})

test('creates a key and reveals the new value once', async ({ page }) => {
  await page.goto('/keys')
  await page.getByRole('button', { name: '创建', exact: true }).click()
  await page.getByLabel('名称', { exact: true }).fill('浏览器测试')
  await page.getByRole('button', { name: '创建', exact: true }).last().click()
  await expect(page.getByText('sk-created-test-only')).toBeVisible()
  await page.getByRole('button', { name: '关闭', exact: true }).click()
  await expect(page.getByText('sk-created-test-only')).toHaveCount(0)
})

test('shows exact credits and lazy-loads the English language pack', async ({ page }) => {
  await page.goto('/dashboard')
  await expect(page.getByText('123.456789 credits', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'EN', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Dashboard', exact: true })).toBeVisible()
})

test('login errors remain visible and allow another attempt', async ({ page }) => {
  await page.route('**/api/user/login', (route) =>
    route.fulfill({ status: 401, json: { success: false, message: '身份验证失败' } }),
  )
  await page.goto('/sign-in')
  await page.getByLabel('用户名', { exact: true }).fill('operator')
  await page.getByLabel('密码', { exact: true }).fill('incorrect-password')
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('身份验证失败')
  await expect(page.getByRole('button', { name: '登录', exact: true })).toBeEnabled()
})

test('channel creation sends models, group and encrypted-at-rest credential input', async ({
  page,
}) => {
  await page.goto('/channels')
  await page.getByRole('button', { name: '创建', exact: true }).click()
  await page.getByLabel('名称', { exact: true }).first().fill('测试渠道')
  await page.getByLabel('模型', { exact: true }).fill('gpt-4o, claude-sonnet-4')
  await page.getByLabel('凭据', { exact: true }).fill('sk-browser-fixture-only')
  const sent = page.waitForRequest(
    (request) =>
      new URL(request.url()).pathname === '/api/catalog/channels' && request.method() === 'POST',
  )
  await page.getByRole('button', { name: '保存', exact: true }).click()
  const body = (await sent).postDataJSON()
  expect(body.models).toEqual(['gpt-4o', 'claude-sonnet-4'])
  expect(body.groups).toEqual(['default'])
  expect(body.credentials).toEqual([{ kind: 'api_key', secret: 'sk-browser-fixture-only' }])
})

test('balance adjustment uses exact signed amounts and stable operation identifiers', async ({
  page,
}) => {
  await page.goto('/users')
  await page.getByRole('button', { name: '余额调整', exact: true }).click()
  await page.getByLabel('钱包账户 ID', { exact: true }).fill('1')
  await page.getByLabel('调整 credits', { exact: true }).fill('-1.000001')
  await page.getByLabel('调整原因', { exact: true }).fill('测试账本调整')
  const sent = page.waitForRequest(
    (request) =>
      new URL(request.url()).pathname === '/api/billing/adjustments' && request.method() === 'POST',
  )
  await page.getByRole('button', { name: '入账', exact: true }).click()
  const body = (await sent).postDataJSON()
  expect(body.amount_micro).toBe(-1000001)
  expect(body.operation_id).toMatch(/^[a-f0-9-]{36}$/)
  await expect(page.getByRole('status')).toHaveText('已入账')
})

test('blind-box retry reuses the operation identifier after a server failure', async ({ page }) => {
  const identifiers: string[] = []
  await page.route('**/api/blind-box/inventory/purchase', async (route) => {
    identifiers.push(route.request().postDataJSON().request_id)
    await route.fulfill(
      identifiers.length === 1
        ? { status: 503, json: { success: false, message: '账本服务暂时不可用' } }
        : { json: { success: true, data: { id: 1 } } },
    )
  })
  page.on('dialog', (dialog) => dialog.accept())
  await page.goto('/blind-box')
  await page.getByRole('button', { name: '购买', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('账本服务暂时不可用')
  await page.getByRole('button', { name: '购买', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveCount(0)
  expect(identifiers).toHaveLength(2)
  expect(identifiers[0]).toBe(identifiers[1])
})

test('ordinary users cannot open administrator pages', async ({ page }) => {
  await page.route('**/api/user/self', (route) =>
    route.fulfill({
      json: { success: true, data: { id: 2, username: 'member', role: 'user', status: 'active' } },
    }),
  )
  await page.goto('/users')
  await expect(page.getByRole('alert')).toHaveText('无权执行此操作')
  await expect(page.getByRole('button', { name: '余额调整', exact: true })).toHaveCount(0)
})

test('external account links use only configured provider slugs', async ({ page }) => {
  await page.route('**/api/oauth/providers', (route) =>
    route.fulfill({
      json: { success: true, data: [{ slug: 'company', name: '企业账号', icon: '' }] },
    }),
  )
  await page.goto('/sign-in')
  await expect(page.getByRole('link', { name: '企业账号', exact: true })).toHaveAttribute(
    'href',
    '/api/oauth/company',
  )
  await expect(page.getByRole('link', { name: 'GitHub', exact: true })).toHaveCount(0)
  await page.goto('/profile')
  await expect(page.getByRole('link', { name: '企业账号', exact: true })).toHaveAttribute(
    'href',
    '/api/oauth/company?bind=true',
  )
})
