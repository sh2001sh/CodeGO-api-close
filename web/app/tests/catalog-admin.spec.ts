import { test, expect } from '@playwright/test'
import { fixtureAPI } from './fixtures'

const channel = {
  id: 1,
  name: 'OpenAI 官方',
  provider: 'openai',
  base_url: 'https://api.openai.com',
  proxy_url: '',
  status: 'enabled',
  scope: 'official',
  owner_user_id: null,
  priority: 1,
  weight: 1,
  max_concurrency: 20,
  max_user_concurrency: 5,
  auto_disable: true,
  multiplier_card_supported: true,
  groups: ['default'],
  models: ['gpt-4o'],
  tag: null,
  remark: '',
  settings: {},
  model_mapping: {},
  param_override: {},
  header_override: {},
  status_code_mapping: {},
}

test.beforeEach(async ({ page }) => fixtureAPI(page))

test('batch tag sends selected channel ids and tag value', async ({ page }) => {
  await page.route('**/api/catalog/channels', (route) =>
    route.fulfill({ json: { success: true, data: { items: [channel], total: 1 } } }),
  )
  const sent = page.waitForRequest(
    (request) =>
      new URL(request.url()).pathname === '/api/channel/batch/tag' && request.method() === 'POST',
  )
  await page.route('**/api/channel/batch/tag', (route) =>
    route.fulfill({ json: { success: true, data: 1 } }),
  )
  await page.goto('/channels')
  await page.getByRole('checkbox', { name: `选择 ${channel.name}` }).check()
  await page.getByLabel('批量标签', { exact: true }).fill('priority')
  await page.getByRole('button', { name: '设置标签', exact: true }).click()
  const body = (await sent).postDataJSON()
  expect(body).toEqual({ ids: [1], tag: 'priority' })
})

test('batch delete requires confirmation and posts selected ids', async ({ page }) => {
  await page.route('**/api/catalog/channels', (route) =>
    route.fulfill({ json: { success: true, data: { items: [channel], total: 1 } } }),
  )
  const sent = page.waitForRequest(
    (request) =>
      new URL(request.url()).pathname === '/api/channel/batch' && request.method() === 'POST',
  )
  await page.route('**/api/channel/batch', (route) =>
    route.fulfill({ json: { success: true, data: 1 } }),
  )
  await page.goto('/channels')
  await page.getByRole('checkbox', { name: `选择 ${channel.name}` }).check()
  await page.getByRole('button', { name: '批量删除', exact: true }).click()
  await page.getByRole('button', { name: '确认', exact: true }).click()
  const body = (await sent).postDataJSON()
  expect(body).toEqual({ ids: [1] })
})

test('row test shows latency on success', async ({ page }) => {
  await page.route('**/api/catalog/channels', (route) =>
    route.fulfill({ json: { success: true, data: { items: [channel], total: 1 } } }),
  )
  await page.route('**/api/channel/test/1', (route) =>
    route.fulfill({ json: { success: true, id: 1, time: 0.234, message: '' } }),
  )
  await page.goto('/channels')
  await page.getByRole('button', { name: '测速', exact: true }).click()
  await expect(page.getByText('234ms')).toBeVisible()
})

test('fetch models into the channel form lets the admin pick upstream models', async ({ page }) => {
  await page.route('**/api/catalog/channels', (route) =>
    route.fulfill({ json: { success: true, data: { items: [], total: 0 } } }),
  )
  const sent = page.waitForRequest(
    (request) =>
      new URL(request.url()).pathname === '/api/channel/fetch_models' &&
      request.method() === 'POST',
  )
  await page.route('**/api/channel/fetch_models', (route) =>
    route.fulfill({ json: { success: true, data: ['gpt-4o', 'gpt-4o-mini'] } }),
  )
  await page.goto('/channels')
  await page.getByRole('button', { name: '创建', exact: true }).click()
  await page.getByLabel('凭据', { exact: true }).fill('sk-fixture')
  await page.getByRole('button', { name: '获取模型', exact: true }).click()
  const body = (await sent).postDataJSON()
  expect(body).toEqual({ base_url: '', type: 1, key: 'sk-fixture' })
  await page.getByRole('checkbox', { name: 'gpt-4o-mini' }).check()
  await expect(page.getByLabel('模型', { exact: true })).toHaveValue('gpt-4o-mini')
})

