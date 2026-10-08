import { expect, test } from '@playwright/test'
import { fixtureAPI } from './fixtures'
import { marketGroups } from './market-fixture'

const quote = (input: string) => ({
  mode: 'per_token',
  unit: 'tokens',
  input_per_million: input,
  output_per_million: '3',
  cache_read_per_million: '0.1',
  cache_write_per_million: '1',
  per_unit: '0',
})
const group = (id: string, name: string, multiplier: number, input: string) => ({
  id:
    (
      {
        'stable-1': '101',
        expensive: '102',
        affordable: '103',
        stale: '104',
        'coding-1': '105',
        'reasoning-1': '106',
      } as Record<string, string>
    )[id] ?? '999',
  group_id: id,
  public_slug: `slug-${id}`,
  routing_group: `market:${id}`,
  system_display_name: name,
  provider_type: 'openai',
  approved_source_label: 'OpenAI',
  declared_models: ['gpt-4o'],
  model_prices: {},
  effective_model_prices: { 'gpt-4o': quote(input) },
  multiplier,
  multiplier_ppm: multiplier * 1_000_000,
  visibility: 'public',
  lifecycle_status: 'active',
  verification_status: 'passed',
  model_verification_results: [],
  max_concurrency: 10,
  user_max_concurrency: 2,
  recent_request_bucket_seconds: 3600,
  recent_request_series: [],
})

test.beforeEach(async ({ page }) => fixtureAPI(page))

test('a visitor can inspect a selected public group without requesting private resources', async ({
  page,
}) => {
  const protectedRequests: string[] = []
  await marketGroups(page, [group('stable-1', '推理服务', 1, '2')], 'groups')
  await page.route('**/api/**', async (route) => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/user/self' || path === '/api/user/refresh')
      return route.fulfill({ status: 401, json: { success: false, message: '请登录' } })
    if (
      [
        '/api/token/',
        '/api/marketplace/key-group-options',
        '/api/marketplace/multiplier-notices',
        '/api/marketplace/route-pools',
        '/api/marketplace/auto-route-pool',
      ].includes(path)
    )
      protectedRequests.push(path)
    return route.fallback()
  })
  await page.goto('/channel-market?group=101&model=gpt-4o')
  await expect(page.getByRole('heading', { name: '推理服务', exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: '登录后绑定此分组', exact: true })).toHaveAttribute(
    'href',
    /returnTo=.*101/,
  )
  await expect(page.getByRole('button', { name: '绑定 Key', exact: true })).toHaveCount(0)
  expect(protectedRequests).toEqual([])
  await page.getByRole('tab', { name: '路由池', exact: true }).click()
  await expect(page.getByText('登录后配置你的路由池', { exact: true })).toBeVisible()
  expect(protectedRequests).toEqual([])
})

test('six recent hourly slots use real request counts, correct percentage thresholds and accessible detail', async ({
  page,
}) => {
  const now = Math.floor(Date.now() / 3_600_000) * 3600
  const rates = [95, 90, 75, 74, 0, 100]
  const counts = [5, 10, 20, 8, 0, 1]
  await marketGroups(page, [
    {
      ...group('stable-1', '请求统计分组', 1, '2'),
      recent_request_series: rates.map((success_rate, index) => ({
        ts: now - (5 - index) * 3600,
        success_rate,
        request_count: counts[index],
      })),
    },
  ])
  await page.goto('/channel-market')
  const slots = page.locator('.market-recent-slot')
  await expect(slots).toHaveCount(6)
  await expect(page.locator('.market-recent-slot[data-tone="good"]')).toHaveCount(2)
  await expect(page.locator('.market-recent-slot[data-tone="warning"]')).toHaveCount(2)
  await expect(page.locator('.market-recent-slot[data-tone="poor"]')).toHaveCount(1)
  await expect(page.locator('.market-recent-slot[data-tone="empty"]')).toHaveCount(1)
  await expect(slots.first()).toHaveAttribute('aria-label', /请求数 5.*成功率 95%/)
  await slots.first().focus()
  await expect(slots.first()).toBeFocused()
  await expect(slots.nth(4)).toHaveAttribute('title', /请求数 0.*无请求/)
})

