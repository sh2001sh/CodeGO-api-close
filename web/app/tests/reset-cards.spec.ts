import { test, expect } from '@playwright/test'
import { fixtureAPI, plan } from './fixtures'

const fixed = {
  ...plan,
  policy_version: 'standard_v2',
  plan_type: 'fixed',
  credits: 103000000,
  duration_unit: 'day',
  duration_value: 90,
  reset_period: 'never',
  group_buy_enabled: false,
}
const cardQuote = {
  quote_id: 'card-quote-1',
  rule_id: 2,
  rule_revision: 1,
  quantity: 2,
  available_count: 3,
  plan: fixed,
  expires_at: '2099-01-01T00:00:00Z',
  terms_version: 'reset-card-v1',
}

test.beforeEach(async ({ page }) => {
  await fixtureAPI(page)
  const summary = {
    available_count: 3,
    earned_total: 3,
    used_total: 0,
    exchanged_total: 0,
    current_month: '2026-10',
    last_used_month: '',
    used_this_month: false,
  }
  await page.route('**/api/subscription/self/reset-opportunity', (route) =>
    route.fulfill({ json: { success: true, data: summary } }),
  )
  await page.route('**/api/user/aff/rewards', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          affiliate_code: 'invite',
          invited_count: 1,
          successful_purchase_invites: 1,
          reset_opportunity: summary,
          invitees: [],
        },
      },
    }),
  )
  await page.route('**/api/subscription/self/reset-cards/rules', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: [
          {
            id: 2,
            name: '已核定历史换卡',
            reference_plan_id: 1,
            credits: fixed.credits,
            duration_days: 90,
          },
        ],
      },
    }),
  )
  await page.route('**/api/subscription/self/reset-cards', (route) =>
    route.fulfill({ json: { success: true, data: [] } }),
  )
})

test('voluntary exchange requires consent and resets quote after changed opportunity or budget', async ({
  page,
}) => {
  let quotes = 0
  let confirmations = 0
  await page.route('**/api/subscription/self/reset-cards/quote', (route) => {
    quotes++
    expect(BigInt(route.request().postDataJSON().rule_id)).toBe(2n)
    expect(route.request().postDataJSON().quantity).toBe(2)
    return route.fulfill({
      json: { success: true, data: { ...cardQuote, quote_id: `card-quote-${quotes}` } },
    })
  })
  await page.route('**/api/subscription/self/reset-cards/confirm', (route) => {
    confirmations++
    expect(route.request().postDataJSON().accepted_terms).toBe(true)
    return route.fulfill(
      confirmations === 1
        ? { status: 409, json: { success: false, message: '换卡预算变化，请重新报价' } }
        : {
            json: {
              success: true,
              data: {
                request_id: route.request().postDataJSON().request_id,
                quote_id: 'card-quote-2',
                quantity: 2,
                remaining_count: 1,
                cards: [],
              },
            },
          },
    )
  })
  await page.goto('/referral-rewards')
  await page.getByRole('spinbutton', { name: '本次消耗刷新次数', exact: true }).fill('2')
  await page.getByRole('button', { name: '预览次数换卡', exact: true }).click()
  const consent = page.getByRole('checkbox', { name: /我同意消耗上述次数/ })
  await expect(consent).not.toBeChecked()
  await expect(page.getByRole('button', { name: '确认次数换卡', exact: true })).toBeDisabled()
  await expect(page.getByText(/当前 3 次，确认后剩余 1 次/)).toBeVisible()
  await consent.check()
  await page.getByRole('button', { name: '确认次数换卡', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('换卡预算变化')
  await expect(page.getByRole('button', { name: '重试同一次换卡', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: '重新预览换卡', exact: true }).click()
  await page.getByRole('spinbutton', { name: '本次消耗刷新次数', exact: true }).fill('2')
  await page.getByRole('button', { name: '预览次数换卡', exact: true }).click()
  await expect(consent).not.toBeChecked()
  await consent.check()
  await page.getByRole('button', { name: '确认次数换卡', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: '已换取 2 张套餐卡' })).toContainText(
    '剩余 1 次',
  )
  expect(quotes).toBe(2)
  expect(confirmations).toBe(2)
})

test('card activation retries one idempotent key and never restarts another package', async ({
  page,
}) => {
  let activated = false
  const card = { id: '9007199254740993', state: 'ready', plan: fixed }
  await page.route('**/api/subscription/self/reset-cards', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: [
          {
            ...card,
            state: activated ? 'activated' : 'ready',
            subscription_id: activated ? 3 : undefined,
          },
        ],
      },
    }),
  )
  const requests: { request_id: string }[] = []
  await page.route('**/api/subscription/self/reset-cards/*/activate', (route) => {
    expect(route.request().url()).toContain('9007199254740993')
    requests.push(route.request().postDataJSON())
    if (requests.length === 1)
      return route.fulfill({ status: 503, json: { success: false, message: '激活同步暂不可用' } })
    activated = true
    return route.fulfill({
      json: { success: true, data: { ...card, state: 'activated', subscription_id: 3 } },
    })
  })
  await page.goto('/referral-rewards')
  await page.getByRole('button', { name: '激活此卡', exact: true }).click()
  await expect(page.getByText(/从现在起 90 天到期.*不延长其它套餐/)).toBeVisible()
  await page.getByRole('button', { name: '确认激活套餐卡', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('激活同步暂不可用')
  await page.getByRole('button', { name: '重试激活同一张卡', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: '套餐卡已激活' })).toContainText(
    '订阅编号 3',
  )
  await expect(page.getByRole('button', { name: '激活此卡', exact: true })).toBeDisabled()
  expect(requests[0].request_id).toBe(requests[1].request_id)
})
