import { test, expect } from '@playwright/test'
import { fixtureAPI } from './fixtures'

const pool = {
  id: '9007199254740993',
  name: '官方模型路由',
  group: 'default',
  model: '',
  strategy: 'weighted',
  enabled: true,
  model_scope: '*',
  auto_discover: true,
  multiplier_weight: 3,
  ttft_weight: 2,
  cache_weight: 1,
  success_weight: 4,
  members: [
    {
      channel_id: 1,
      priority: 0,
      weight: 1,
      cost_multiplier: 1.2,
      model_cost_overrides: { 'gpt-4o': 1.3 },
      fault_domain: 'openai',
      enabled: true,
    },
  ],
}

test.beforeEach(async ({ page }) => fixtureAPI(page))

test('named route pools retain official ranking metadata and edit/delete by pool ID', async ({
  page,
}) => {
  await page.route('**/api/catalog/route-pools', (route) =>
    route.fulfill({
      json: { success: true, data: route.request().method() === 'GET' ? [pool] : { id: pool.id } },
    }),
  )
  await page.goto('/channels')
  await page.getByRole('button', { name: '分组与定价', exact: true }).click()
  await page.getByRole('combobox', { name: '配置类型', exact: true }).selectOption('route-pools')
  const row = page.getByRole('row').filter({ hasText: '官方模型路由' })
  await row.getByRole('button', { name: '编辑', exact: true }).click()
  const configuration = page.getByRole('textbox', { name: '配置', exact: true })
  const body = JSON.parse(await configuration.inputValue())
  expect(body).toEqual(pool)
  body.name = '更新后的官方路由'
  await configuration.fill(JSON.stringify(body))
  const saved = page.waitForRequest(
    (request) =>
      new URL(request.url()).pathname === '/api/catalog/route-pools' && request.method() === 'PUT',
  )
  await page.getByRole('button', { name: '保存', exact: true }).click()
  expect((await saved).postData()).toContain('"id":9007199254740993')
  await expect(configuration).toHaveCount(0)
  page.once('dialog', (dialog) => dialog.accept())
  const deleted = page.waitForRequest('**/api/catalog/route-pools/9007199254740993')
  await row.getByRole('button', { name: '删除', exact: true }).click()
  expect((await deleted).method()).toBe('DELETE')
})

test('pool member validation refuses malformed monetary multipliers', async ({ page }) => {
  await page.route('**/api/catalog/route-pools', (route) =>
    route.fulfill({ json: { success: true, data: [pool] } }),
  )
  await page.goto('/channels')
  await page.getByRole('button', { name: '分组与定价', exact: true }).click()
  await page.getByRole('combobox', { name: '配置类型', exact: true }).selectOption('route-pools')
  await page
    .getByRole('row')
    .filter({ hasText: '官方模型路由' })
    .getByRole('button', { name: '编辑', exact: true })
    .click()
  await page
    .getByRole('textbox', { name: '配置', exact: true })
    .fill(
      JSON.stringify({ ...pool, members: [{ ...pool.members[0], cost_multiplier: 'invalid' }] }),
    )
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('配置字段与当前类型不匹配')
})

test('model prices are edited in credits with exact large, zero and fractional amounts', async ({
  page,
}) => {
  const price = {
    model: 'credit-model',
    mode: 'per_token',
    input_per_mtok: '9007199254740993',
    output_per_mtok: 2500000,
    cache_read_per_mtok: 0,
    cache_write_per_mtok: 1,
    per_request: 40000,
    rules: { money_quantum: 2 },
  }
  await page.route('**/api/catalog/prices', (route) =>
    route.fulfill({ json: { success: true, data: [price] } }),
  )
  await page.route('**/api/catalog/prices/credit-model', (route) =>
    route.fulfill({ json: { success: true, data: null } }),
  )
  await page.goto('/channels')
  await page.getByRole('button', { name: '分组与定价', exact: true }).click()
  await page.getByRole('combobox', { name: '配置类型', exact: true }).selectOption('prices')
  await page
    .getByRole('row')
    .filter({ hasText: price.model })
    .getByRole('button', { name: '编辑', exact: true })
    .click()
  const input = page.getByRole('textbox', { name: '输入 credits / 百万 tokens', exact: true })
  await expect(input).toHaveValue('9007199254.740993')
  await expect(
    page.getByRole('textbox', { name: '输出 credits / 百万 tokens', exact: true }),
  ).toHaveValue('2.500000')
  await expect(page.getByRole('textbox', { name: '每次 credits', exact: true })).toHaveValue(
    '0.040000',
  )
  const configuration = page.getByRole('textbox', { name: '配置', exact: true })
  expect(JSON.parse(await configuration.inputValue())).toEqual({
    model: price.model,
    mode: price.mode,
    rules: price.rules,
  })

  let writes = 0
  page.on('request', (request) => {
    if (
      request.method() === 'PUT' &&
      new URL(request.url()).pathname === '/api/catalog/prices/credit-model'
    )
      writes++
  })
  await input.fill('1.0000001')
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('最多六位小数')
  expect(writes).toBe(0)
  await input.fill('9007199254.740993')
  const saved = page.waitForRequest(
    (request) =>
      request.method() === 'PUT' &&
      new URL(request.url()).pathname === '/api/catalog/prices/credit-model',
  )
  await page.getByRole('button', { name: '保存', exact: true }).click()
  const body = (await saved).postData()
  expect(body).toContain('"input_per_mtok":9007199254740993')
  expect(body).toContain('"output_per_mtok":2500000')
  expect(body).toContain('"cache_read_per_mtok":0')
  expect(body).toContain('"cache_write_per_mtok":1')
  expect(body).toContain('"per_request":40000')
  expect(body).toContain('"rules":{"money_quantum":2}')
  await expect(configuration).toHaveCount(0)
})
