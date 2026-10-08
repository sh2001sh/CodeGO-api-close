import { expect, test, type Page } from '@playwright/test'
import { fixtureAPI } from './fixtures'
import { marketGroups } from './market-fixture'

const groups = [
  {
    id: '101',
    group_id: 'internal-a',
    public_slug: 'alpha',
    routing_group: 'market_a',
    system_display_name: 'Alpha',
    provider_type: 'openai',
    approved_source_label: '',
    declared_models: ['gpt-4o', 'claude-sonnet'],
    multiplier: 1,
    multiplier_ppm: 1000000,
    visibility: 'public',
    verification_status: 'passed',
    effective_model_prices: {},
    model_verification_results: [],
    recent_request_series: [],
  },
  {
    id: '102',
    group_id: 'internal-b',
    public_slug: 'beta',
    routing_group: 'market_b',
    system_display_name: 'Beta',
    provider_type: 'openai',
    approved_source_label: '',
    declared_models: ['gpt-4o'],
    multiplier: 0.8,
    multiplier_ppm: 800000,
    visibility: 'public',
    verification_status: 'passed',
    effective_model_prices: {},
    model_verification_results: [],
    recent_request_series: [],
  },
]
const poolGroups = groups.map((group) => ({
  group_id: group.group_id,
  name: group.system_display_name,
  display_id: group.id,
  kind: 'market',
  multiplier: group.multiplier,
  models: group.declared_models,
}))
const official = {
  group_id: 'official:default',
  name: '平台默认服务',
  display_id: 'default',
  kind: 'official',
  multiplier: 1,
  models: ['official-only-chat'],
}
const build = {
  enabled: true,
  models: ['gpt-4o'],
  size: 3,
  explore: 0,
  schedule: 'interval',
  interval_minutes: 60,
  daily_time: '',
  consumer_weight: 25,
  success_weight: 55,
  cache_weight: 20,
  last_build_at: '2026-10-06T00:00:00Z',
  next_build_at: '2026-10-06T01:00:00Z',
  last_error: '',
}
const original = {
  id: 'pool-1',
  owner_user_id: 1,
  name: '我的模型池',
  strategy: 'priority',
  max_attempts: 3,
  failure_cooldown_seconds: 30,
  max_multiplier: 0,
  members: [
    { group_id: 'internal-b', priority: 0 },
    { group_id: 'internal-a', priority: 1 },
  ],
  auto_build: build,
  config: { auto_build: build },
}
async function setup(page: Page) {
  await fixtureAPI(page)
  await marketGroups(page, groups)
  await page.route('**/api/marketplace/route-pools/group-options', (route) =>
    route.fulfill({ json: { success: true, data: poolGroups } }),
  )
  await page.route('**/api/marketplace/auto-route-pool', (route) =>
    route.fulfill({
      json: { success: true, data: { ...original, id: 'pool-auto', name: 'Auto' } },
    }),
  )
}
test.beforeEach(async ({ page }) => setup(page))

