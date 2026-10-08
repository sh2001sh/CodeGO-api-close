import { test, expect } from '@playwright/test'
import { fixtureAPI, user } from './fixtures'

test.beforeEach(async ({ page }) => fixtureAPI(page))

test('retired desktop routes lead to profile without authorization API calls', async ({ page }) => {
  const calls: string[] = []
  page.on('request', (request) => {
    if (new URL(request.url()).pathname.startsWith('/api/desktop/')) calls.push(request.url())
  })
  await page.goto('/desktop/authorize?session_id=session-1&code=CODE-123')
  await expect(page).toHaveURL(/\/profile$/)
  await page.goto('/desktop/devices')
  await expect(page).toHaveURL(/\/profile$/)
  expect(calls).toEqual([])
})

test('ordinary users cannot access root tools even through direct URLs', async ({ page }) => {
  await page.route('**/api/user/self', (route) =>
    route.fulfill({ json: { success: true, data: { ...user, role: 'user' } } }),
  )
  const calls: string[] = []
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname
    if (path.startsWith('/api/performance') || path.startsWith('/api/ratio_sync')) calls.push(path)
  })
  await page.goto('/ratio-sync')
  await expect(page.getByRole('alert')).toHaveText('无权执行此操作')
  await page.goto('/performance')
  await expect(page.getByRole('alert')).toHaveText('无权执行此操作')
  expect(calls).toEqual([])
})

test('upstream errors are shown and cannot result in a price write', async ({ page }) => {
  await page.route('**/api/user/self', (route) =>
    route.fulfill({ json: { success: true, data: { ...user, role: 'root' } } }),
  )
  await page.route('**/api/ratio_sync/channels', (route) =>
    route.fulfill({ json: { success: true, data: [{ id: 1, name: 'Broken upstream' }] } }),
  )
  await page.route('**/api/ratio_sync/fetch', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          differences: {},
          test_results: [{ name: 'Broken upstream', status: 'error', error: '上游连接失败' }],
        },
      },
    }),
  )
  const writes: string[] = []
  page.on('request', (request) => {
    if (new URL(request.url()).pathname === '/api/ratio_sync/apply') writes.push(request.url())
  })
  await page.goto('/ratio-sync')
  await page.getByLabel('Broken upstream', { exact: true }).check()
  await page.getByRole('button', { name: '获取价格差异', exact: true }).click()
  await expect(page.getByRole('cell', { name: '上游连接失败', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: /确认应用/ })).toHaveCount(0)
  expect(writes).toEqual([])
})
