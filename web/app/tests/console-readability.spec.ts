import { expect, test } from '@playwright/test'
import { fixtureAPI } from './fixtures'

test.beforeEach(async ({ page }) => fixtureAPI(page))

test('dashboard discovers authorized models without sending a paid request or exposing the secret', async ({
  page,
}) => {
  let calls = 0
  page.on('request', (request) => {
    if (request.url().includes('/v1/chat/completions')) calls++
  })
  await page.route('**/api/user/self/groups', (route) =>
    route.fulfill({ json: { success: true, data: ['premium'] } }),
  )
  await page.route('**/api/token/*/key', (route) =>
    route.fulfill({ json: { success: true, data: { key: 'sk-fixture-private-value' } } }),
  )
  await page.route('**/v1/models', (route) =>
    route.fulfill({
      json: {
        data: [
          {
            id:
              route.request().headers()['x-codego-group'] === 'premium'
                ? 'premium-model'
                : 'current-model',
          },
        ],
      },
    }),
  )
  await page.goto('/dashboard')
  await page.getByRole('link', { name: 'API 接入', exact: true }).click()
  await page.getByText('查看调用示例', { exact: true }).click()
  await expect(page.locator('.overview-code-disclosure pre')).toContainText('YOUR_MODEL_ID')
  await page.getByLabel('API Key', { exact: true }).selectOption('1')
  await page.getByRole('button', { name: '使用此 Key', exact: true }).click()
  await expect(page.getByLabel('模型', { exact: true })).toHaveValue('current-model')
  await page.getByLabel('分组', { exact: true }).selectOption('premium')
  await expect(page.locator('.overview-code-disclosure pre')).toContainText('premium-model')
  await expect(page.locator('.overview-code-disclosure pre')).toContainText(
    'X-CodeGo-Group: premium',
  )
  await expect(page.locator('.overview-code-disclosure pre')).not.toContainText(
    'sk-fixture-private-value',
  )
  expect(await page.evaluate(() => Object.values(localStorage).join(''))).not.toContain(
    'sk-fixture-private-value',
  )
  expect(calls).toBe(0)
})

test('a refused model list is explicit and does not leave an obsolete fixed model in the example', async ({
  page,
}) => {
  let denied = true
  await page.route('**/api/user/self/groups', (route) =>
    route.fulfill({ json: { success: true, data: [] } }),
  )
  await page.route('**/api/token/*/key', (route) =>
    route.fulfill({ json: { success: true, data: { key: 'sk-fixture-private-value' } } }),
  )
  await page.route('**/v1/models', (route) =>
    route.fulfill(
      denied
        ? { status: 403, json: { error: { message: '所选分组没有权限' } } }
        : { json: { data: [{ id: 'restored-model' }] } },
    ),
  )
  await page.goto('/dashboard')
  await page.getByText('查看调用示例', { exact: true }).click()
  await page.getByLabel('API Key', { exact: true }).selectOption('1')
  await page.getByRole('button', { name: '使用此 Key', exact: true }).click()
  await expect(page.getByRole('alert').filter({ hasText: '所选分组没有权限' })).toBeVisible()
  await expect(page.getByLabel('模型', { exact: true })).toBeDisabled()
  await expect(page.locator('.overview-code-disclosure pre')).not.toContainText('gpt-4o-mini')
  denied = false
  await page
    .locator('.overview-code-disclosure')
    .getByRole('button', { name: '重试', exact: true })
    .click()
  await expect(page.getByLabel('模型', { exact: true })).toHaveValue('restored-model')
})

test('Arabic documentation keeps English fallback and a readable mobile layout', async ({
  page,
}) => {
  await page.addInitScript(() => localStorage.setItem('codego.locale', 'ar'))
  await page.goto('/docs?article=route-pools')
  await expect(page.locator('html')).toHaveAttribute('dir', 'rtl')
  await expect(page.locator('article')).toHaveAttribute('lang', 'en')
  await expect(page.locator('.docs-language-notice')).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  )
})

test('income graph supports touch selection, exact large amounts and keyboard navigation', async ({
  page,
}) => {
  const point = (timestamp: string, net: string) => ({
    timestamp,
    request_count: 1,
    success_count: 1,
    gross_micro: net,
    commission_micro: 0,
    fee_micro: 0,
    net_micro: net,
  })
  await page.route('**/api/marketplace/channels/mine/analytics**', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          from: '2026-10-01T00:00:00Z',
          to: '2026-10-08T00:00:00Z',
          bucket_seconds: 86400,
          summary: {
            request_count: 2,
            success_count: 2,
            consumer_count: 1,
            prompt_tokens: 10,
            completion_tokens: 20,
            consumer_micro: '0',
            gross_micro: '0',
            commission_micro: '0',
            fee_micro: '0',
            net_micro: '0',
            pending_income_micro: '0',
            released_income_micro: '0',
            reclaimed_income_micro: '0',
          },
          points: [
            point('2026-10-01T00:00:00Z', '9007199254740993'),
            point('2026-10-02T00:00:00Z', '7'),
          ],
          channels: [],
          settlements: [],
          settlements_truncated: false,
        },
      },
    }),
  )
  await page.goto('/my-channels')
  const chart = page.getByRole('group', { name: '所选时段净收入趋势', exact: true })
  await expect(chart).toBeVisible()
  await chart.getByRole('button').first().click()
  await expect(page.locator('.owner-income-point strong')).toHaveText(
    '9,007,199,254.740993 credits',
  )
  await chart.getByRole('button').first().press('ArrowRight')
  await expect(chart.getByRole('button').nth(1)).toBeFocused()
  await expect(page.locator('.owner-income-point strong')).toHaveText('0.000007 credits')
  await page.getByRole('button', { name: '查看数据表', exact: true }).click()
  await expect(page.getByRole('table', { name: '所选时段净收入趋势', exact: true })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  )
})

test('Hong Kong articles use Traditional Chinese and mobile navigation starts collapsed', async ({
  page,
  isMobile,
}) => {
  await page.addInitScript(() => localStorage.setItem('codego.locale', 'zh-HK'))
  await page.goto('/docs?article=quickstart')
  await expect(page.locator('article')).toHaveAttribute('lang', 'zh-HK')
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('完成第一次調用')
  await expect(page.locator('.docs-language-notice')).toHaveCount(0)
  if (isMobile) {
    const toggle = page.getByRole('button', { name: '文件分區', exact: true })
    await expect(toggle).toHaveAttribute('aria-expanded', 'false')
    await expect(page.getByRole('searchbox')).not.toBeVisible()
    const heading = await page.getByRole('heading', { level: 1 }).boundingBox()
    expect(heading!.y).toBeLessThan(500)
    await toggle.click()
    await page.getByRole('searchbox').fill('密鑰')
    await expect(
      page.locator('.docs-navigation').getByRole('link', { name: '密鑰與認證', exact: true }),
    ).toBeVisible()
    await page
      .locator('.docs-navigation')
      .getByRole('link', { name: '密鑰與認證', exact: true })
      .click()
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('密鑰與認證')
    await expect(toggle).toHaveAttribute('aria-expanded', 'false')
  }
  if (process.env.CAPTURE_UI === '1')
    await page.screenshot({ path: test.info().outputPath('docs-traditional.png'), fullPage: true })
})