test('price comparison uses effective credits, including private groups, rather than the lowest multiplier', async ({
  page,
  isMobile,
}) => {
  await marketGroups(page, [
    group('expensive', '低倍率渠道', 0.5, '8'),
    { ...group('affordable', '私有推理', 1, '2'), visibility: 'private' },
  ])
  await page.goto('/channel-market?model=gpt-4o')
  if (isMobile) await page.getByRole('button', { name: '筛选与排序', exact: true }).click()
  await page.getByRole('combobox', { name: '排序', exact: true }).selectOption('price')
  const rows = page.locator('.market-listing')
  await expect(rows.first().getByRole('heading', { name: '私有推理', exact: true })).toBeVisible()
  await expect(rows.first().locator('.market-row-quote')).toContainText('2')
  await expect(rows.first().locator('.market-verified')).toHaveText('连通验证通过')
  await expect(rows.first().locator('.market-stable-id')).toContainText('103')
  await expect(rows.first().locator('.market-row-quote dt')).toHaveText(
    '输入 credits / 百万 tokens',
  )
  await expect(rows.first().locator('.market-row-quote dd')).toHaveText('2')
  await page.getByRole('combobox', { name: '比较价格口径', exact: true }).selectOption('output')
  await expect(rows.first().locator('.market-row-quote dt')).toHaveText(
    '输出 credits / 百万 tokens',
  )
  await expect(rows.first().locator('.market-row-quote dd')).toHaveText('3')
  await rows.first().getByRole('button', { name: '选择渠道', exact: true }).click()
  await rows.first().getByText('模型价格', { exact: true }).click()
  await expect(rows.first().locator('.market-model-quotes .model-price-lines dt')).toHaveCount(4)
})

test('expired usage snapshots never present a historical success rate as current service quality', async ({
  page,
}) => {
  await marketGroups(page, [
    {
      ...group('stale', '历史统计分组', 1, '2'),
      quality: {
        calculated_at: new Date(Date.now() - 2 * 60 * 60_000).toISOString(),
        request_count: 999,
        success_rate: 1,
        score: 10,
        observing: false,
      },
    },
  ])
  await page.goto('/channel-market?group=stale')
  const row = page.locator('.market-listing')
  await expect(row.getByText('调用统计已过期，等待更新。', { exact: true })).toBeVisible()
  await expect(row.locator('dl div').filter({ hasText: '调用成功率' }).locator('dd')).toHaveText(
    '暂无数据',
  )
  await expect(row.locator('dl div').filter({ hasText: '24 小时请求' }).locator('dd')).toHaveText(
    '暂无数据',
  )
})

