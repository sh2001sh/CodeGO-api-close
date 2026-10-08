import { test, expect } from '@playwright/test'
import { fixtureAPI } from './fixtures'

test.beforeEach(async ({ page }) => fixtureAPI(page))

test('console brand returns to the public home on every viewport', async ({ page }) => {
  await page.goto('/dashboard')
  const brand = page.locator('.topbar').getByRole('link', { name: 'CodeGo AI', exact: true })
  await expect(brand).toHaveAttribute('href', '/')
  await brand.click()
  await expect(page).toHaveURL(/\/$/)
  await expect(page.getByRole('heading', { name: '选择模型，开始构建。', level: 1 })).toBeVisible()
})

test('mobile console drawer brand returns home and retired links stay absent', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/dashboard')
  await page.getByRole('button', { name: '打开导航', exact: true }).click()
  const drawer = page.getByRole('dialog')
  await expect(drawer.getByRole('link', { name: '对话', exact: true })).toHaveAttribute(
    'href',
    '/playground',
  )
  await expect(drawer.getByRole('link', { name: '渠道工作台', exact: true })).toHaveAttribute(
    'href',
    '/my-channels',
  )
  for (const label of ['拼团', '幸运抽奖', '抽奖管理', '社区', '桌面设备', '发票']) {
    await expect(drawer.getByRole('link', { name: label, exact: true })).toHaveCount(0)
  }
  await drawer.getByRole('link', { name: 'CodeGo AI', exact: true }).click()
  await expect(page).toHaveURL(/\/$/)
})

test('community is an external topbar link and invoices move to billing', async ({ page }) => {
  await page.goto('/dashboard')
  const community = page.locator('.topbar').getByRole('link', { name: '社区', exact: true })
  await expect(community).toHaveAttribute('href', 'https://community.codegoai.com')
  await expect(community).toHaveAttribute('rel', 'noopener noreferrer')
  await page.goto('/invoices')
  await expect(page).toHaveURL(/\/billing#invoices$/)
  await expect(page.getByRole('heading', { name: '账单明细', exact: true })).toBeVisible()
})

test('retired group buy URL redirects without loading its purchase data', async ({ page }) => {
  const calls: string[] = []
  page.on('request', (request) => {
    if (new URL(request.url()).pathname.startsWith('/api/group-buy/')) calls.push(request.url())
  })
  await page.goto('/group-buy')
  await expect(page).toHaveURL(/\/dashboard$/)
  expect(calls).toEqual([])
})
