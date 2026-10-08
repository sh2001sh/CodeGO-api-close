import { test, expect } from '@playwright/test'
import { fixtureAPI } from './fixtures'

test.beforeEach(async ({ page }) => fixtureAPI(page))

const summary = {
  requests: '9007199254740993',
  amount: '12345678',
  prompt_tokens: '9007199254740993',
  completion_tokens: '7',
  cached_tokens: '100',
}

test('overview keeps exact aggregate amounts and switches the complete statistics range', async ({
  page,
}) => {
  const ranges: URL[] = []
  await page.route('**/api/log/self/stat**', (route) => {
    ranges.push(new URL(route.request().url()))
    return route.fulfill({ json: { success: true, data: summary } })
  })
  await page.route('**/api/subscription/self', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: [
          {
            id: 1,
            state: 'active',
            expires_at: '2099-01-01T00:00:00Z',
            balance: '9007199254740993',
          },
          { id: 2, state: 'active', expires_at: '2099-01-01T00:00:00Z', balance: '2000002' },
          { id: 3, state: 'active', expires_at: '2000-01-01T00:00:00Z', balance: '999999999' },
          { id: 4, state: 'paused', expires_at: '2099-01-01T00:00:00Z', balance: '999999999' },
        ],
      },
    }),
  )
  await page.goto('/dashboard')
  const usage = page.getByRole('region', { name: '用量概览' })
  await expect(usage.getByText('9,007,199,254,740,993', { exact: true })).toBeVisible()
  await expect(usage.getByText('9,007,199,254,741,000', { exact: true })).toBeVisible()
  await expect(usage.getByText('12.345678 credits', { exact: true })).toBeVisible()
  await expect(page.getByText('9,007,199,256.740995 credits', { exact: true })).toBeVisible()
  await expect(page.getByText('仅根据所选时段最新 50 条调用统计，不代表全量用量。')).toBeVisible()
  const request = page.waitForRequest(
    (item) => item.url().includes('/api/log/self/stat') && item.url() !== ranges[0].href,
  )
  await usage.getByRole('button', { name: '最近 30 天', exact: true }).click()
  const changed = new URL((await request).url())
  expect(
    Date.parse(changed.searchParams.get('to')!) - Date.parse(changed.searchParams.get('from')!),
  ).toBe(30 * 24 * 60 * 60 * 1000)
  await expect(usage.getByRole('button', { name: '最近 30 天' })).toHaveAttribute(
    'aria-pressed',
    'true',
  )
  if (process.env.CAPTURE_UI === '1')
    await page.screenshot({ path: test.info().outputPath('dashboard.png'), fullPage: true })
  const row = page.getByRole('row').filter({ hasText: 'gpt-4o' })
  await row.focus()
  await row.press('Enter')
  await expect(page.getByRole('dialog', { name: '调用详情' })).toBeVisible()
  await page.getByRole('button', { name: '关闭', exact: true }).click()
  await expect(row).toBeFocused()
})

test('a zero wallet with an unexpired funded subscription does not show an unfunded warning', async ({
  page,
}) => {
  await page.route('**/api/wallet', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: { account_id: 1, balance_micro_credits: '0', balance_micro: 0, version: 0 },
      },
    }),
  )
  await page.route('**/api/subscription/self', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: [{ id: 1, state: 'active', expires_at: '2099-01-01T00:00:00Z', balance: '1000001' }],
      },
    }),
  )
  await page.goto('/dashboard')
  await expect(page.getByText('1.000001 credits', { exact: true })).toBeVisible()
  await expect(page.getByRole('alert')).toHaveCount(0)
  await page.getByText('查看调用示例', { exact: true }).click()
  await expect(page.locator('.overview-code-disclosure pre')).toContainText(
    'Authorization: Bearer $CODEGO_API_KEY',
  )
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  )
  if (process.env.CAPTURE_UI === '1')
    await page.screenshot({ path: test.info().outputPath('dashboard-empty.png'), fullPage: true })
})

test('statistics errors stay explicit and can be retried without losing the account overview', async ({
  page,
}) => {
  let fail = true
  await page.route('**/api/log/self/stat**', (route) =>
    route.fulfill(
      fail
        ? { status: 503, json: { success: false, message: '用量统计暂时不可用' } }
        : { json: { success: true, data: { ...summary, requests: '0', amount: '0' } } },
    ),
  )
  await page.route('**/api/log/self**', (route) =>
    route.request().url().includes('/stat')
      ? route.fallback()
      : route.fulfill({
          json: { success: true, data: { items: [], page_size: 50, has_more: false } },
        }),
  )
  await page.goto('/dashboard')
  await expect(page.getByText('123.456789 credits', { exact: true })).toBeVisible()
  const usage = page.getByRole('region', { name: '用量概览' })
  await expect(usage.getByRole('alert')).toHaveText('用量统计暂时不可用')
  await expect(page.getByText('还没有调用记录', { exact: true })).toBeVisible()
  fail = false
  await usage.getByRole('button', { name: '重试', exact: true }).click()
  await expect(usage.getByRole('alert')).toHaveCount(0)
  await expect(usage.getByText('0 credits', { exact: true })).toBeVisible()
})