test('binding failure remains visible and retry keeps the stable group selection', async ({
  page,
}) => {
  await marketGroups(page, [group('stable-1', '推理服务', 1, '2')])
  const bodies: unknown[] = []
  await page.route('**/api/marketplace/groups/stable-1/bind-token', (route) => {
    bodies.push(route.request().postDataJSON())
    return route.fulfill(
      bodies.length === 1
        ? { status: 403, json: { success: false, message: '分组访问权限已失效' } }
        : { json: { success: true, data: {} } },
    )
  })
  await page.goto('/channel-market?group=stable-1&model=gpt-4o')
  await page.getByRole('combobox', { name: 'API Key', exact: true }).selectOption('1')
  await page.getByRole('button', { name: '绑定 Key', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('分组访问权限已失效')
  await expect(page.getByRole('link', { name: '使用此分组对话', exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: '绑定 Key', exact: true }).click()
  await expect(page.getByRole('link', { name: '使用此分组对话', exact: true })).toHaveAttribute(
    'href',
    /group=market%3Astable-1/,
  )
  expect(bodies).toEqual([{ token_id: 1 }, { token_id: 1 }])
})

test('provider tags filter listings and provider names are searchable', async ({
  page,
  isMobile,
}) => {
  await marketGroups(page, [
    {
      ...group('coding-1', '服务 A', 1, '2'),
      approved_source_label: '',
      tags: ['anthropic'],
    },
    {
      ...group('reasoning-1', '服务 B', 1, '2'),
      approved_source_label: '',
      tags: ['google'],
    },
  ])
  await page.goto('/channel-market')
  if (isMobile) await page.getByRole('button', { name: '筛选与排序', exact: true }).click()
  await page.getByRole('combobox', { name: '厂商标签', exact: true }).selectOption('google')
  await expect(page.locator('.market-listing')).toHaveCount(1)
  await expect(page.getByRole('heading', { name: '服务 B', exact: true })).toBeVisible()
  await page.getByRole('combobox', { name: '厂商标签', exact: true }).selectOption('')
  await page.getByRole('textbox', { name: '搜索渠道或模型', exact: true }).fill('Anthropic')
  await expect(page.locator('.market-listing')).toHaveCount(1)
  await expect(page.getByRole('heading', { name: '服务 A', exact: true })).toBeVisible()
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= document.documentElement.clientWidth + 1,
    ),
  ).toBe(true)
})

test('new key creation binds in one request and preserves selection on denied access', async ({
  page,
}) => {
  await page.route('**/api/token/', (route) => route.fulfill({ json: { success: true, data: [] } }))
  await marketGroups(page, [group('stable-1', '推理服务', 1, '2')])
  const bodies: unknown[] = []
  await page.route('**/api/marketplace/groups/stable-1/bind-token', (route) => {
    bodies.push(route.request().postDataJSON())
    return route.fulfill(
      bodies.length === 1
        ? { status: 403, json: { success: false, message: '分组访问权限已失效' } }
        : {
            json: {
              success: true,
              data: { token_id: 2, token_group: 'market:stable-1', api_key: 'sk-test-only-bound' },
            },
          },
    )
  })
  await page.goto('/channel-market?group=stable-1')
  await page.getByRole('button', { name: '创建 API Key', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('分组访问权限已失效')
  await expect(page.locator('.secret-panel')).toHaveCount(0)
  await page.getByRole('button', { name: '创建 API Key', exact: true }).click()
  await expect(page.locator('.secret-panel')).toContainText('sk-test-only-bound')
  await expect(page.getByRole('link', { name: '使用此分组对话', exact: true })).toHaveAttribute(
    'href',
    /group=market%3Astable-1/,
  )
  expect(bodies).toEqual([{ token_id: 0 }, { token_id: 0 }])
})

test('owner edits the pending name and curated tags without changing stable identifiers', async ({
  page,
}) => {
  await page.route('**/api/marketplace/channels/mine', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: [
          {
            ...group('stable-1', '已发布名称', 1, '2'),
            submitted_name: '待审名称',
            name_status: 'pending',
          },
        ],
      },
    }),
  )
  await page.goto('/my-channels')
  await page.getByRole('tab', { name: '渠道与访问', exact: true }).click()
  await page.getByRole('button', { name: '编辑', exact: true }).click()
  await expect(page.getByLabel('分组名称', { exact: true })).toHaveValue('待审名称')
  await expect(page.locator('.copy-field').filter({ hasText: '101' }).first()).toBeVisible()
  await page.getByLabel('分组名称', { exact: true }).fill('模型工作流')
  await page.getByLabel('分组备注', { exact: true }).fill('适合长文本和多轮对话')
  for (const name of ['OpenAI', 'Anthropic', 'Google', 'DeepSeek', 'xAI'])
    await page.getByRole('checkbox', { name, exact: true }).check()
  await expect(page.getByRole('checkbox', { name: 'Meta', exact: true })).toBeDisabled()
  const sent = page.waitForRequest(
    (request) =>
      request.method() === 'PATCH' && request.url().endsWith('/api/marketplace/channels/101'),
  )
  await page.getByRole('button', { name: '保存', exact: true }).click()
  const body = (await sent).postDataJSON()
  expect(body).toMatchObject({
    name: '模型工作流',
    remark: '适合长文本和多轮对话',
    tags: ['openai', 'anthropic', 'google', 'deepseek', 'xai'],
  })
  expect(body).not.toHaveProperty('group_id')
  expect(body).not.toHaveProperty('routing_group')
  expect(body).not.toHaveProperty('name_status')
})
