import { expect, test } from '@playwright/test'
import { fixtureAPI } from './fixtures'
import { marketGroups, marketShops, marketShopResponse } from './market-fixture'
import { selectLanguage } from './language-helper'

const shop = {
  id: '7001',
  name: '海港模型服务',
  description: '面向开发者的模型调用服务',
  group_count: 1,
  declared_models: ['gpt-4o'],
  tags: ['openai'],
  rating: { average_score: 9, rating_count: '123' },
  created_at: '2026-10-01T00:00:00Z',
  updated_at: '2026-10-07T00:00:00Z',
}
const group = {
  id: '349',
  group_id: 'group-test',
  routing_group: 'market_group-test',
  system_display_name: '公共模型渠道',
  provider_type: 'openai',
  approved_source_label: 'OpenAI',
  declared_models: ['gpt-4o'],
  model_prices: {},
  effective_model_prices: {},
  multiplier: 1,
  multiplier_ppm: 1000000,
  lifecycle_status: 'active',
  visibility: 'public',
  verification_status: 'passed',
  recent_request_series: [],
  recent_request_bucket_seconds: 3600,
  shop: { id: shop.id, name: shop.name },
  rating: { average_score: 8, rating_count: '1', viewer_stars: 0 },
}

test.beforeEach(async ({ page }) => {
  await fixtureAPI(page)
  await marketGroups(page, [group])
  await marketShops(page, [shop], { [shop.id]: [group] })
})

