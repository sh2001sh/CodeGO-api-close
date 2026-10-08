import { test, expect, type Page } from '@playwright/test'
import { fixtureAPI } from './fixtures'

// The group rail is a row of buttons on desktop and a <select> on mobile;
// only one is visible per viewport (the other stays in the DOM but hidden).
// isVisible() is a non-waiting snapshot, so wait briefly before branching to
// avoid racing the initial render.
async function goToGroup(page: Page, label: string) {
  const button = page.getByRole('button', { name: label, exact: true })
  const select = page.getByRole('combobox', { name: '系统设置分区', exact: true })
  await Promise.race([
    button.waitFor({ state: 'visible' }).catch(() => {}),
    select.waitFor({ state: 'visible' }).catch(() => {}),
  ])
  if (await button.isVisible()) {
    await button.click()
    return
  }
  await select.selectOption({ label })
}

const base = [
  { key: 'site_name_unused', value: 'legacy', sensitive: false, configured: true },
  { key: 'Maintenance', value: false, sensitive: false, configured: true },
  { key: 'EmailVerificationEnabled', value: false, sensitive: false, configured: true },
  { key: 'ServerAddress', value: 'https://old.example.com', sensitive: false, configured: true },
  { key: 'GitHubClientSecret', value: null, sensitive: true, configured: true },
]

test.beforeEach(async ({ page }) => {
  await fixtureAPI(page)
  await page.route('**/api/settings', (route) => {
    if (route.request().method() !== 'GET') return route.fallback()
    return route.fulfill({ json: { success: true, data: base } })
  })
})

test('loads grouped settings sections', async ({ page }) => {
  await page.goto('/settings')
  await expect(page.getByRole('heading', { name: '系统设置', exact: true, level: 1 })).toBeVisible()
  await expect(page.getByLabel('站点公开地址', { exact: true })).toHaveValue(
    'https://old.example.com',
  )
  await goToGroup(page, '登录与认证')
  await expect(page.getByLabel('注册邮箱验证码', { exact: true })).toBeVisible()
})

test('editing and saving a string field sends only the changed key', async ({ page }) => {
  await page.goto('/settings')
  const sent = page.waitForRequest(
    (request) =>
      new URL(request.url()).pathname === '/api/settings/ServerAddress' &&
      request.method() === 'PUT',
  )
  await page.getByLabel('站点公开地址', { exact: true }).fill('https://new.example.com')
  await page.getByRole('button', { name: '保存', exact: true }).click()
  const body = (await sent).postDataJSON()
  expect(body).toEqual({ value: 'https://new.example.com', sensitive: false })
})

test('editing and saving a boolean field sends the new value', async ({ page }) => {
  await page.goto('/settings')
  await goToGroup(page, '登录与认证')
  const sent = page.waitForRequest(
    (request) =>
      new URL(request.url()).pathname === '/api/settings/EmailVerificationEnabled' &&
      request.method() === 'PUT',
  )
  await page.getByLabel('注册邮箱验证码', { exact: true }).check()
  await page.getByRole('button', { name: '保存', exact: true }).click()
  const body = (await sent).postDataJSON()
  expect(body).toEqual({ value: true, sensitive: false })
})

test('a configured secret is never echoed and replacing sends a new value', async ({ page }) => {
  await page.goto('/settings')
  await goToGroup(page, '登录与认证')
  await expect(page.getByText('已配置', { exact: true })).toBeVisible()
  await expect(page.locator('input[type="password"]')).toHaveCount(0)
  await page.getByRole('button', { name: '替换', exact: true }).click()
  const secretInput = page.locator('input[name="GitHubClientSecret"]')
  await expect(secretInput).toHaveValue('')
  await secretInput.fill('new-secret-value')
  const sent = page.waitForRequest(
    (request) =>
      new URL(request.url()).pathname === '/api/settings/GitHubClientSecret' &&
      request.method() === 'PUT',
  )
  await page.getByRole('button', { name: '保存', exact: true }).click()
  const body = (await sent).postDataJSON()
  expect(body).toEqual({ value: 'new-secret-value', sensitive: true })
})

test('invalid JSON blocks save for a json field', async ({ page }) => {
  await page.goto('/settings')
  await goToGroup(page, '登录与认证')
  await page.getByLabel('邮箱域名白名单', { exact: true }).fill('{not valid json')
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByText('配置值必须为有效 JSON', { exact: true })).toBeVisible()
})

test('search filters settings by label across all groups', async ({ page }) => {
  await page.goto('/settings')
  await page.getByLabel('搜索', { exact: true }).fill('SMTP')
  await expect(page.getByLabel('SMTP 服务器地址', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '站点', exact: true })).toHaveCount(0)
})
