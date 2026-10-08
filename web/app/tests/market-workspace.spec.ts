import { test, expect } from '@playwright/test'
import { fixtureAPI } from './fixtures'

const publicGroup = {
  id: '349',
  internal_channel_id: 1,
  owner_user_id: 1,
  group_id: 'group-test',
  system_display_name: '公共模型渠道',
  provider_type: 'openai',
  approved_source_label: '公共渠道',
  declared_models: ['gpt-4o'],
  model_prices: {},
  multiplier_ppm: 1000000,
  multiplier: 1,
  visibility: 'public',
  lifecycle_status: 'active',
  verification_status: 'passed',
  recent_request_bucket_seconds: 3600,
  recent_request_series: [],
  last_review_reason: '',
}
const privateGroup = {
  ...publicGroup,
  id: '350',
  group_id: 'private-test',
  system_display_name: '受邀私有渠道',
  visibility: 'private',
  declared_models: ['claude-sonnet-4'],
  multiplier: 0.8,
  multiplier_ppm: 800000,
}

test.beforeEach(async ({ page }) => fixtureAPI(page))

test('administrators browse the buyer market and can open the separate channel workspace', async ({
  page,
}) => {
  const adminRequests: string[] = []
  page.on('request', (request) => {
    if (new URL(request.url()).pathname.startsWith('/api/catalog/channels'))
      adminRequests.push(request.url())
  })
  await page.goto('/channel-market')
  await expect(page.getByRole('heading', { name: '渠道市场', exact: true })).toBeVisible()
  await expect(page.getByRole('tab', { name: '分组', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await expect(page.getByRole('button', { name: '选择渠道', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '创建', exact: true })).toHaveCount(0)
  expect(adminRequests).toEqual([])
  await page
    .locator('.market-workspace')
    .getByRole('link', { name: '渠道工作台', exact: true })
    .click()
  await expect(page.getByRole('heading', { name: '渠道工作台', exact: true })).toBeVisible()
})

test('market filtering, empty results and ordering use the actual returned groups', async ({
  page,
}) => {
  await page.route('**/api/marketplace/key-group-options', (route) =>
    route.fulfill({ json: { success: true, data: [publicGroup, privateGroup] } }),
  )
  await page.goto('/channel-market')
  await page.getByRole('combobox', { name: '排序', exact: true }).selectOption('multiplier')
  await expect(page.locator('.market-listing h2').first()).toHaveText('受邀私有渠道')
  await page.getByRole('combobox', { name: '渠道范围', exact: true }).selectOption('public')
  await expect(page.locator('.market-listing')).toHaveCount(1)
  await expect(page.locator('.market-listing h2')).toHaveText('公共模型渠道')
  await page.getByLabel('搜索渠道或模型', { exact: true }).fill('nonexistent-model')
  await expect(page.getByText('没有匹配的渠道', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '清除筛选', exact: true }).click()
  await expect(page.locator('.market-listing')).toHaveCount(2)
  await page.getByLabel('搜索渠道或模型', { exact: true }).fill('claude-sonnet-4')
  await expect(page.locator('.market-listing h2')).toHaveText('受邀私有渠道')
  expect(await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth)).toBe(
    false,
  )
})

test('key binding preserves int64 IDs and reports a denied bind without claiming success', async ({
  page,
}) => {
  await page.route('**/api/token/', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: [{ id: '9223372036854775807', name: '精确密钥', status: 'active' }],
      },
    }),
  )
  let body = ''
  await page.route('**/api/marketplace/groups/group-test/bind-token', (route) => {
    body = route.request().postData() ?? ''
    return route.fulfill({ status: 403, json: { success: false, message: '无权执行此操作' } })
  })
  await page.goto('/channel-market')
  await page.getByRole('button', { name: '选择渠道', exact: true }).click()
  await page
    .getByRole('combobox', { name: 'API Key', exact: true })
    .selectOption('9223372036854775807')
  await page.getByRole('button', { name: '绑定 Key', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('无权执行此操作')
  expect(body).toBe('{"token_id":9223372036854775807}')
  await expect(page.getByRole('status').filter({ hasText: '已保存' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '绑定 Key', exact: true })).toBeEnabled()
})

test('market without active keys offers key creation instead of an unusable bind form', async ({
  page,
}) => {
  await page.route('**/api/token/', (route) => route.fulfill({ json: { success: true, data: [] } }))
  await page.goto('/channel-market')
  await page.getByRole('button', { name: '选择渠道', exact: true }).click()
  await expect(page.getByText('暂无可绑定的 API Key', { exact: true })).toBeVisible()
  await expect(
    page
      .locator('.market-listing-detail')
      .getByRole('button', { name: '创建 API Key', exact: true }),
  ).toBeEnabled()
  await expect(page.getByRole('button', { name: '绑定 Key', exact: true })).toHaveCount(0)
})

test('route pools and private invitations remain reachable in buyer tabs', async ({ page }) => {
  await page.route('**/api/marketplace/invites/accept', (route) =>
    route.fulfill({ json: { success: true, data: { group_id: 'private-test' } } }),
  )
  await page.goto('/channel-market')
  await page.getByRole('tab', { name: '路由池', exact: true }).click()
  await expect(page.getByRole('button', { name: '创建路由池', exact: true })).toBeVisible()
  await page.getByRole('tab', { name: '邀请与通知', exact: true }).click()
  await page.getByLabel('邀请令牌', { exact: true }).fill('fixture-private-invitation')
  await page.getByRole('button', { name: '接受邀请', exact: true }).click()
  await expect(page.getByRole('button', { name: '绑定私有组', exact: true })).toBeVisible()
  await page.getByRole('combobox', { name: 'API Key', exact: true }).selectOption('1')
  const sent = page.waitForRequest('**/api/marketplace/groups/private-test/bind-token')
  await page.getByRole('button', { name: '绑定私有组', exact: true }).click()
  expect((await sent).postDataJSON()).toEqual({ token_id: 1 })
})

test('channel workspace aggregates income exactly and retains operations across sections', async ({
  page,
}) => {
  await page.route('**/api/marketplace/channels/mine/observability', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: [
          {
            owner_user_id: 1,
            pending_income_micro: '9007199254740993',
            total_income_micro: '9007199254740993',
            request_count: 2,
            released_income_micro: 0,
            reclaimed_income_micro: 0,
          },
          {
            owner_user_id: 1,
            pending_income_micro: '7',
            total_income_micro: '7',
            request_count: 1,
            released_income_micro: 0,
            reclaimed_income_micro: 0,
          },
        ],
      },
    }),
  )
  await page.goto('/my-channels')
  await expect(page.getByRole('tab', { name: '运营概览', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await page.getByRole('tab', { name: '累计收入与结算', exact: true }).click()
  await expect(
    page.locator('.channel-workspace').getByText('9,007,199,254.740993 credits', { exact: true }),
  ).toHaveCount(2)
  await expect(page.getByRole('heading', { name: '渠道收入与结算', exact: true })).toBeVisible()
  await page.getByRole('tab', { name: '渠道与访问', exact: true }).click()
  await expect(page.getByRole('button', { name: '访问管理', exact: true })).toBeVisible()
  await page.getByRole('tab', { name: '议价与风控', exact: true }).click()
  await expect(page.getByRole('region', { name: '安全审计', exact: true })).toBeVisible()
  await page.getByRole('button', { name: '提交渠道', exact: true }).click()
  await expect(page.getByRole('tab', { name: '渠道与访问', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await expect(page.getByLabel('上游地址', { exact: true })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth)).toBe(
    false,
  )
})