test('manual order, limits and per-pool daily auto settings survive a denied save and corrected retry', async ({
  page,
}, testInfo) => {
  const bodies: Record<string, unknown>[] = []
  await page.route('**/api/marketplace/route-pools', (route) =>
    route.fulfill({ json: { success: true, data: [original] } }),
  )
  await page.route('**/api/marketplace/route-pools/pool-1', (route) => {
    bodies.push(route.request().postDataJSON())
    return route.fulfill(
      bodies.length === 1
        ? { status: 403, json: { success: false, message: '路由池访问权限不足' } }
        : { json: { success: true, data: { ...original, ...bodies.at(-1) } } },
    )
  })
  await page.goto('/channel-market')
  await page.getByRole('tab', { name: '路由池', exact: true }).click()
  await page.getByRole('button', { name: '编辑', exact: true }).click()
  await expect(page.locator('.pool-member-order li').first()).toContainText('Beta · 102')
  await page.getByRole('button', { name: '上移 Alpha · 101', exact: true }).click()
  await page.getByLabel('路由池名称', { exact: true }).fill('多模型池')
  await page.getByLabel('最大尝试次数', { exact: true }).fill('7')
  await page.getByLabel('失败冷却（秒）', { exact: true }).fill('90')
  await page.getByLabel('最高倍率（0 不限）', { exact: true }).fill('1.5')
  await page.getByRole('combobox', { name: '更新计划', exact: true }).selectOption('daily')
  await page.getByLabel('每日更新时间（UTC）', { exact: true }).fill('06:30')
  await page.getByLabel('候选分组数量', { exact: true }).fill('4')
  await page.getByLabel('探索分组数量', { exact: true }).fill('1')
  await page
    .getByRole('group', { name: '自动更新模型', exact: true })
    .getByRole('checkbox', { name: 'claude-sonnet', exact: true })
    .check()
  await page.screenshot({ path: testInfo.outputPath('pool-editor.png'), fullPage: true })
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('路由池访问权限不足')
  await expect(page.locator('.pool-member-order li').first()).toContainText('Alpha · 101')
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: '路由池已保存' })).toBeVisible()
  expect(bodies).toHaveLength(2)
  expect(bodies[0]).toEqual(bodies[1])
  expect(bodies[1]).toMatchObject({
    name: '多模型池',
    max_attempts: 7,
    failure_cooldown_seconds: 90,
    max_multiplier: 1.5,
    members: [
      { group_id: 'internal-a', priority: 0 },
      { group_id: 'internal-b', priority: 1 },
    ],
    auto_build: {
      enabled: true,
      models: ['gpt-4o', 'claude-sonnet'],
      size: 4,
      explore: 1,
      schedule: 'daily',
      daily_time: '06:30',
    },
  })
  expect(bodies[1].auto_build).not.toHaveProperty('last_build_at')
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= document.documentElement.clientWidth + 1,
    ),
  ).toBe(true)
})

test('run-now failure refreshes server-maintained error metadata and can be retried', async ({
  page,
}) => {
  let attempts = 0
  let lastError = ''
  await page.route('**/api/marketplace/route-pools', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: [{ ...original, auto_build: { ...build, last_error: lastError } }],
      },
    }),
  )
  await page.route('**/api/marketplace/route-pools/pool-1/auto-build/run', (route) => {
    attempts++
    lastError = attempts === 1 ? '暂无符合条件的候选分组' : ''
    return route.fulfill(
      attempts === 1
        ? { status: 409, json: { success: false, message: '此次更新未找到候选分组' } }
        : { json: { success: true, data: original } },
    )
  })
  await page.goto('/channel-market')
  await page.getByRole('tab', { name: '路由池', exact: true }).click()
  await page.getByRole('button', { name: '立即更新', exact: true }).click()
  await expect(page.locator('.pool-build-error')).toHaveText('暂无符合条件的候选分组')
  await expect(page.locator('.pool-saved-members')).toContainText('Beta · 102')
  await page.getByRole('button', { name: '立即更新', exact: true }).click()
  await expect(page.locator('.pool-build-error')).toHaveCount(0)
  await expect(page.getByRole('status').filter({ hasText: '路由池已更新' })).toBeVisible()
  expect(attempts).toBe(2)
})

test('adding a selected numeric public group preserves existing members and uses internal binding ids', async ({
  page,
}) => {
  let pool = { ...original, members: [{ group_id: 'internal-b', priority: 0 }] }
  let sent: Record<string, unknown> | undefined
  await page.route('**/api/marketplace/route-pools', (route) =>
    route.fulfill({ json: { success: true, data: [pool] } }),
  )
  await page.route('**/api/marketplace/route-pools/pool-1', (route) => {
    sent = route.request().postDataJSON()
    return route.fulfill({ json: { success: true, data: pool } })
  })
  await page.goto('/channel-market?group=101')
  await expect(page.locator('#market-detail-internal-a')).toBeVisible()
  await page.getByRole('combobox', { name: '选择路由池', exact: true }).selectOption('pool-1')
  await page.getByRole('button', { name: '加入路由池', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: '已加入路由池' })).toBeVisible()
  expect(sent).toMatchObject({
    members: [
      { group_id: 'internal-b', priority: 0 },
      { group_id: 'internal-a', priority: 1 },
    ],
  })
})

