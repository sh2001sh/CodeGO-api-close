import { test, expect, type Page } from '@playwright/test'
import { fixtureAPI, plan } from './fixtures'

const active = {
  id: 1,
  plan_id: 1,
  state: 'active',
  balance: 600000000,
  total_credits: 1000000000,
  used_credits: 400000000,
  period_credits: 0,
  period_used: 0,
  policy_version: 'legacy',
  starts_at: '2026-09-01T00:00:00Z',
  expires_at: '2099-01-01T00:00:00Z',
}
const quote = {
  quote_id: 'whole-quote-1',
  subscription_id: 1,
  state: 'quoted',
  basis_key: 'reviewed-original-plan',
  rule_id: 1,
  rule_revision: 1,
  source_total: 1000000000,
  source_credits: 600000000,
  target_credits: 60000000,
  paid_credits: 50000000,
  reward_credits: 10000000,
  subscription_expires_at: active.expires_at,
  expires_at: '2099-01-01T00:00:00Z',
  terms_version: 'legacy-wallet-v1',
}

async function setup(page: Page) {
  await fixtureAPI(page)
  await page.route('**/api/subscription/self', (route) =>
    route.fulfill({ json: { success: true, data: [active] } }),
  )
  await page.route('**/api/subscription/plans', (route) =>
    route.fulfill({ json: { success: true, data: [plan] } }),
  )
}

test('whole conversion requires explicit consent, uses reviewed amounts and requotes after changed rights', async ({
  page,
}) => {
  await setup(page)
  let quotes = 0
  let confirmations = 0
  await page.route('**/api/subscription/self/wallet-conversion/quote', (route) => {
    quotes++
    expect(route.request().postDataJSON()).toEqual({ subscription_id: 1 })
    return route.fulfill({
      json: { success: true, data: { ...quote, quote_id: `whole-quote-${quotes}` } },
    })
  })
  await page.route('**/api/subscription/self/wallet-conversion/confirm', (route) => {
    confirmations++
    expect(route.request().postDataJSON().accepted_terms).toBe(true)
    return route.fulfill(
      confirmations === 1
        ? { status: 409, json: { success: false, message: '套餐余额发生变化，请重新报价' } }
        : {
            json: {
              success: true,
              data: {
                ...quote,
                state: 'completed',
                request_id: route.request().postDataJSON().request_id,
              },
            },
          },
    )
  })
  await page.goto('/wallet')
  await page.getByRole('combobox', { name: '需要转余额的老套餐', exact: true }).selectOption('1')
  await page.getByRole('button', { name: '预览整份转余额', exact: true }).click()
  const consent = page.getByRole('checkbox', { name: /我确认将整份剩余权益转入本人余额/ })
  await expect(consent).not.toBeChecked()
  await expect(page.getByRole('button', { name: '确认整份转余额', exact: true })).toBeDisabled()
  await expect(page.getByText(/本次比例：600 credits 老额度 → 60 credits 钱包额度/)).toBeVisible()
  await consent.check()
  await page.getByRole('button', { name: '确认整份转余额', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('请重新报价')
  await expect(page.getByRole('button', { name: '重试同一次整份转换', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: '重新获取转换报价', exact: true }).click()
  await expect(consent).not.toBeChecked()
  await consent.check()
  await page.getByRole('button', { name: '确认整份转余额', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: '整份转换完成' })).toContainText(
    '钱包到账 60 credits',
  )
  expect(quotes).toBe(2)
  expect(confirmations).toBe(2)
})

test('expired old subscription cannot request a conversion quote', async ({ page }) => {
  await setup(page)
  await page.route('**/api/subscription/self', (route) =>
    route.fulfill({
      json: { success: true, data: [{ ...active, expires_at: '2020-01-01T00:00:00Z' }] },
    }),
  )
  let quotes = 0
  await page.route('**/api/subscription/self/wallet-conversion/quote', (route) => {
    quotes++
    return route.fulfill({ json: { success: true, data: quote } })
  })
  await page.goto('/wallet')
  await expect(
    page
      .getByRole('combobox', { name: '需要转余额的老套餐', exact: true })
      .locator('option[value="1"]'),
  ).toHaveAttribute('disabled')
  await expect(page.getByRole('button', { name: '预览整份转余额', exact: true })).toBeDisabled()
  expect(quotes).toBe(0)
})

test('network retry keeps one request and accepted pending result polls until confirmed completion', async ({
  page,
}) => {
  await setup(page)
  await page.route('**/api/subscription/self/wallet-conversion/quote', (route) =>
    route.fulfill({ json: { success: true, data: quote } }),
  )
  const bodies: { request_id: string }[] = []
  await page.route('**/api/subscription/self/wallet-conversion/confirm', (route) => {
    bodies.push(route.request().postDataJSON())
    return route.fulfill(
      bodies.length === 1
        ? { status: 503, json: { success: false, message: '同步暂时不可用' } }
        : {
            status: 202,
            json: {
              success: true,
              data: { ...quote, state: 'pending', request_id: bodies[0].request_id },
            },
          },
    )
  })
  await page.route('**/api/subscription/self/wallet-conversion/*', (route) => {
    if (route.request().method() !== 'GET') return route.fallback()
    expect(route.request().method()).toBe('GET')
    return route.fulfill({
      json: {
        success: true,
        data: { ...quote, state: 'completed', request_id: bodies[0].request_id },
      },
    })
  })
  await page.goto('/wallet')
  await page.getByRole('combobox', { name: '需要转余额的老套餐', exact: true }).selectOption('1')
  await page.getByRole('button', { name: '预览整份转余额', exact: true }).click()
  await page.getByRole('checkbox', { name: /我确认将整份剩余权益转入本人余额/ }).check()
  await page.getByRole('button', { name: '确认整份转余额', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('同步暂时不可用')
  await page.getByRole('button', { name: '重试同一次整份转换', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: '整份转换完成' })).toBeVisible()
  expect(bodies[0].request_id).toBe(bodies[1].request_id)
})
