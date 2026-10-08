import { test, expect, type Page } from '@playwright/test'
import { fixtureAPI } from './fixtures'

const quote = {
  mode: 'per_token',
  unit: 'request',
  input_per_million: '1.234567',
  output_per_million: '2',
  cache_read_per_million: '0.1234567',
  cache_write_per_million: '1.543208',
  per_unit: '0',
}
const groups = Array.from({ length: 49 }, (_, index) => ({
  id: String(index + 1),
  internal_channel_id: index + 1,
  owner_user_id: 1,
  group_id: `group-${index + 1}`,
  routing_group: `market_group-${index + 1}`,
  public_slug: `slug-${index + 1}`,
  system_display_name: `渠道 ${String(index + 1).padStart(2, '0')}`,
  provider_type: 'openai_compatible',
  approved_source_label: '',
  declared_models: index === 48 ? ['gpt-test', 'model-beyond-page'] : ['gpt-test'],
  model_prices: {},
  effective_model_prices: { 'gpt-test': quote, 'model-beyond-page': quote },
  tags: ['openai'],
  recent_request_bucket_seconds: 3600,
  recent_request_series: [],
  multiplier_ppm: 1000000,
  multiplier: 1,
  visibility: 'public',
  lifecycle_status: 'active',
  verification_status: 'passed',
  last_review_reason: '',
  max_concurrency: 10,
  user_max_concurrency: 2,
  model_verification_results: [],
  created_at: '2026-10-07T10:00:00Z',
  updated_at: '2026-10-07T10:00:00Z',
  shop: { id: '701', name: '示例店铺' },
  rating: { average_score: 8, rating_count: 2 },
}))
const shop = {
  id: '701',
  name: '示例店铺',
  description: '提供文本模型调用。',
  group_count: 49,
  declared_models: ['gpt-test', 'model-beyond-page'],
  tags: ['openai'],
  rating: { average_score: 8, rating_count: 2 },
  created_at: '2026-10-07T10:00:00Z',
  updated_at: '2026-10-07T10:00:00Z',
}

async function browseFixture(page: Page) {
  await fixtureAPI(page)
  await page.route('**/api/marketplace/**', async (route) => {
    const url = new URL(route.request().url())
    const pathname = url.pathname
    const q = url.searchParams
    const pageNumber = Number(q.get('page') ?? 1)
    const size = Number(q.get('page_size') ?? 24)
    const search = (q.get('search') ?? '').trim()
    const model = q.get('model') ?? ''
    const filtered = groups.filter(
      (group) =>
        (!search || group.system_display_name.includes(search)) &&
        (!model || group.declared_models.includes(model)),
    )
    const pagination = { page: pageNumber, page_size: size, total: filtered.length }
    const pageGroups = filtered.slice((pageNumber - 1) * size, pageNumber * size)
    let payload: unknown
    if (
      pathname === '/api/marketplace/groups' ||
      pathname === '/api/marketplace/key-group-options'
    ) {
      payload = {
        success: true,
        message: '',
        data: pageGroups.map((group) => ({
          ...group,
          effective_model_prices: model ? { [model]: quote } : {},
        })),
        pagination,
        models: shop.declared_models,
      }
    } else if (pathname === '/api/marketplace/shops') {
      payload = {
        success: true,
        message: '',
        data: [shop],
        pagination: { ...pagination, total: 1 },
        models: shop.declared_models,
      }
    } else if (pathname === '/api/marketplace/shops/701') {
      payload = { success: true, message: '', data: { shop, groups: pageGroups, pagination } }
    } else if (/^\/api\/marketplace\/groups\/[^/]+$/.test(pathname)) {
      const id = pathname.split('/').at(-1)
      const group = groups.find((group) => group.id === id || group.group_id === id)
      if (!group)
        return route.fulfill({
          status: 404,
          json: { success: false, message: '资源不存在或无权访问' },
        })
      payload = { success: true, message: '', data: group }
    } else return route.fallback()
    await route.fulfill({ json: payload })
  })
}

test.beforeEach(async ({ page }) => browseFixture(page))

