import { test, expect } from '@playwright/test'
import { fixtureAPI } from './fixtures'
import { selectLanguage } from './language-helper'

const languages = [
  ['繁體中文（香港）', 'zh-HK'],
  ['简体中文', 'zh-CN'],
  ['English', 'en'],
  ['日本語', 'ja'],
  ['Русский', 'ru'],
  ['한국어', 'ko'],
  ['Français', 'fr'],
  ['Deutsch', 'de'],
  ['العربية', 'ar'],
]

test('all nine languages update the home, navigation and saved choice', async ({ page }) => {
  await fixtureAPI(page)
  await page.goto('/')
  for (const [name, code] of languages) {
    await selectLanguage(page, name)
    await expect(page.locator('html')).toHaveAttribute('lang', code)
    await expect(page.locator('html')).toHaveAttribute('dir', code === 'ar' ? 'rtl' : 'ltr')
    expect(await page.evaluate(() => localStorage.getItem('codego.locale'))).toBe(code)
    const title = await page.getByRole('heading', { level: 1 }).textContent()
    expect(title?.trim().length).toBeGreaterThan(0)
    if (!code.startsWith('zh') && code !== 'ja') {
      expect(title).not.toMatch(/[\u3400-\u9fff]/)
      expect(await page.locator('.site-footer-links').textContent()).not.toMatch(/[\u3400-\u9fff]/)
    }
  }
  await page.reload()
  await expect(page.locator('html')).toHaveAttribute('lang', 'ar')
  await expect(page.locator('.integration-code pre')).toHaveCSS('direction', 'ltr')
  await expect(page.locator('.brand').first()).toHaveCSS('direction', 'ltr')
  await selectLanguage(page, 'English')
  await expect(page.locator('html')).toHaveAttribute('dir', 'ltr')
})

test('German and Arabic fit a narrow screen and remain available on sign-in', async ({ page }) => {
  await fixtureAPI(page)
  await page.setViewportSize({ width: 375, height: 812 })
  await page.goto('/')
  for (const [name, code] of [
    ['Deutsch', 'de'],
    ['العربية', 'ar'],
  ]) {
    await selectLanguage(page, name)
    await expect(page.locator('html')).toHaveAttribute('lang', code)
    await page.evaluate(() => document.fonts.ready)
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
    ).toBe(true)
  }
  await page.goto('/sign-in')
  await expect(page.locator('html')).toHaveAttribute('lang', 'ar')
  await selectLanguage(page, '繁體中文（香港）')
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-HK')
  await expect(page.getByRole('heading', { level: 1 })).not.toBeEmpty()
  await expect(page.locator('.auth-footer').getByText('碼高智能有限公司')).toBeVisible()
})

test('first visit follows the browser and unknown saved languages recover', async ({ browser }) => {
  const context = await browser.newContext({ locale: 'ja-JP' })
  const page = await context.newPage()
  await fixtureAPI(page, null)
  await page.addInitScript(() => localStorage.setItem('codego.locale', 'invalid-locale'))
  await page.goto('/')
  await expect(page.locator('html')).toHaveAttribute('lang', 'ja')
  await expect(page.getByRole('heading', { level: 1 })).not.toHaveText('选择模型，开始构建。')
  await context.close()
})

test('Hong Kong and Arabic also translate signed-in account security and wallet navigation', async ({
  page,
}) => {
  await fixtureAPI(page)
  await page.route('**/api/user/2fa/status', (route) =>
    route.fulfill({
      json: { success: true, data: { enabled: true, backup_codes_remaining: 8, locked: false } },
    }),
  )
  await page.goto('/profile')
  await selectLanguage(page, '繁體中文（香港）')
  await expect(page.getByRole('heading', { name: '兩步驗證', exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: '電郵驗證與綁定', exact: true })).toBeVisible()
  await selectLanguage(page, 'العربية')
  await expect(page.getByRole('heading', { name: 'التحقق بخطوتين', exact: true })).toBeVisible()
  await expect(
    page.getByRole('button', { name: 'تعطيل التحقق بخطوتين', exact: true }),
  ).toBeVisible()
  await page.goto('/wallet')
  await expect(page.locator('html')).toHaveAttribute('lang', 'ar')
  await expect(page.getByRole('heading', { level: 1, name: 'المحفظة', exact: true })).toBeVisible()
})

test('a failed language download keeps the current interface and reports an error', async ({
  page,
}) => {
  await fixtureAPI(page)
  await page.route('**/*ar_json*.js', (route) => route.abort())
  await page.goto('/')
  await selectLanguage(page, 'العربية')
  await expect(page.getByRole('alert').filter({ hasText: '语言加载失败，请重试。' })).toBeVisible()
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN')
  await expect(page.locator('html')).toHaveAttribute('dir', 'ltr')
  await page.unroute('**/*ar_json*.js')
  await selectLanguage(page, 'العربية')
  await expect(page.locator('html')).toHaveAttribute('lang', 'ar')
  await expect(page.locator('html')).toHaveAttribute('dir', 'rtl')
})
