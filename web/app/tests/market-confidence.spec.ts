import { expect, test } from '@playwright/test'
import { fixtureAPI } from './fixtures'
import { marketGroups } from './market-fixture'

const group = (id: string, input: string, output: string) => ({
  id,
  group_id: `market-${id}`,
  public_slug: `group-${id}`,
  internal_channel_id: Number(id),
  owner_user_id: 1,
  system_display_name: `分组 ${id}`,
  provider_type: 'openai',
  declared_models: ['gpt-4o'],
  multiplier: 2,
  multiplier_ppm: 2000000,
  visibility: 'public',
  lifecycle_status: 'active',
  verification_status: 'passed',
  max_concurrency: 20,
  user_max_concurrency: 5,
  qps: 10,
  effective_model_prices: {
    'gpt-4o': {
      mode: 'per_token',
      unit: 'tokens',
      input_per_million: input,
      output_per_million: output,
      cache_read_per_million: '0',
      cache_write_per_million: '0',
      per_unit: '0',
    },
  },
  model_prices: {},
  recent_request_series: [],
  maintenance_window: '',
})

test.beforeEach(async ({ page }) => fixtureAPI(page))

test('Arabic comparison and documentation preserve RTL layout with explicit English article fallback', async ({
  page,
}) => {
  await page.addInitScript(() => localStorage.setItem('codego.locale', 'ar'))
  await marketGroups(page, [group('101', '2', '3'), group('102', '4', '6')])
  await page.goto('/channel-market?model=gpt-4o')
  await expect(page.locator('html')).toHaveAttribute('dir', 'rtl')
  await page.locator('.market-compare-choice input').nth(0).check()
  await page.locator('.market-compare-choice input').nth(1).check()
  await expect(page.locator('.market-estimate-amount')).toHaveText(['3.5 credits', '7 credits'])
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  )
  await page.goto('/docs?article=route-pools')
  await expect(page.locator('.docs-language-notice')).toBeVisible()
  await expect(page.locator('article')).toHaveAttribute('lang', 'en')
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  )
})

test('same-model comparison applies quoted rates once and keeps unknown performance explicit', async ({
  page,
}) => {
  await marketGroups(page, [group('101', '2', '3'), group('102', '4', '6')])
  await page.goto('/channel-market?model=gpt-4o')
  await page.getByRole('checkbox', { name: '加入比较 分组 101', exact: true }).check()
  await page.getByRole('checkbox', { name: '加入比较 分组 102', exact: true }).check()
  await expect(page.locator('.market-estimate-amount')).toHaveText(['3.5 credits', '7 credits'])
  const timing = page
    .locator('.market-comparison-table tr')
    .filter({ has: page.getByRole('rowheader', { name: /首字时间 P50/ }) })
  await expect(timing).toContainText('暂无数据')
  await page.getByLabel('预计请求数', { exact: true }).fill('-1')
  await expect(page.locator('.market-comparison-table')).toContainText('请输入有效非负整数')
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  )
})

test('documentation supports deep links, search, legacy anchors and missing articles', async ({
  page,
  isMobile,
}) => {
  await page.goto('/docs?article=route-pools')
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('用户路由池与自动更新')
  if (isMobile) await page.getByRole('button', { name: '文档分区', exact: true }).click()
  await page.getByRole('searchbox').fill('nonexistent_article_8374')
  await expect(page.locator('.docs-no-results')).toBeVisible()
  await page.goto('/docs#billing')
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('价格、用量与费用')
  await page.goto('/docs?article=does-not-exist')
  await expect(page.locator('.docs-missing')).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  )
})

test('supplier agreement rejection remains visible and acceptance unlocks new supply', async ({
  page,
}) => {
  let accepted = false
  let attempts = 0
  let payload: unknown
  const record = {
    document: 'supplier',
    version: '2026-10-07',
    locale: 'zh-CN',
    accepted_at: '2026-10-07T00:00:00Z',
  }
  await page.route('**/api/user/policy-acceptance', async (route) => {
    if (route.request().method() === 'GET')
      return route.fulfill({ json: { success: true, data: accepted ? [record] : [] } })
    payload = route.request().postDataJSON()
    if (++attempts === 1)
      return route.fulfill({
        status: 403,
        json: { success: false, message: '当前协议暂时无法接受' },
      })
    accepted = true
    return route.fulfill({ json: { success: true, data: record } })
  })
  await page.goto('/my-channels')
  await page.getByRole('button', { name: '提交渠道', exact: true }).click()
  await expect(page.getByLabel('上游地址', { exact: true })).toHaveCount(0)
  await page
    .getByRole('checkbox', { name: '我已阅读并同意渠道供给与结算协议。', exact: true })
    .check()
  await page.getByRole('button', { name: '同意并继续', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('当前协议暂时无法接受')
  await page.getByRole('button', { name: '同意并继续', exact: true }).click()
  await expect(page.getByLabel('上游地址', { exact: true })).toBeVisible()
  expect(payload).toEqual({ document: 'supplier', version: '2026-10-07', locale: 'zh-CN' })
})

test('owner overview validates date boundaries and passes filters to settlement export', async ({
  page,
}) => {
  await page.goto('/my-channels')
  await expect(page.getByRole('tab', { name: '运营概览', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await page.getByLabel('开始时间', { exact: true }).fill('2026-10-07T12:00')
  await page.getByLabel('结束时间', { exact: true }).fill('2026-10-06T12:00')
  await page.getByRole('button', { name: '应用筛选', exact: true }).click()
  await expect(page.getByRole('alert')).toBeVisible()
  await page.getByLabel('开始时间', { exact: true }).fill('2026-10-01T12:00')
  await page.getByRole('button', { name: '应用筛选', exact: true }).click()
  await expect(page.getByRole('link', { name: '导出筛选结果', exact: true })).toHaveAttribute(
    'href',
    /analytics\/export\?from=.*&to=/,
  )
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  )
})
