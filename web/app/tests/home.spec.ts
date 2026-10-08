import { test, expect } from '@playwright/test'
import { fixtureAPI } from './fixtures'
import { selectLanguage } from './language-helper'
import { marketBrowseResponse } from './market-fixture'

test.beforeEach(async ({ page }) => fixtureAPI(page))

const group = {
  id: 'home-group',
  group_id: 'home-group',
  public_slug: 'home-route',
  system_display_name: '公开模型路由',
  provider_type: 'openai',
  approved_source_label: '官方渠道',
  declared_models: ['gpt-4o', 'claude-sonnet-4'],
  multiplier_ppm: 1000000,
  visibility: 'public',
  lifecycle_status: 'active',
  verification_status: 'passed',
  model_verification_results: [
    { model: 'gpt-4o', listed: true, status: 'passed', latency_ms: 320 },
  ],
}

test('home search filters real groups and restores them after an empty result', async ({
  page,
}) => {
  await page.route('**/api/marketplace/groups?*', (route) =>
    route.fulfill({ json: marketBrowseResponse([group], new URL(route.request().url())) }),
  )
  await page.goto('/')
  const market = page.getByRole('region', { name: '模型路由行情', exact: true })
  const discovery = page.getByRole('region', { name: '探索模型', exact: true })
  await expect(discovery.getByText('claude-sonnet-4', { exact: true })).toBeVisible()
  await expect(discovery.getByText('gpt-4o', { exact: true })).toBeVisible()
  await expect(discovery.getByText('热门模型', { exact: true })).toHaveCount(0)
  await expect(market.getByRole('button', { name: '公开模型路由', exact: true })).toBeVisible()
  const search = page.getByRole('searchbox', { name: '搜索分组或模型', exact: true })
  await search.fill('claude')
  await search.press('Enter')
  await expect(market.getByRole('button', { name: '公开模型路由', exact: true })).toBeVisible()
  await search.fill('missing-model')
  await expect(market.getByText('没有匹配的分组', { exact: true })).toBeVisible()
  await market.getByRole('button', { name: '清除搜索', exact: true }).click()
  await expect(search).toHaveValue('')
  await expect(market.getByRole('button', { name: '公开模型路由', exact: true })).toBeVisible()
})

test('home market reports an API failure and recovers through retry', async ({ page }) => {
  let fail = true
  await page.route('**/api/marketplace/groups?*', (route) =>
    fail
      ? route.fulfill({ status: 503, json: { success: false, message: '模型市场暂时不可用' } })
      : route.fulfill({ json: marketBrowseResponse([group], new URL(route.request().url())) }),
  )
  await page.goto('/')
  await expect(page.getByRole('alert')).toHaveText('模型市场暂时不可用')
  await expect(page.getByText('暂无公开分组', { exact: true })).toHaveCount(0)
  fail = false
  await page.getByRole('button', { name: '重试', exact: true }).click()
  await expect(page.getByRole('button', { name: '公开模型路由', exact: true })).toBeVisible()
  await expect(page.getByRole('alert')).toHaveCount(0)
})

test('brand and model discovery remain usable with reduced motion and English', async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/')
  await expect(page.getByRole('heading', { level: 1, name: '选择模型，开始构建。' })).toBeVisible()
  await expect(page.locator('.home-intro-art img')).toHaveCSS('animation-name', 'none')
  await expect(page.getByText(/new-api|QuantumNous/)).toHaveCount(0)
  await selectLanguage(page, 'English')
  await expect(
    page.getByRole('heading', { level: 1, name: 'Choose a model. Start building.' }),
  ).toBeVisible()
  await page.getByRole('link', { name: 'Models & pricing', exact: true }).first().click()
  await expect(page).toHaveURL(/\/models$/)
  await expect(page.getByRole('heading', { name: 'Model', exact: true })).toBeVisible()
  await page.goto('/sign-in')
  await expect(page.getByText(/new-api|QuantumNous/)).toHaveCount(0)
})
