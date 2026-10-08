import { test, expect } from '@playwright/test'
import { fixtureAPI, user } from './fixtures'

test.beforeEach(async ({ page }) => fixtureAPI(page))

test('consumption records show frozen public terms, precise rewards and unknown-cost review', async ({
  page,
}) => {
  const summary = {
    available_count: 0,
    earned_total: 0,
    used_total: 0,
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
          successful_purchase_invites: 0,
          reset_opportunity: summary,
          invitees: [],
        },
      },
    }),
  )
  await page.route('**/api/user/aff/consumption-rewards', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          records: [
            {
              id: 1,
              order_id: 2,
              invitee_id: 3,
              state: 'needs_review',
              reason: 'unknown_cost',
              terms: {
                eligible: true,
                reward_ppm: 10000,
                profit_share_ppm: 200000,
                window_days: 30,
                delay_days: 7,
                max_reward_credits: 1000000,
              },
              paid_credits: '9007199254740993',
              max_reward_credits: '9007199254740993',
              refunded_reward_credits: 0,
              window_until: null,
            },
          ],
          refund_offset_credits: 0,
          owner_only: true,
          new_invites_grant_refresh: false,
        },
      },
    }),
  )
  await page.goto('/referral-rewards')
  await page.getByRole('button', { name: '查看消费奖励', exact: true }).click()
  await expect(page.getByText(/付款后 30 天内的实际付费消费按 1% 核算/)).toBeVisible()
  await expect(
    page.getByRole('cell', { name: '9,007,199,254.740993 credits', exact: true }).last(),
  ).toBeVisible()
  await expect(page.getByRole('cell', { name: '待核对', exact: true })).toBeVisible()
  await expect(page.getByText(/奖励上限为最高可得金额，不代表已赚取或保证发放/)).toBeVisible()
})

test('root config explicitly confirms effective program and freezes exact budget and cost percent', async ({
  page,
}) => {
  const policy = {
    enabled: false,
    revision: 1,
    effective_at: null,
    ancillary_cost_ppm: null,
    reward_ppm: 10000,
    profit_share_ppm: 200000,
    window_days: 30,
    delay_days: 7,
    max_reward_credits: 0,
    total_budget_credits: 0,
    reserved_credits: 0,
    spent_credits: 0,
  }
  await page.route('**/api/user/self', (route) =>
    route.fulfill({ json: { success: true, data: { ...user, role: 'root' } } }),
  )
  await page.route('**/api/subscription/admin/redesign-rules', (route) =>
    route.fulfill({ json: { success: true, data: { conversion_rules: [], card_rules: [] } } }),
  )
  let submitted: Record<string, unknown> | undefined
  await page.route('**/api/subscription/admin/referral-policy', (route) => {
    if (route.request().method() === 'GET')
      return route.fulfill({ json: { success: true, data: policy } })
    submitted = route.request().postDataJSON()
    return route.fulfill({
      json: {
        success: true,
        data: { ...submitted, revision: 2, effective_at: '2026-10-02T00:00:00Z' },
      },
    })
  })
  await page.goto('/subscriptions')
  await page.getByLabel('每个首购奖励上限 credits', { exact: true }).fill('1')
  await page.getByLabel('奖励全额履约总预算 credits', { exact: true }).fill('100')
  await page.getByLabel('可归属手续费与活动费用 %（未知留空）', { exact: true }).fill('0.5')
  await page.getByRole('checkbox', { name: '启用新邀请消费奖励', exact: true }).check()
  await page.getByRole('button', { name: '核对邀请活动配置', exact: true }).click()
  expect(submitted).toBeUndefined()
  await expect(page.getByText(/本次首次启用后，新订单不再赠邀请刷新/)).toBeVisible()
  await page.getByRole('button', { name: '确认保存邀请配置', exact: true }).click()
  await expect
    .poll(() => submitted)
    .toMatchObject({
      enabled: true,
      revision: 1,
      reward_ppm: 10000,
      profit_share_ppm: 200000,
      ancillary_cost_ppm: 5000,
      max_reward_credits: 1000000,
      total_budget_credits: 100000000,
      reserved_credits: 0,
      spent_credits: 0,
    })
})
