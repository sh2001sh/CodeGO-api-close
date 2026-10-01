import { test, expect } from '@playwright/test'
import { fixtureAPI, plan } from './fixtures'

test.beforeEach(async ({ page }) => fixtureAPI(page))

test('editing preserves imported group, model caps, and other paid membership metadata exactly', async ({
  page,
}) => {
  await page.route('**/api/subscription/admin/plans', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: [
          {
            ...plan,
            upgrade_group: 'vip',
            membership_tier: 'paid',
            model_limits: { 'gpt-4o': '9007199254740993', 'claude-sonnet': '1000001' },
          },
        ],
      },
    }),
  )
  await page.goto('/subscriptions')
  await page.getByRole('button', { name: '编辑', exact: true }).click()
  await expect(page.getByLabel('订阅期间升级分组（留空保持原分组）', { exact: true })).toHaveValue(
    'vip',
  )
  await expect(page.getByLabel('模型消费上限 credits', { exact: true }).first()).toHaveValue(
    '9007199254.740993',
  )
  await expect(page.getByLabel('模型消费上限 credits', { exact: true }).last()).toHaveValue(
    '1.000001',
  )
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  )
  const sent = page.waitForRequest('**/api/subscription/admin/plans/1')
  await page.getByRole('button', { name: '保存', exact: true }).click()
  const request = await sent
  expect(request.method()).toBe('PUT')
  expect(request.postData()).toContain('"gpt-4o":9007199254740993')
  expect(request.postData()).toContain('"claude-sonnet":1000001')
  expect(request.postData()).toContain('"upgrade_group":"vip"')
  expect(request.postData()).toContain('"membership_tier":"paid"')
})

test('new model cap input rejects duplicates, excess precision, overflow and byte-length boundaries', async ({
  page,
}) => {
  await page.goto('/subscriptions')
  await page.getByRole('button', { name: '新增套餐', exact: true }).click()
  await page.getByLabel('套餐名称', { exact: true }).fill('模型额度方案')
  await page.getByRole('button', { name: '添加模型限制', exact: true }).click()
  await page.getByLabel('模型名称', { exact: true }).fill('gpt-4o')
  await page.getByLabel('模型消费上限 credits', { exact: true }).fill('1.0000001')
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('请输入最多六位小数的正数')
  await page.getByLabel('模型消费上限 credits', { exact: true }).fill('9223372036854.775808')
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('金额超出允许范围')
  await page.getByLabel('模型消费上限 credits', { exact: true }).fill('1')
  await page.getByRole('button', { name: '添加模型限制', exact: true }).click()
  await page.getByLabel('模型名称', { exact: true }).last().fill('gpt-4o')
  await page.getByLabel('模型消费上限 credits', { exact: true }).last().fill('0')
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('模型 gpt-4o 重复设置了额度')
  await page.getByLabel('模型名称', { exact: true }).last().fill('模'.repeat(67))
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('模型名称不能为空且最多 200 字节')
  await page.getByLabel('模型名称', { exact: true }).last().fill('no-cap')
  const sent = page.waitForRequest(
    (request) =>
      new URL(request.url()).pathname === '/api/subscription/admin/plans' &&
      request.method() === 'POST',
  )
  await page.getByRole('button', { name: '保存', exact: true }).click()
  expect((await sent).postDataJSON()).toMatchObject({
    upgrade_group: '',
    model_limits: { 'gpt-4o': 1000000, 'no-cap': 0 },
  })
})
