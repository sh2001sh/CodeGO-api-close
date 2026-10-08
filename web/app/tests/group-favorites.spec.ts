import { expect, test } from '@playwright/test'
import { fixtureAPI } from './fixtures'
import { marketGroups } from './market-fixture'

const group = {
  id: '101',
  group_id: 'group-101',
  public_slug: '101',
  system_display_name: '开发主分组',
  declared_models: ['gpt-4o'],
  provider_type: 'openai_compatible',
  approved_source_label: 'OpenAI',
  multiplier: 1,
  multiplier_ppm: 1_000_000,
  visibility: 'public',
  lifecycle_status: 'active',
  verification_status: 'passed',
  model_verification_results: [],
  recent_request_series: [],
}
const bookmark = {
  group_id: group.group_id,
  id: group.id,
  public_slug: group.public_slug,
  system_display_name: group.system_display_name,
  declared_models: group.declared_models,
  multiplier: 1,
  available: true,
  created_at: '2026-10-08T00:00:00Z',
}
test.beforeEach(async ({ page }) => {
  await fixtureAPI(page)
})

test('market favorites persist across navigation and the retired model page redirects', async ({
  page,
}) => {
  let saved = false
  await marketGroups(page, [group])
  await page.route('**/api/marketplace/group-favorites**', async (route) => {
    const request = route.request()
    if (request.method() === 'PUT') {
      const body = request.postDataJSON()
      expect(body.group_id).toBe(group.group_id)
      saved = body.favorite
      return route.fulfill({ json: { success: true, data: body } })
    }
    await route.fulfill({
      json: {
        success: true,
        data: saved ? [bookmark] : [],
        pagination: { page: 1, page_size: 24, total: saved ? 1 : 0 },
      },
    })
  })
  await page.goto('/channel-market')
  const row = page.locator('.market-listing').filter({ hasText: group.system_display_name })
  await row.getByRole('button', { name: '收藏分组', exact: true }).click()
  await expect(row.getByRole('button', { name: '已收藏', exact: true })).toHaveAttribute(
    'aria-pressed',
    'true',
  )
  await page.goto('/model-favorites')
  await expect(page).toHaveURL(/\/group-favorites$/)
  await expect(page.getByRole('heading', { name: '分组收藏', exact: true })).toBeVisible()
  await expect(page.getByText(group.system_display_name, { exact: true })).toBeVisible()
  await expect(page.getByText('101', { exact: true })).toBeVisible()
  await page.getByRole('link', { name: '查看分组', exact: true }).click()
  await expect(page).toHaveURL(
    (url) => JSON.parse(url.searchParams.get('group') ?? 'null') === '101',
  )
  await expect(row.getByRole('button', { name: '收起', exact: true })).toBeVisible()
  await page.goto('/group-favorites')
  await page.getByRole('button', { name: '取消收藏', exact: true }).click()
  await expect(page.getByText('尚未收藏分组', { exact: true })).toBeVisible()
  await page.goto('/channel-market')
  await expect(row.getByRole('button', { name: '收藏分组', exact: true })).toHaveAttribute(
    'aria-pressed',
    'false',
  )
})

test('unavailable group bookmarks can be removed and list failures expose retry', async ({
  page,
}) => {
  let failed = true
  let saved = true
  await page.route('**/api/marketplace/group-favorites**', async (route) => {
    if (route.request().method() === 'PUT') {
      expect(route.request().postDataJSON()).toEqual({ group_id: group.group_id, favorite: false })
      saved = false
      return route.fulfill({
        json: { success: true, data: { group_id: group.group_id, favorite: false } },
      })
    }
    if (failed)
      return route.fulfill({ status: 503, json: { success: false, message: '收藏暂时不可用' } })
    return route.fulfill({
      json: {
        success: true,
        data: saved
          ? [
              {
                group_id: group.group_id,
                id: '101',
                available: false,
                created_at: bookmark.created_at,
              },
            ]
          : [],
        pagination: { page: 1, page_size: 24, total: saved ? 1 : 0 },
      },
    })
  })
  await page.goto('/group-favorites')
  await expect(page.getByText('收藏暂时不可用', { exact: true })).toBeVisible()
  failed = false
  await page.getByRole('button', { name: '重试', exact: true }).click()
  await expect(page.getByText('暂不可访问的分组', { exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: '查看分组', exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: '取消收藏', exact: true }).click()
  await expect(page.getByText('尚未收藏分组', { exact: true })).toBeVisible()
})

test('model catalog no longer fetches or offers model favorites', async ({ page }) => {
  const requests: string[] = []
  page.on('request', (request) => {
    if (request.url().includes('/api/models/favorites')) requests.push(request.url())
  })
  await page.goto('/models')
  await expect(page.getByRole('heading', { name: '模型', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '添加收藏', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '取消收藏', exact: true })).toHaveCount(0)
  expect(requests).toEqual([])
})
