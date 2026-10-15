import { test, expect } from '@playwright/test'
import { fixtureAPI } from './fixtures'

test.beforeEach(async ({ page }) => fixtureAPI(page))

test('new key blank restrictions are unrestricted nulls', async ({ page }) => {
  await page.goto('/keys')
  await page.getByRole('button', { name: '创建', exact: true }).click()
  await page.getByLabel('名称', { exact: true }).fill('无限制测试密钥')
  const sent = page.waitForRequest(
    (request) => new URL(request.url()).pathname === '/api/token/' && request.method() === 'POST',
  )
  await page.getByRole('dialog').getByRole('button', { name: '创建', exact: true }).click()
  expect((await sent).postDataJSON()).toMatchObject({
    allowed_models: null,
    allowed_cidrs: null,
    group: null,
  })
})

test('editing a deny-all key preserves empty restrictions and its group', async ({ page }) => {
  await page.route('**/api/token/', (route) =>
    route.request().method() !== 'GET'
      ? route.fallback()
      : route.fulfill({
          json: {
            success: true,
            data: [
              {
                id: 1,
                name: '受限密钥',
                status: 'active',
                key_prefix: 'sk-test',
                group: 'private-granted',
                allowed_models: [],
                allowed_cidrs: [],
                expires_at: null,
              },
            ],
          },
        }),
  )
  await page.goto('/keys')
  await page.getByRole('button', { name: '编辑', exact: true }).first().click()
  await page.getByLabel('名称', { exact: true }).fill('重命名受限密钥')
  await expect(page.getByLabel('模型访问', { exact: true })).toHaveValue('none')
  await expect(page.getByLabel('网络访问', { exact: true })).toHaveValue('none')
  const sent = page.waitForRequest(
    (request) => new URL(request.url()).pathname === '/api/token/' && request.method() === 'PUT',
  )
  await page.getByRole('dialog').getByRole('button', { name: '保存', exact: true }).click()
  expect((await sent).postDataJSON()).toMatchObject({
    allowed_models: [],
    allowed_cidrs: [],
    group: 'private-granted',
  })
})

test('create key selects an owned route pool and sends exact spending controls', async ({
  page,
}) => {
  await page.route('**/api/user/self/groups', (route) =>
    route.fulfill({ json: { success: true, data: ['default', 'personal-pool'] } }),
  )
  await page.route('**/api/marketplace/route-pools', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: [
          { id: 'owned-1', name: '生产池', token_group: 'personal-pool' },
          { id: 'foreign-2', name: '其他池', token_group: 'foreign-pool' },
        ],
      },
    }),
  )
  await page.goto('/keys')
  await page.getByRole('button', { name: '创建', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('名称', { exact: true }).fill('高精度生产密钥')
  await expect(dialog.getByRole('option', { name: '生产池', exact: true })).toBeAttached()
  await expect(dialog.getByRole('option', { name: '其他池', exact: true })).toHaveCount(0)
  await dialog.getByLabel('分组 / 路由池', { exact: true }).selectOption('personal-pool')
  await dialog.getByLabel('限制 Key 额度', { exact: true }).check()
  await dialog.getByLabel('Key 剩余额度（credits）', { exact: true }).fill('1234567890123.456789')
  await dialog.getByLabel('市场分组最大倍率', { exact: true }).fill('1.234567')
  await dialog.getByLabel('跨分组重试', { exact: true }).check()
  const sent = page.waitForRequest(
    (request) => new URL(request.url()).pathname === '/api/token/' && request.method() === 'POST',
  )
  await dialog.getByRole('button', { name: '创建', exact: true }).click()
  const payload = (await sent).postData()!
  expect(payload).toContain('"budget_micro_credits":1234567890123456789')
  expect(payload).toContain('"max_marketplace_multiplier_ppm":1234567')
  expect(payload).toContain('"group":"personal-pool"')
  expect(payload).toContain('"cross_group_retry":true')
})

test('status toggle cannot replenish a key allowance', async ({ page }) => {
  await page.goto('/keys')
  const sent = page.waitForRequest(
    (request) => new URL(request.url()).pathname === '/api/token/' && request.method() === 'PUT',
  )
  await page.getByRole('button', { name: '停用', exact: true }).first().click()
  const request = await sent
  expect(new URL(request.url()).searchParams.get('status_only')).toBe('true')
  expect(request.postDataJSON()).toEqual({ id: 1, status: 'disabled' })
})

test('key policy drawer keeps controls within the viewport', async ({ page }, testInfo) => {
  await page.goto('/keys')
  await page.getByRole('button', { name: '创建', exact: true }).click()
  await page.getByLabel('限制 Key 额度', { exact: true }).check()
  const dialog = page.getByRole('dialog')
  const size = await dialog.evaluate((element) => ({
    width: element.getBoundingClientRect().width,
    scrollWidth: element.scrollWidth,
    clientWidth: element.clientWidth,
    viewport: innerWidth,
  }))
  expect(size.width).toBeLessThanOrEqual(size.viewport)
  expect(size.scrollWidth).toBeLessThanOrEqual(size.clientWidth)
  await page.screenshot({ path: testInfo.outputPath('key-policy.png'), fullPage: true })
})