test('group links open a stable shop URL and return to the selected group', async ({
  page,
}, testInfo) => {
  await page.goto('/channel-market')
  await page.getByRole('button', { name: '海港模型服务 · 进入店铺', exact: true }).click()
  await expect
    .poll(() => JSON.parse(new URL(page.url()).searchParams.get('shop') ?? 'null'))
    .toBe(shop.id)
  await page.goBack()
  await expect(page.getByRole('tab', { name: '分组', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await page.goForward()
  await expect(page.getByRole('heading', { name: shop.name, exact: true })).toBeVisible()
  await page.goto('/channel-market?shop=7001')
  await expect(page.getByRole('heading', { name: shop.name, exact: true })).toBeVisible()
  await expect(page.getByText(shop.description, { exact: true })).toBeVisible()
  await page.screenshot({
    path: `E:/sh/Coding/cpa_bussiness/output/market-shops-20261007/shop-fixture-${testInfo.project.name}.png`,
    fullPage: true,
  })
  await page.getByRole('button', { name: '查看分组', exact: true }).click()
  await expect
    .poll(() => JSON.parse(new URL(page.url()).searchParams.get('group') ?? 'null'))
    .toBe(group.id)
  await expect(page.getByRole('button', { name: '收起', exact: true })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)
})

test('shop search keeps an empty state and does not poll shop summaries', async ({ page }) => {
  await page.clock.install()
  let reads = 0
  await page.route(/\/api\/marketplace\/shops(?:\?.*)?$/, (route) => {
    reads++
    return route.fulfill({ json: marketShopResponse([shop], new URL(route.request().url())) })
  })
  await page.goto('/channel-market')
  await page.getByRole('tab', { name: '店铺', exact: true }).click()
  await expect(page.getByRole('heading', { name: shop.name, exact: true })).toBeVisible()
  await page.getByLabel('搜索店铺或模型', { exact: true }).fill('不存在的店铺')
  await page.clock.fastForward(300)
  await expect(page.getByText('没有匹配的店铺', { exact: true })).toBeVisible()
  expect(reads).toBe(2)
  const completedReads = reads
  await page.clock.fastForward(65_000)
  expect(reads).toBe(completedReads)
})

test('owner submits a proposed profile while the existing public shop remains visible', async ({
  page,
}) => {
  let body: unknown
  await page.route('**/api/marketplace/shop/mine', async (route) => {
    if (route.request().method() === 'PATCH') {
      body = route.request().postDataJSON()
      return route.fulfill({
        json: {
          success: true,
          data: {
            ...shop,
            submitted_name: '海港推理服务',
            submitted_description: '',
            review_status: 'pending',
          },
        },
      })
    }
    return route.fulfill({
      json: {
        success: true,
        data: { ...shop, submitted_description: '', review_status: 'approved' },
      },
    })
  })
  await page.goto('/my-channels')
  await page.getByRole('tab', { name: '店铺资料', exact: true }).click()
  await page.getByLabel('店铺名称', { exact: true }).fill('海港推理服务')
  await page.getByLabel('店铺简介', { exact: true }).fill('')
  await page.getByRole('button', { name: '提交店铺资料审核', exact: true }).click()
  await expect(page.getByText('店铺资料已提交审核', { exact: true })).toBeVisible()
  expect(body).toEqual({ name: '海港推理服务', description: '' })
  await expect(page.getByText(shop.name, { exact: true })).toBeVisible()
})

test('shop profile validation errors remain visible without reporting success', async ({
  page,
}) => {
  await page.route('**/api/marketplace/shop/mine', (route) =>
    route.request().method() === 'PATCH'
      ? route.fulfill({
          status: 400,
          json: { success: false, message: '店铺资料包含不允许的内容' },
        })
      : route.fulfill({ json: { success: true, data: { ...shop, review_status: 'approved' } } }),
  )
  await page.goto('/my-channels')
  await page.getByRole('tab', { name: '店铺资料', exact: true }).click()
  await page.getByLabel('店铺名称', { exact: true }).fill('广告群')
  await page.getByRole('button', { name: '提交店铺资料审核', exact: true }).click()
  await expect(page.getByText('店铺资料包含不允许的内容', { exact: true })).toBeVisible()
  await expect(page.getByText('店铺资料已提交审核', { exact: true })).toHaveCount(0)
})

for (const reviewStatus of ['pending', 'rejected']) {
  test(`system name reset remains empty after remounting a ${reviewStatus} profile`, async ({
    page,
  }) => {
    const submissions: unknown[] = []
    let profile = {
      ...shop,
      review_status: 'approved',
      submitted_description: shop.description,
    }
    await page.route('**/api/marketplace/shop/mine', (route) => {
      if (route.request().method() === 'PATCH') {
        const body = route.request().postDataJSON() as { name: string; description: string }
        submissions.push(body)
        profile = {
          ...profile,
          review_status: reviewStatus,
          submitted_description: body.description,
          updated_at: `2026-10-07T00:00:0${submissions.length}Z`,
        }
      }
      // The real API omits an empty submitted_name through json omitempty.
      return route.fulfill({ json: { success: true, data: profile } })
    })
    await page.goto('/my-channels')
    await page.getByRole('tab', { name: '店铺资料', exact: true }).click()
    await page.getByLabel('店铺名称', { exact: true }).fill('')
    await page.getByRole('button', { name: '提交店铺资料审核', exact: true }).click()
    await expect(page.getByText('店铺资料已提交审核', { exact: true })).toBeVisible()
    expect(submissions[0]).toEqual({ name: '', description: shop.description })

    await page.reload()
    await page.getByRole('tab', { name: '店铺资料', exact: true }).click()
    await expect(page.getByLabel('店铺名称', { exact: true })).toHaveValue('')
    await expect(page.getByText(shop.name, { exact: true })).toBeVisible()
    await page.getByRole('textbox', { name: '店铺简介', exact: true }).fill('更新后的服务介绍')
    await page.getByRole('button', { name: '提交店铺资料审核', exact: true }).click()
    await expect.poll(() => submissions.length).toBe(2)
    expect(submissions[1]).toEqual({ name: '', description: '更新后的服务介绍' })
    await expect(page.getByText(shop.name, { exact: true })).toBeVisible()
  })
}

test('system name reset is explicitly identified in the administrator review', async ({ page }) => {
  await page.route('**/api/marketplace/admin/shops', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: [{ ...shop, review_status: 'pending', submitted_description: '' }],
      },
    }),
  )
  await page.goto('/market-admin')
  await page.locator('.shop-review summary').click()
  await expect(
    page.getByRole('heading', { name: '恢复系统名称 · 渠道店铺 #7001', exact: true }),
  ).toBeVisible()
  await expect(page.getByText(`当前公开名称：${shop.name}`, { exact: false })).toBeVisible()
})

