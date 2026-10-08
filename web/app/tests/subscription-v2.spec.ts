import { test, expect } from '@playwright/test'
import { fixtureAPI, plan } from './fixtures'

test.beforeEach(async ({ page }) => fixtureAPI(page))

test('fixed package compares against actual provider credits and has no legacy renew path', async ({
  page,
}) => {
  await page.route('**/api/subscription/plans', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: [
          {
            ...plan,
            policy_version: 'standard_v2',
            group_buy_enabled: false,
            credits: 103000000,
            period_credits: 0,
            duration_unit: 'day',
            duration_value: 90,
          },
        ],
      },
    }),
  )
  await page.route('**/api/commerce/providers', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: [{ provider: 'stripe', currency: 'usd', credits_per_minor: 100000 }],
      },
    }),
  )
  await page.goto('/wallet')
  await expect(page.getByText(/同金额充值可得 100 credits/)).toContainText(
    '多得 3 credits（3.00%）',
  )
  await page.getByRole('combobox', { name: '需要续期或升级的订阅', exact: true }).selectOption('1')
  await expect(page.getByRole('button', { name: '续期', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '升级到此套餐', exact: true })).toHaveCount(0)
  await expect(page.getByText(/额度一次到账，不支持刷新/)).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  )
})

test('new plan starts disabled and submits fixed version without old reward or reset settings', async ({
  page,
}) => {
  let body: Record<string, unknown> | undefined
  await page.route('**/api/subscription/admin/plans', async (route) => {
    if (route.request().method() !== 'POST') return route.fallback()
    body = route.request().postDataJSON()
    await route.fulfill({ json: { success: true, data: { ...body, id: 3 } } })
  })
  await page.goto('/subscriptions')
  await page.getByRole('button', { name: '新增套餐', exact: true }).click()
  await expect(page.getByRole('combobox', { name: '套餐规则版本', exact: true })).toHaveValue(
    'standard_v2',
  )
  await expect(page.getByLabel('上架套餐', { exact: true })).not.toBeChecked()
  await expect(page.getByLabel('重置周期', { exact: true })).toHaveCount(0)
  await expect(page.getByLabel('允许拼团', { exact: true })).toHaveCount(0)
  await page.getByLabel('套餐名称', { exact: true }).fill('已核定轻量包')
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect
    .poll(() => body)
    .toMatchObject({
      policy_version: 'standard_v2',
      enabled: false,
      reset_period: 'never',
      period_credits: 0,
      group_buy_enabled: false,
      fuel_enabled: false,
      lucky_draw_enabled: false,
      membership_tier: 'none',
    })
})
