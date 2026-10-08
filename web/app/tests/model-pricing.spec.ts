import { test, expect } from '@playwright/test'
import { fixtureAPI, user } from './fixtures'

const price = {
  mode: 'per_token',
  unit: 'request',
  input_per_million: '0.25',
  output_per_million: '1',
  cache_read_per_million: '0.025',
  cache_write_per_million: '0.3125',
  per_unit: '0',
}
const catalog = [
  {
    name: 'gpt-example',
    model_id: '17',
    vendor: 'OpenAI',
    groups: [{ slug: 'public', name: '公开分组', multiplier: '0.125000', verified: true, price }],
  },
  {
    name: 'image-example',
    model_id: '18',
    groups: [
      {
        slug: 'images',
        name: '图片分组',
        multiplier: '1',
        verified: false,
        price: { ...price, mode: 'per_request', unit: 'image', per_unit: '0.015' },
      },
    ],
  },
  {
    name: 'dynamic-example',
    groups: [
      {
        slug: 'dynamic',
        name: '动态分组',
        multiplier: '1',
        verified: false,
        price: { ...price, mode: 'expression', expression: 'p * 5 + c * 20' },
      },
    ],
  },
  {
    name: 'unpriced-example',
    groups: [{ slug: 'unknown', name: '未报价分组', multiplier: '1', verified: false }],
  },
]

test.beforeEach(async ({ page }) => {
  await fixtureAPI(page)
  await page.route('**/api/user/self', (route) =>
    route.fulfill({ json: { success: true, data: { ...user, role: 'user' } } }),
  )
  await page.route('**/api/public/models', (route) =>
    route.fulfill({ json: { success: true, data: catalog } }),
  )
})

test('public quotations show token, cache and media units without treating missing or dynamic prices as free', async ({
  page,
}) => {
  await page.route('**/api/models/favorites/', (route) =>
    route.fulfill({ json: { success: true, data: { model_ids: [], models: [] } } }),
  )
  await page.goto('/models')
  const token = page
    .locator('.model-row')
    .filter({ has: page.getByText('gpt-example', { exact: true }) })
  await expect(token.getByText('0.25', { exact: true }).first()).toBeVisible()
  await expect(token.getByText('0.025', { exact: true }).first()).toBeVisible()
  await expect(token.getByText('0.3125', { exact: true }).first()).toBeVisible()
  await expect(page.getByText('0.015 credits').first()).toBeVisible()
  await expect(page.getByText('每张图片').first()).toBeVisible()
  const dynamic = page
    .locator('.model-row')
    .filter({ has: page.getByText('dynamic-example', { exact: true }) })
  await dynamic.locator('.model-row-price summary').click()
  await expect(dynamic.getByText('p * 5 + c * 20').first()).toBeVisible()
  await expect(dynamic.locator('.model-price-lines')).toHaveCount(0)
  await expect(page.getByText('暂未提供价格').first()).toBeVisible()
  const width = await page.evaluate(() => ({
    document: document.documentElement.scrollWidth,
    viewport: window.innerWidth,
  }))
  expect(width.document).toBeLessThanOrEqual(width.viewport)
})

test('ordinary users can discover a model, favorite it, and see its name in their favorites', async ({
  page,
}) => {
  let ids: string[] = []
  await page.route('**/api/models/favorites/', async (route) => {
    if (route.request().method() === 'PUT') {
      const body = route.request().postDataJSON()
      expect(String(body.model_id)).toBe('17')
      ids = body.favorite ? ['17'] : []
    }
    await route.fulfill({
      json: {
        success: true,
        data: { model_ids: ids, models: ids.map((id) => ({ id, model_name: 'gpt-example' })) },
      },
    })
  })
  await page.goto('/models')
  const row = page
    .locator('.model-row')
    .filter({ has: page.getByText('gpt-example', { exact: true }) })
  await row.getByRole('button', { name: '添加收藏', exact: true }).click()
  await expect(row.getByRole('button', { name: '取消收藏', exact: true })).toBeVisible()
  await page.goto('/model-favorites')
  await expect(page.getByRole('cell', { name: 'gpt-example', exact: true })).toBeVisible()
  await page.getByRole('button', { name: '取消收藏', exact: true }).click()
  await expect(page.getByText('尚未收藏模型。', { exact: true })).toBeVisible()
  await page.getByLabel('选择模型', { exact: true }).selectOption('17')
  await page.getByRole('button', { name: '添加收藏', exact: true }).click()
  await expect(page.getByRole('cell', { name: 'gpt-example', exact: true })).toBeVisible()
})

test('a public catalog failure has an explicit retry and does not invent zero prices', async ({
  page,
}) => {
  let failed = true
  await page.route('**/api/public/models', (route) =>
    failed
      ? route.fulfill({ status: 503, json: { success: false, message: '目录暂时不可用' } })
      : route.fulfill({ json: { success: true, data: catalog } }),
  )
  await page.route('**/api/models/favorites/', (route) =>
    route.fulfill({ json: { success: true, data: { model_ids: [], models: [] } } }),
  )
  await page.goto('/models')
  await expect(page.getByRole('alert')).toHaveText('目录暂时不可用')
  failed = false
  await page.getByRole('button', { name: '重试', exact: true }).click()
  await expect(page.getByText('gpt-example', { exact: true })).toBeVisible()
})
