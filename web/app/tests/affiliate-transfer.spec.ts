import { test, expect } from '@playwright/test'
import { fixtureAPI, user } from './fixtures'

test.beforeEach(async ({ page }) => fixtureAPI(page))

test('shows positive monetary balances and refuses below-minimum or excessive amounts', async ({
  page,
}) => {
  await page.route('**/api/user/self', (route) =>
    route.fulfill({
      json: { success: true, data: { ...user, affiliate_micro_credits: '2500000' } },
    }),
  )
  let transfers = 0
  await page.route('**/api/user/aff_transfer', (route) => {
    transfers++
    return route.fulfill({ json: { success: true, data: {} } })
  })
  await page.goto('/wallet')
  await expect(page.getByText('当前可提现余额：2.5 credits')).toBeVisible()
  await page.getByLabel('转入金额 credits', { exact: true }).fill('0.999999')
  await page.getByRole('button', { name: '核对转入金额', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('最低转入 1 credit')
  await page.getByLabel('转入金额 credits', { exact: true }).fill('2.500001')
  await page.getByRole('button', { name: '核对转入金额', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('转入金额超过可提现余额')
  await page.getByLabel('转入金额 credits', { exact: true }).fill('1.0000001')
  await page.getByRole('button', { name: '核对转入金额', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('请输入最多六位小数的正数')
  expect(transfers).toBe(0)
})

test('hides zero balances and keeps sub-minimum balances read-only', async ({ page }) => {
  await page.goto('/wallet')
  await expect(page.getByRole('region', { name: '可提现余额转入钱包' })).toHaveCount(0)
  await page.route('**/api/user/self', (route) =>
    route.fulfill({ json: { success: true, data: { ...user, affiliate_micro_credits: 999999 } } }),
  )
  await page.reload()
  await expect(page.getByText('当前可提现余额：0.999999 credits')).toBeVisible()
  await expect(page.getByText('余额不足 1 credit，暂时无法转入。')).toBeVisible()
  await expect(page.getByRole('button', { name: '全部转入钱包', exact: true })).toHaveCount(0)
})

test('retries the same minimum transfer and reloads actual profile and wallet balances', async ({
  page,
}) => {
  let affiliate = '1000000'
  let wallet = '123456789'
  let profileReads = 0
  let walletReads = 0
  const requests: { operation_id: string; amount_micro_credits: number }[] = []
  await page.route('**/api/user/self', (route) => {
    profileReads++
    return route.fulfill({
      json: { success: true, data: { ...user, affiliate_micro_credits: affiliate } },
    })
  })
  await page.route('**/api/wallet', (route) => {
    walletReads++
    return route.fulfill({ json: { success: true, data: { balance_micro_credits: wallet } } })
  })
  await page.route('**/api/user/aff_transfer', async (route) => {
    expect(route.request().headers()['x-codego-api-version']).toBe('3')
    requests.push(route.request().postDataJSON())
    if (requests.length === 1) {
      await route.fulfill({ status: 503, json: { success: false, message: '请稍后重试' } })
      return
    }
    affiliate = '0'
    wallet = '124456789'
    await new Promise((resolve) => setTimeout(resolve, 150))
    await route.fulfill({
      json: {
        success: true,
        data: { ...requests[0], affiliate_micro_credits: 0, wallet_micro_credits: wallet },
      },
    })
  })
  await page.goto('/wallet')
  await page.getByLabel('转入金额 credits', { exact: true }).fill('1')
  await page.getByRole('button', { name: '核对转入金额', exact: true }).click()
  await page.getByRole('button', { name: '确认转入钱包', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('请稍后重试')
  await expect(page.getByRole('button', { name: '返回修改', exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: '重试同一次转入', exact: true }).click()
  await expect(page.getByRole('button', { name: '转入中…', exact: true })).toBeDisabled()
  await expect(page.getByRole('status').filter({ hasText: '已转入钱包' })).toHaveText(
    '已转入钱包 1 credits',
  )
  await expect(page.getByText('当前可提现余额：0 credits')).toBeVisible()
  await expect(page.getByText('124.456789 credits', { exact: true })).toBeVisible()
  expect(requests).toHaveLength(2)
  expect(requests[0]).toEqual(requests[1])
  expect(requests[0].amount_micro_credits).toBe(1000000)
  expect(requests[0].operation_id).toMatch(/^[\da-f-]{36}$/)
  expect(profileReads).toBeGreaterThan(1)
  expect(walletReads).toBeGreaterThan(1)
})

test('selects the full balance and sends integers beyond Number precision exactly', async ({
  page,
}) => {
  await page.route('**/api/user/self', (route) =>
    route.fulfill({
      json: { success: true, data: { ...user, affiliate_micro_credits: '9007199254740993' } },
    }),
  )
  await page.route('**/api/user/aff_transfer', (route) => {
    expect(route.request().postData()).toContain('"amount_micro_credits":9007199254740993')
    return route.fulfill({
      status: 400,
      json: { success: false, message: '余额已变化，请稍后重试' },
    })
  })
  await page.goto('/wallet')
  await page.getByRole('button', { name: '全部转入钱包', exact: true }).click()
  await expect(page.getByText('确认将 9,007,199,254.740993 credits 转入钱包？')).toBeVisible()
  await page.getByRole('button', { name: '确认转入钱包', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('余额已变化，请稍后重试')
})
