import { test, expect } from '@playwright/test'
import { fixtureAPI, user } from './fixtures'

test.beforeEach(async ({ page }) => fixtureAPI(page))

test('root explicitly opens Shanghai funding report with exact revenue, negative profit and source costs', async ({
  page,
}) => {
  await page.clock.setFixedTime(new Date('2026-09-30T16:00:00Z'))
  await page.route('**/api/user/self', (route) =>
    route.fulfill({ json: { success: true, data: { ...user, role: 'root' } } }),
  )
  const days: (string | null)[] = []
  await page.route('**/api/billing/funding-economics**', (route) => {
    days.push(new URL(route.request().url()).searchParams.get('day'))
    return route.fulfill({
      contentType: 'application/json',
      body: `{"success":true,"data":{"date":"2026-10-01",
        "recognized_revenue_micro":9007199254740993,"recognized_cost_micro":9007199255740994,
        "recognized_profit_micro":-1000001,"unattributed_cost_micro":3,"sources":[
        {"source":"topup","amount_micro":9007199254740993,"revenue_micro":9007199254740993,"cost_micro":9007199255740994,"profit_micro":-1000001},
        {"source":"legacy_unattributed","amount_micro":3,"revenue_micro":0,"cost_micro":3,"profit_micro":-3}]}}`,
    })
  })
  await page.goto('/dashboard')
  const report = page.getByRole('region', { name: '资金经营日报', exact: true })
  await expect(report.locator('summary')).toBeVisible()
  expect(days).toEqual([])
  await report.locator('summary').click()
  await expect(report.getByLabel('统计日期（上海）')).toHaveValue('2026-10-01')
  await expect(report.getByText('报告日期：2026-10-01', { exact: true })).toBeVisible()
  await expect(report.locator('dd')).toHaveText([
    '9,007,199,254.740993 credits',
    '9,007,199,255.740994 credits',
    '-1.000001 credits',
    '0.000003 credits',
  ])
  const paid = report.getByRole('row').filter({ hasText: 'topup' })
  await expect(paid.getByRole('cell')).toHaveText([
    'topup',
    '9,007,199,254.740993 credits',
    '9,007,199,254.740993 credits',
    '9,007,199,255.740994 credits',
    '-1.000001 credits',
  ])
  await expect(report.getByRole('row').filter({ hasText: 'legacy_unattributed' })).toContainText(
    '-0.000003 credits',
  )
  expect(days).toEqual(['2026-10-01'])
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  )
})

test('root can retry a failed report and explicitly select an empty historical day', async ({
  page,
}) => {
  await page.route('**/api/user/self', (route) =>
    route.fulfill({ json: { success: true, data: { ...user, role: 'root' } } }),
  )
  const days: (string | null)[] = []
  await page.route('**/api/billing/funding-economics**', (route) => {
    const day = new URL(route.request().url()).searchParams.get('day')
    days.push(day)
    if (days.length === 1)
      return route.fulfill({ status: 503, json: { success: false, message: '日报服务暂时不可用' } })
    return route.fulfill({
      json: {
        success: true,
        data: {
          date: day,
          recognized_revenue_micro: 0,
          recognized_cost_micro: 0,
          recognized_profit_micro: 0,
          unattributed_cost_micro: 0,
          sources: [],
        },
      },
    })
  })
  await page.goto('/dashboard')
  const report = page.getByRole('region', { name: '资金经营日报', exact: true })
  await report.locator('summary').click()
  await expect(report.getByRole('alert')).toHaveText('日报服务暂时不可用')
  expect(days).toHaveLength(1)
  await report.getByRole('button', { name: '查询日报', exact: true }).click()
  await expect(report.getByText('该日期暂无已结算请求', { exact: true })).toBeVisible()
  expect(days).toHaveLength(2)
  expect(days[1]).toBe(days[0])
  await report.getByLabel('统计日期（上海）').fill('2025-02-01')
  expect(days).toHaveLength(2)
  await report.getByRole('button', { name: '查询日报', exact: true }).click()
  await expect(report.getByText('报告日期：2025-02-01', { exact: true })).toBeVisible()
  expect(days[2]).toBe('2025-02-01')
  await expect(report.getByRole('alert')).toHaveCount(0)
  await report.getByLabel('统计日期（上海）').fill('')
  await report.getByRole('button', { name: '查询日报', exact: true }).click()
  expect(days).toHaveLength(3)
})

for (const role of ['admin', 'user']) {
  test(`${role} never sees or fetches root funding report`, async ({ page }) => {
    await page.route('**/api/user/self', (route) =>
      route.fulfill({ json: { success: true, data: { ...user, role } } }),
    )
    const requests: string[] = []
    page.on('request', (request) => {
      if (request.url().includes('/api/billing/funding-economics')) requests.push(request.url())
    })
    await page.goto('/dashboard')
    await expect(page.getByRole('heading', { name: '仪表板', exact: true })).toBeVisible()
    await expect(page.getByText('123.456789 credits', { exact: true })).toBeVisible()
    await expect(page.getByRole('region', { name: '资金经营日报', exact: true })).toHaveCount(0)
    expect(requests).toEqual([])
  })
}
