import { test, expect } from '@playwright/test'
import { fixtureAPI } from './fixtures'

test.beforeEach(async ({ page }) => fixtureAPI(page))

const healthyGroup = {
  id: 'group-1',
  group_id: 'group-1',
  internal_channel_id: 1,
  owner_user_id: 1,
  public_slug: 'official-openai',
  system_display_name: '官方 OpenAI 分组',
  provider_type: 'openai',
  approved_source_label: '官方渠道',
  declared_models: ['gpt-4o'],
  model_prices: {},
  multiplier_ppm: 1000000,
  multiplier: 1,
  visibility: 'public',
  lifecycle_status: 'active',
  verification_status: 'passed',
  last_review_reason: '',
  auto_probe_last_at: '2026-09-30T08:00:00Z',
  model_verification_results: [
    {
      model: 'gpt-4o',
      listed: true,
      status: 'passed',
      latency_ms: 320,
      tested_at: '2026-09-30T08:00:00Z',
    },
  ],
  created_at: '2026-09-30T08:00:00Z',
  updated_at: '2026-09-30T08:00:00Z',
}

const degradedGroup = {
  ...healthyGroup,
  id: 'group-2',
  group_id: 'group-2',
  public_slug: 'official-claude',
  system_display_name: '官方 Claude 分组',
  model_verification_results: [
    {
      model: 'claude-sonnet-4',
      listed: true,
      status: 'failed',
      latency_ms: 980,
      error: '上游连通性或响应验证失败',
      tested_at: '2026-09-30T09:00:00Z',
    },
  ],
}

test.describe('status page', () => {
  test('renders the overall banner and per-group probe rows', async ({ page }) => {
    await page.clock.setFixedTime(new Date('2026-09-30T09:01:00Z'))
    await page.route('**/api/status', (route) =>
      route.fulfill({
        json: { success: true, data: { version: '3.0.0', credits_per_unit: 1000000 } },
      }),
    )
    await page.route('**/api/marketplace/group-status', (route) =>
      route.fulfill({ json: { success: true, data: [healthyGroup, degradedGroup] } }),
    )
    await page.goto('/status')
    await expect(page.getByRole('heading', { name: '服务状态', exact: true })).toBeVisible()
    await expect(page.getByText('部分分组探测异常', { exact: true })).toBeVisible()
    await expect(page.getByText('官方 OpenAI 分组', { exact: true })).toBeVisible()
    await expect(page.getByText('官方 Claude 分组', { exact: true })).toBeVisible()
    await expect(page.getByText('上游连通性或响应验证失败', { exact: true })).toBeVisible()
  })

  test('shows an error state with a working retry', async ({ page }) => {
    let calls = 0
    await page.route('**/api/status', (route) => {
      calls += 1
      return route.fulfill({ status: 503, json: { success: false, message: '服务暂时不可用' } })
    })
    await page.route('**/api/marketplace/group-status', (route) =>
      route.fulfill({ json: { success: true, data: [] } }),
    )
    await page.goto('/status')
    await expect(page.getByRole('alert')).toHaveText('服务暂时不可用')
    await page.getByRole('button', { name: '重试', exact: true }).click()
    expect(calls).toBeGreaterThan(1)
  })

  test('shows an empty state when there are no public groups', async ({ page }) => {
    await page.route('**/api/status', (route) =>
      route.fulfill({
        json: { success: true, data: { version: '3.0.0', credits_per_unit: 1000000 } },
      }),
    )
    await page.route('**/api/marketplace/group-status', (route) =>
      route.fulfill({ json: { success: true, data: [] } }),
    )
    await page.goto('/status')
    await expect(page.getByText('暂无公开分组', { exact: true })).toBeVisible()
  })
})

test('retired download links lead to integration docs', async ({ page }) => {
  await page.goto('/download')
  await expect(page).toHaveURL(/\/docs$/)
  await expect(page.getByRole('link', { name: '下载', exact: true })).toHaveCount(0)
})

test.describe('auth layout', () => {
  test('shows the brand aside panel on a desktop viewport', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })
    await page.goto('/sign-in')
    await expect(page.locator('.auth-aside')).toBeVisible()
    await expect(page.getByRole('heading', { name: '登录', exact: true })).toBeVisible()
  })
})
