import { test, expect } from '@playwright/test'
import { fixtureAPI } from './fixtures'

test.beforeEach(async ({ page }) => fixtureAPI(page))

test('subscription reset shows server restrictions and never claims success on a repeated monthly use', async ({
  page,
}) => {
  const summary = {
    available_count: 1,
    earned_total: 2,
    used_total: 1,
    current_month: '2026-10',
    last_used_month: '2026-09',
    used_this_month: false,
  }
  await page.route('**/api/user/aff/rewards', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          affiliate_code: 'real-invite',
          invited_count: 2,
          successful_purchase_invites: 2,
          reset_opportunity: summary,
          invitees: [],
        },
      },
    }),
  )
  await page.route('**/api/subscription/self/reset-opportunity', (route) =>
    route.fulfill({ json: { success: true, data: summary } }),
  )
  let calls = 0
  await page.route('**/api/subscription/self/reset-opportunity/use', (route) => {
    calls++
    expect(route.request().postDataJSON()).toEqual({})
    return route.fulfill({ status: 409, json: { success: false, message: '本月已经使用重置机会' } })
  })
  await page.goto('/referral-rewards')
  await expect(page.getByRole('textbox', { name: '邀请链接', exact: true })).toHaveValue(
    new URL('/sign-up?ref=real-invite', page.url()).href,
  )
  await page.getByRole('button', { name: '使用一次重置机会', exact: true }).click()
  await page.getByRole('button', { name: '确认使用重置机会', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('本月已经使用重置机会')
  await expect(page.getByText(/已重置，恢复/)).toHaveCount(0)
  expect(calls).toBe(1)
  await page.getByRole('button', { name: '确认使用重置机会', exact: true }).click()
  await expect.poll(() => calls).toBe(2)
  await expect(page.getByText(/已重置，恢复/)).toHaveCount(0)
})

test('removed daily lucky routes show not found without loading draw data', async ({ page }) => {
  const calls: string[] = []
  page.on('request', (request) => {
    if (new URL(request.url()).pathname.startsWith('/api/daily-lucky-number/'))
      calls.push(request.url())
  })
  for (const path of ['/lucky-draw', '/lucky-admin']) {
    await page.goto(path)
    await expect(page.getByRole('heading', { name: '404', exact: true })).toBeVisible()
  }
  expect(calls).toEqual([])
})