test('official-only candidates provide models and save the authorized official member', async ({
  page,
}) => {
  await marketGroups(page, [])
  await page.route('**/api/marketplace/route-pools/group-options', (route) =>
    route.fulfill({ json: { success: true, data: [official] } }),
  )
  let sent: Record<string, unknown> | undefined
  await page.route('**/api/marketplace/route-pools', (route) => {
    if (route.request().method() === 'POST') sent = route.request().postDataJSON()
    return route.fulfill({ json: { success: true, data: [] } })
  })
  await page.goto('/channel-market')
  await page.getByRole('tab', { name: '路由池', exact: true }).click()
  await page.getByRole('button', { name: '创建路由池', exact: true }).click()
  await page.getByLabel('路由池名称', { exact: true }).fill('官方备用池')
  await page
    .locator('.pool-group-picker')
    .getByRole('checkbox', { name: '官方分组 · 平台默认服务 · default · 1×', exact: true })
    .check()
  await page.getByRole('checkbox', { name: '按计划自动更新路由池', exact: true }).check()
  await page
    .getByRole('group', { name: '自动更新模型', exact: true })
    .getByRole('checkbox', { name: 'official-only-chat', exact: true })
    .check()
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: '路由池已保存' })).toBeVisible()
  expect(sent).toMatchObject({
    members: [{ group_id: 'official:default', priority: 0 }],
    auto_build: { enabled: true, models: ['official-only-chat'] },
  })
})

test('official and renamed market members are displayed while unlisted members remain intact', async ({
  page,
}) => {
  await page.route('**/api/marketplace/route-pools/group-options', (route) =>
    route.fulfill({
      json: { success: true, data: [official, { ...poolGroups[0], name: '海港推理服务' }] },
    }),
  )
  const existing = {
    ...original,
    members: [
      { group_id: 'official:default', priority: 0 },
      { group_id: 'internal-unlisted', priority: 1 },
      { group_id: 'internal-a', priority: 2 },
    ],
  }
  await page.route('**/api/marketplace/route-pools', (route) =>
    route.fulfill({ json: { success: true, data: [existing] } }),
  )
  let sent: Record<string, unknown> | undefined
  await page.route('**/api/marketplace/route-pools/pool-1', (route) => {
    sent = route.request().postDataJSON()
    return route.fulfill({ json: { success: true, data: existing } })
  })
  await page.goto('/channel-market')
  await page.getByRole('tab', { name: '路由池', exact: true }).click()
  await expect(page.locator('.pool-saved-members')).toContainText(
    '官方分组 · 平台默认服务 · default',
  )
  await expect(page.locator('.pool-saved-members')).toContainText('海港推理服务 · 101')
  await page.getByRole('button', { name: '编辑', exact: true }).click()
  await expect(page.locator('.pool-member-order')).toContainText('未列出的分组')
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: '路由池已保存' })).toBeVisible()
  expect(sent).toMatchObject({ members: existing.members })
})

test('candidate authorization failure is shown without offering an empty editor', async ({
  page,
}) => {
  await page.route('**/api/marketplace/route-pools/group-options', (route) =>
    route.fulfill({ status: 403, json: { success: false, message: '分组选项访问权限不足' } }),
  )
  await page.route('**/api/marketplace/route-pools', (route) =>
    route.fulfill({ json: { success: true, data: [original] } }),
  )
  await page.goto('/channel-market')
  await page.getByRole('tab', { name: '路由池', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('分组选项访问权限不足')
  await expect(page.getByRole('button', { name: '创建路由池', exact: true })).toBeDisabled()
  await expect(page.getByRole('button', { name: '配置自动路由池', exact: true })).toBeDisabled()
  await expect(page.getByRole('button', { name: '编辑', exact: true })).toBeDisabled()
  await expect(
    page.getByText('暂无可选择的分组；可使用的官方分组和已授权的市场分组会显示在此。'),
  ).toHaveCount(0)
})

for (const [serverError, expectedMessage] of [
  [
    'No eligible groups match the selected models and multiplier limit',
    '没有符合模型及倍率上限的分组；保留原成员，稍后重试。',
  ],
  ['Automatic pool update failed; retry scheduled', '自动更新失败，已安排重试。'],
]) {
  test(`known pool build error is localized: ${serverError}`, async ({ page }) => {
    await page.route('**/api/marketplace/route-pools', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: [{ ...original, auto_build: { ...build, last_error: serverError } }],
        },
      }),
    )
    await page.goto('/channel-market')
    await page.getByRole('tab', { name: '路由池', exact: true }).click()
    await expect(page.locator('.pool-build-error')).toHaveText(expectedMessage)
  })
}