test('main-site ratings submit only selected stars and refresh after a change without polling', async ({
  page,
}) => {
  await page.clock.install()
  let stars = 0
  let reads = 0
  let submitted: unknown
  await page.route('**/api/marketplace/groups/group-test/rating', (route) => {
    if (route.request().method() === 'POST') {
      submitted = route.request().postDataJSON()
      stars = Number((submitted as { stars: number }).stars)
    } else reads++
    return route.fulfill({
      json: {
        success: true,
        data: {
          group_id: 'group-test',
          channel_id: '349',
          channel: {
            average_score: stars * 2,
            rating_count: stars ? '1' : '0',
            viewer_stars: stars,
          },
          seller: { average_score: stars * 2, rating_count: stars ? '1' : '0', viewer_stars: 0 },
          can_rate: true,
          eligibility_reason: '',
        },
      },
    })
  })
  await page.goto('/channel-market?group=349')
  await page.getByRole('radio', { name: '4 / 5', exact: true }).check()
  await page.getByRole('button', { name: '提交评分', exact: true }).click()
  await expect(page.getByText('评分已保存，社区展示将同步更新。', { exact: true })).toBeVisible()
  await expect(page.locator('.market-ratings .market-rating-summary strong')).toHaveText('4.0 / 5')
  expect(submitted).toEqual({ stars: 4 })
  await page.clock.fastForward(65_000)
  expect(reads).toBe(2)
})

test('non-consumers cannot rate and a server-side denial does not report a saved rating', async ({
  page,
}) => {
  await page.goto('/channel-market?group=349')
  await expect(
    page.getByText('真实使用此分组后可以评价，服务失败的调用也可符合资格。', { exact: true }),
  ).toBeVisible()
  await expect(page.getByRole('radio')).toHaveCount(0)
  await page.route('**/api/marketplace/groups/group-test/rating', (route) =>
    route.request().method() === 'POST'
      ? route.fulfill({ status: 403, json: { success: false, message: '当前账号不符合评分条件' } })
      : route.fulfill({
          json: {
            success: true,
            data: {
              group_id: 'group-test',
              channel_id: '349',
              channel: { average_score: 0, rating_count: 0, viewer_stars: 0 },
              seller: { average_score: 0, rating_count: 0, viewer_stars: 0 },
              can_rate: true,
              eligibility_reason: '',
            },
          },
        }),
  )
  await page.reload()
  await page.getByRole('radio', { name: '5 / 5', exact: true }).check()
  await page.getByRole('button', { name: '提交评分', exact: true }).click()
  await expect(page.getByText('当前账号不符合评分条件', { exact: true })).toBeVisible()
  await expect(page.getByText('评分已保存，社区展示将同步更新。', { exact: true })).toHaveCount(0)
})

test('store controls localize across nine languages and Arabic remains within the viewport', async ({
  page,
}) => {
  await page.goto('/channel-market?shop=7001')
  for (const [language, code, allShops] of [
    ['繁體中文（香港）', 'zh-HK', '全部店舖'],
    ['简体中文', 'zh-CN', '全部店铺'],
    ['English', 'en', 'All shops'],
    ['日本語', 'ja', 'すべてのショップ'],
    ['Русский', 'ru', 'Все магазины'],
    ['한국어', 'ko', '모든 스토어'],
    ['Français', 'fr', 'Toutes les boutiques'],
    ['Deutsch', 'de', 'Alle Shops'],
    ['العربية', 'ar', 'جميع المتاجر'],
  ]) {
    await selectLanguage(page, language)
    await expect(page.locator('html')).toHaveAttribute('lang', code)
    await expect(page.getByRole('button', { name: allShops, exact: true })).toBeVisible()
    const overflow = await page.evaluate(() =>
      [...document.querySelectorAll('body *')]
        .filter((node) => {
          const bounds = node.getBoundingClientRect()
          return bounds.width > 0 && (bounds.right > innerWidth + 1 || bounds.left < -1)
        })
        .slice(0, 12)
        .map((node) => ({
          tag: node.tagName,
          class: node.className,
          text: node.textContent?.trim().slice(0, 80),
          right: node.getBoundingClientRect().right,
        })),
    )
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth > innerWidth),
      `${code}: ${JSON.stringify(overflow)}`,
    ).toBe(false)
  }
  await expect(page.locator('html')).toHaveAttribute('dir', 'rtl')
})
