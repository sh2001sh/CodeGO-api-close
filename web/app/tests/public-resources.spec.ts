import { test, expect } from '@playwright/test'
import { fixtureAPI } from './fixtures'
import { selectLanguage } from './language-helper'

test.beforeEach(async ({ page }) => {
  await fixtureAPI(page)
  await page.route('**/api/user/self', (route) =>
    route.fulfill({ status: 401, json: { success: false, message: '请先登录' } }),
  )
})

test('public help and policies remain readable without a session or working catalog', async ({
  page,
}) => {
  await page.route('**/api/marketplace/**', (route) =>
    route.fulfill({ status: 503, json: { success: false, message: '市场暂时不可用' } }),
  )
  for (const [path, title] of [
    ['/help', '常见问题'],
    ['/support', '联系支持'],
    ['/about', '关于 CodeGo'],
    ['/privacy', '隐私政策'],
    ['/terms', '服务条款'],
    ['/refund-policy', '退款规则'],
  ]) {
    await page.goto(path)
    await expect(page.getByRole('heading', { level: 1, name: title, exact: true })).toBeVisible()
    await expect(page.getByRole('alert')).toHaveCount(0)
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
    ).toBe(true)
  }
  await expect(page.getByText(/扣除毛额的 2%/)).toBeVisible()
  await page.getByRole('link', { name: '钱包与退款报价', exact: true }).click()
  await expect(page).toHaveURL(/\/sign-in\?returnTo=%2Fwallet$/)
})

test('resources navigation is keyboard accessible on desktop and reachable in the mobile drawer', async ({
  page,
  isMobile,
}) => {
  await page.goto('/')
  if (isMobile) {
    await page.getByRole('button', { name: '打开站点导航', exact: true }).click()
    await page.getByRole('dialog').getByRole('link', { name: '常见问题', exact: true }).click()
  } else {
    const resources = page.getByRole('button', { name: '资源', exact: true })
    await resources.focus()
    await resources.press('Enter')
    await page.getByRole('menuitem', { name: '常见问题', exact: true }).click()
  }
  await expect(page).toHaveURL(/\/help$/)
  const question = page.locator('summary').filter({ hasText: '购买后如何下载香港商业发票？' })
  await question.focus()
  await question.press('Enter')
  await expect(page.getByText(/首次开具后抬头与地址固定/)).toBeVisible()
  await selectLanguage(page, 'English')
  await expect(
    page.getByRole('heading', { level: 1, name: 'Frequently asked questions' }),
  ).toBeVisible()
  await expect(page.getByText(/These details are fixed after first issue/)).toBeVisible()
  await page.locator('footer').getByRole('link', { name: 'Privacy policy', exact: true }).click()
  await expect(page.getByRole('heading', { level: 1, name: 'Privacy policy' })).toBeVisible()
  await expect(page.getByText(/Separate diagnostic content sampling is supported/)).toBeVisible()
  await page
    .locator('.site-footer')
    .getByRole('link', { name: 'Contact support', exact: true })
    .click()
  const community = page.getByRole('link', { name: 'Visit the CodeGo community', exact: true })
  await expect(community).toHaveAttribute('href', 'https://community.codegoai.com')
  await expect(community).toHaveAttribute('rel', 'noopener noreferrer')
})

for (const width of [375, 390]) {
  test(`narrow English ${width}px header fits after fonts load and account actions remain in the drawer`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 812 })
    await page.goto('/')
    await selectLanguage(page, 'English')
    await page.evaluate(() => document.fonts.ready)
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
    ).toBe(true)
    await page.getByRole('button', { name: 'Open site navigation', exact: true }).click()
    await expect(
      page.getByRole('dialog').getByRole('link', { name: 'Sign in', exact: true }),
    ).toBeVisible()
    await page.getByRole('button', { name: 'Close site navigation', exact: true }).click()
    await page.unroute('**/api/user/self')
    let resumeSession!: () => void
    const sessionGate = new Promise<void>((resolve) => {
      resumeSession = resolve
    })
    await page.route('**/api/user/self', async (route) => {
      await sessionGate
      await route.fallback()
    })
    try {
      await page.reload()
      await page.evaluate(() => document.fonts.ready)
      await expect(
        page.getByRole('heading', { level: 1, name: 'Choose a model. Start building.' }),
      ).toBeVisible()
      await expect(page.locator('.account-placeholder')).toBeVisible()
      await expect(page.locator('.account-placeholder')).toHaveAttribute('aria-busy', 'true')
      expect(
        await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
      ).toBe(true)
    } finally {
      resumeSession()
    }
    await expect(
      page.locator('.topbar').getByRole('button', { name: 'Notifications', exact: true }),
    ).toBeVisible()
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
    ).toBe(true)
    await page.getByRole('button', { name: 'Open site navigation', exact: true }).click()
    await expect(
      page.getByRole('dialog').getByRole('link', { name: 'Open console', exact: true }),
    ).toBeVisible()
  })
}
