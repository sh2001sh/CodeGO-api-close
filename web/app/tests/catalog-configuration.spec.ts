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