test('vendor create posts the vendor payload', async ({ page }) => {
  // Playwright glob routes must match the whole URL, and later registrations
  // are checked first, so the specific /missing route is registered after
  // the general models* route that also matches a trailing query string.
  await page.route('**/api/catalog/models*', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: { items: [], total: 0, page: 1, page_size: 30, vendor_counts: {} },
      },
    }),
  )
  await page.route('**/api/catalog/models/missing', (route) =>
    route.fulfill({ json: { success: true, data: [] } }),
  )
  await page.route('**/api/catalog/vendors*', (route) => {
    if (route.request().method() === 'GET')
      return route.fulfill({
        json: { success: true, data: { items: [], total: 0, page: 1, page_size: 100 } },
      })
    return route.fulfill({
      json: {
        success: true,
        data: { id: 1, name: 'OpenAI', status: 1, created_time: 0, updated_time: 0 },
      },
    })
  })
  const sent = page.waitForRequest(
    (request) =>
      new URL(request.url()).pathname === '/api/catalog/vendors' && request.method() === 'POST',
  )
  await page.goto('/admin/models')
  await page.getByRole('tab', { name: '厂商', exact: true }).click()
  await page
    .getByRole('tabpanel', { name: '厂商', exact: true })
    .getByRole('button', { name: '创建', exact: true })
    .click()
  await page.getByLabel('厂商名称', { exact: true }).fill('OpenAI')
  await page.getByRole('button', { name: '保存', exact: true }).click()
  const body = (await sent).postDataJSON()
  expect(body.name).toBe('OpenAI')
  expect(body.status).toBe(1)
})

test('sync preview shows conflicts before an admin applies them', async ({ page }) => {
  await page.route('**/api/catalog/models*', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: { items: [], total: 0, page: 1, page_size: 30, vendor_counts: {} },
      },
    }),
  )
  await page.route('**/api/catalog/models/missing', (route) =>
    route.fulfill({ json: { success: true, data: [] } }),
  )
  await page.route('**/api/catalog/models/sync_upstream/preview', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          missing: ['claude-sonnet-4'],
          conflicts: [
            {
              model_name: 'gpt-4o',
              fields: [{ field: 'description', local: 'old', upstream: 'new' }],
            },
          ],
          source: { locale: 'en', models_url: '', vendors_url: '' },
        },
      },
    }),
  )
  const applied = page.waitForRequest(
    (request) =>
      new URL(request.url()).pathname === '/api/catalog/models/sync_upstream' &&
      request.method() === 'POST',
  )
  await page.route('**/api/catalog/models/sync_upstream', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          created_models: 1,
          created_vendors: 0,
          updated_models: 1,
          skipped_models: [],
          created_list: ['claude-sonnet-4'],
          updated_list: ['gpt-4o'],
          source: { locale: 'en', models_url: '', vendors_url: '' },
        },
      },
    }),
  )
  await page.goto('/admin/models')
  await page.getByRole('button', { name: '同步上游', exact: true }).click()
  await expect(page.getByText('claude-sonnet-4')).toBeVisible()
  await page.getByRole('checkbox', { name: /description/ }).check()
  await page.getByRole('button', { name: '应用同步', exact: true }).click()
  await page.getByRole('button', { name: '确认', exact: true }).click()
  const body = (await applied).postDataJSON()
  expect(body.overwrite).toEqual([{ model_name: 'gpt-4o', fields: ['description'] }])
})
