import { expect, test } from '@playwright/test'
import { fixtureAPI } from './fixtures'

test('navigation updates one title and removes sensitive parameters from canonical links', async ({
  page,
}) => {
  await fixtureAPI(page)
  await page.goto('/docs?article=quickstart&token=fixture-secret')
  await expect(page).toHaveTitle('文档 · CodeGo AI')
  await expect(page.locator('head title')).toHaveCount(1)
  await expect(page.locator('link[rel="canonical"]')).toHaveAttribute(
    'href',
    /\/docs\?article=quickstart$/,
  )
  await expect(page.locator('meta[name="robots"]')).toHaveAttribute('content', 'index,follow')
  await page.goto('/dashboard?token=fixture-secret')
  await expect(page).toHaveTitle('仪表盘 · CodeGo AI')
  await expect(page.locator('head title')).toHaveCount(1)
  await expect(page.locator('link[rel="canonical"]')).toHaveAttribute('href', /\/dashboard$/)
  await expect(page.locator('meta[name="robots"]')).toHaveAttribute('content', 'noindex,nofollow')
  await expect(page.locator('meta[property="og:url"]')).toHaveAttribute('content', /\/dashboard$/)
})

test('pending signed-in session keeps the header stable without displaying login links', async ({
  page,
}) => {
  await fixtureAPI(page)
  let finishSession!: () => void
  const pending = new Promise<void>((resolve) => {
    finishSession = resolve
  })
  await page.route('**/api/user/self', async (route) => {
    await pending
    await route.fallback()
  })
  await page.goto('/', { waitUntil: 'domcontentloaded' })
  await expect(page.locator('.account-placeholder')).toBeVisible()
  await expect(
    page.locator('.topbar').getByRole('link', { name: '登录', exact: true }),
  ).toHaveCount(0)
  finishSession()
  await expect(page.locator('.account-placeholder')).toHaveCount(0)
  await expect(
    page.locator('.topbar').getByRole('link', { name: '登录', exact: true }),
  ).toHaveCount(0)
})