test('server page totals, cross-page search, model facets and direct group details', async ({
  page,
}, info) => {
  await page.goto('/channel-market')
  await expect(page.locator('.market-listing')).toHaveCount(24)
  await expect(page.locator('.market-result-bar strong')).toHaveText('49')
  await page.getByRole('button', { name: '下一页', exact: true }).click()
  await expect(page.getByRole('heading', { name: '渠道 25', exact: true })).toBeVisible()
  await page.getByLabel('搜索渠道或模型').fill('渠道 49')
  await expect(page.locator('.market-listing')).toHaveCount(1)
  await expect(page.getByRole('heading', { name: '渠道 49', exact: true })).toBeVisible()
  await page.getByLabel('搜索渠道或模型').fill('')
  await page.getByLabel('选择比较的模型').selectOption('model-beyond-page')
  await expect(page.locator('.market-listing')).toHaveCount(1)
  await expect(page.getByRole('heading', { name: '渠道 49', exact: true })).toBeVisible()
  await page.goto('/channel-market?group=49')
  await expect(page.locator('#market-detail-group-49')).toBeVisible()
  await page.locator('#market-detail-group-49').getByText('模型价格', { exact: true }).click()
  await expect(page.locator('#market-detail-group-49 .model-price-lines').first()).toBeVisible()
  if (info.project.name === 'mobile') {
    await expect(page.getByRole('combobox', { name: '排序', exact: true })).toBeHidden()
    await page.getByRole('button', { name: '筛选与排序', exact: true }).click()
    await expect(page.getByRole('combobox', { name: '排序', exact: true })).toBeVisible()
  }
  await page.evaluate(() => window.scrollTo(0, 0))
  await page.screenshot({
    path: `E:/sh/Coding/cpa_bussiness/output/market-pagination-${info.project.name}.png`,
    fullPage: false,
    scale: 'css',
  })
})

test('comparison survives page navigation and shop inventory uses independent pagination', async ({
  page,
}) => {
  await page.goto('/channel-market?model=gpt-test')
  await page.getByRole('checkbox', { name: '加入比较 渠道 01', exact: true }).check()
  await page.getByRole('button', { name: '下一页', exact: true }).click()
  await page.getByRole('checkbox', { name: '加入比较 渠道 25', exact: true }).check()
  await expect(
    page
      .getByRole('table', { name: '同模型分组比较 · gpt-test' })
      .getByText('渠道 01', { exact: true }),
  ).toBeVisible()
  await expect(
    page
      .getByRole('table', { name: '同模型分组比较 · gpt-test' })
      .getByText('渠道 25', { exact: true }),
  ).toBeVisible()
  await page.goto('/channel-market?shop=701&view=shops')
  await expect(page.locator('.market-shop-groups article')).toHaveCount(24)
  await page.getByRole('button', { name: '下一页', exact: true }).click()
  await expect(page.getByRole('heading', { name: '渠道 25', exact: true })).toBeVisible()
  await page.getByLabel('搜索店铺或模型').fill('渠道 49')
  await expect(page.locator('.market-shop-groups article')).toHaveCount(1)
  await expect(page.getByRole('heading', { name: '渠道 49', exact: true })).toBeVisible()
})

test('unavailable direct group returns an actionable error without exposing details', async ({
  page,
}) => {
  await page.goto('/channel-market?group=999999')
  await expect(page.getByText('所选分组不存在、已下架或无访问权限。')).toBeVisible()
  await expect(page.locator('.market-listing-detail')).toHaveCount(0)
})

test('a catalog shrinking below the current page recovers to the remaining page', async ({
  page,
}) => {
  await page.goto('/channel-market')
  await expect(page.locator('.market-listing')).toHaveCount(24)
  const requestedPages: number[] = []
  await page.route('**/api/marketplace/key-group-options?**', async (route) => {
    const q = new URL(route.request().url()).searchParams
    const current = Number(q.get('page'))
    requestedPages.push(current)
    await route.fulfill({
      json: {
        success: true,
        message: '',
        data: current === 1 ? [groups[0]] : [],
        pagination: { page: current, page_size: 24, total: 1 },
        models: ['gpt-test'],
      },
    })
  })
  await page.getByRole('button', { name: '下一页', exact: true }).click()
  await expect(page.locator('.market-listing')).toHaveCount(1)
  await expect(page.getByRole('heading', { name: '渠道 01', exact: true })).toBeVisible()
  expect(requestedPages).toEqual([2, 1])
})
