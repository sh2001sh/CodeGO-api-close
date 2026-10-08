import { test, expect } from '@playwright/test'
import { fixtureAPI, user } from './fixtures'
import en from '../src/locales/en'
import hk from '../src/locales/zh-HK.json' with { type: 'json' }
import ar from '../src/locales/ar.json' with { type: 'json' }

for (const [locale, messages] of Object.entries({ en, 'zh-HK': hk, ar })) {
  test(`${locale} translates the backend reason for a stale segment review`, async ({ page }) => {
    await fixtureAPI(page, locale)
    await page.route('**/api/subscription/self', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: [
            {
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
            },
          ],
        },
      }),
    )
    const t = (key: string) => messages[key as keyof typeof messages] ?? key
    const reason = '消费、刷新或来源已变化，需重新核定分段权益'
    await page.route('**/api/subscription/self/wallet-conversion/quote', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: { state: 'needs_review', subscription_id: 1, quote_id: '', review_reason: reason },
        },
      }),
    )
    await page.goto('/wallet')
    await page
      .getByRole('combobox', { name: t('需要转余额的老套餐'), exact: true })
      .selectOption('1')
    await page.getByRole('button', { name: t('预览整份转余额'), exact: true }).click()
    await expect(page.getByRole('status').filter({ hasText: t(reason) })).toBeVisible()
    await expect(page.getByText(reason, { exact: true })).toHaveCount(0)
    await expect(page.getByRole('button', { name: t('确认整份转余额'), exact: true })).toHaveCount(
      0,
    )
  })
}

test('root review covers current and future credits without converting on behalf of the user', async ({
  page,
}) => {
  await fixtureAPI(page)
  await page.route('**/api/user/self', (route) =>
    route.fulfill({ json: { success: true, data: { ...user, role: 'root' } } }),
  )
  await page.route('**/api/subscription/admin/redesign-rules', (route) =>
    route.fulfill({ json: { success: true, data: { conversion_rules: [], card_rules: [] } } }),
  )
  await page.route('**/api/subscription/admin/referral-policy', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
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
        },
      },
    }),
  )
  const evidence = {
    subscription_id: 17,
    user_id: 1,
    fact_hash: 'f'.repeat(64),
    current_credits: 600000000,
    future_credits: 400000000,
    expires_at: '2099-01-01T00:00:00Z',
    reset_used: true,
    sources: [
      {
        order_id: 25,
        state: 'paid',
        purchase_type: 'purchase',
        credits: 1000000000,
        revenue_credits: 100000000,
        previously_paid: 0,
        pending_refund: false,
      },
    ],
  }
  let saves = 0
  let converts = 0
  await page.route('**/api/subscription/admin/wallet-conversion-review/17', (route) => {
    if (route.request().method() === 'GET')
      return route.fulfill({ json: { success: true, data: evidence } })
    saves++
    const body = route.request().postDataJSON()
    expect(body.fact_hash).toBe(evidence.fact_hash)
    expect(body.segments).toEqual([
      {
        name: 'Original order',
        original_order_id: 25,
        source_total: 1000000000,
        current_credits: 600000000,
        future_credits: 400000000,
        wallet_credits: 100000000,
        paid_wallet_credits: 80000000,
        target_credits: 0,
        paid_credits: 0,
        revenue_multiplier_ppm: 0,
      },
    ])
    return route.fulfill({ json: { success: true, data: { ...body, id: 4, revision: 1 } } })
  })
  await page.route('**/api/subscription/self/wallet-conversion/confirm', (route) => {
    converts++
    return route.abort()
  })
  await page.goto('/subscriptions')
  await page.getByLabel('订阅', { exact: true }).fill('17')
  await page.getByRole('button', { name: '核定分段转换', exact: true }).click()
  await expect(page.getByText('未发放的周期承诺', { exact: true })).toBeVisible()
  await page.getByLabel('来源说明').fill('Original order')
  await page.getByLabel('原付费订单（赠送可不选）').selectOption('25')
  await page.getByLabel('该来源整包老额度 credits').fill('1000')
  await page.getByLabel('该段当前未消耗老额度 credits').fill('600')
  await page.getByLabel('该段未发周期额度 credits').fill('350')
  await page.getByLabel('该来源整包可兑余额 credits').fill('100')
  await page.getByLabel('其中原付费 credits', { exact: true }).fill('80')
  await page
    .getByLabel('核对依据与说明', { exact: true })
    .fill('Frozen order and promised grants reconciled')
  await page.getByLabel('已核对权益与资金来源', { exact: true }).check()
  await page.getByLabel('启用此核定报价', { exact: true }).check()
  await page.getByRole('button', { name: '保存分段核定' }).click()
  await expect(page.getByRole('alert')).toContainText('分段合计必须覆盖全部当前额度及未来周期承诺')
  expect(saves).toBe(0)
  await page.getByLabel('该段未发周期额度 credits').fill('400')
  await page.getByRole('button', { name: '保存分段核定' }).click()
  await expect(page.getByRole('status').filter({ hasText: '分段核定已保存' })).toBeVisible()
  expect(saves).toBe(1)
  expect(converts).toBe(0)
})

test('reviewed quote shows future promises before requiring user consent', async ({ page }) => {
  await fixtureAPI(page)
  await page.route('**/api/subscription/self', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: [
          {
            id: 17,
            plan_id: 1,
            state: 'active',
            policy_version: 'legacy',
            balance: 600000000,
            starts_at: '2020-01-01T00:00:00Z',
            expires_at: '2099-01-01T00:00:00Z',
          },
        ],
      },
    }),
  )
  await page.route('**/api/subscription/self/wallet-conversion/quote', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          quote_id: 'reviewed-quote',
          subscription_id: 17,
          state: 'quoted',
          review_id: 4,
          rule_revision: 1,
          source_total: 1000000000,
          source_credits: 600000000,
          future_credits: 400000000,
          target_credits: 100000000,
          paid_credits: 80000000,
          reward_credits: 20000000,
          basis_key: 'f'.repeat(64),
          subscription_expires_at: '2099-01-01T00:00:00Z',
          expires_at: '2099-01-01T00:00:00Z',
          terms_version: 'legacy-wallet-v1',
          segments: [
            {
              name: 'Original order',
              current_credits: 600000000,
              future_credits: 400000000,
              target_credits: 100000000,
              paid_credits: 80000000,
            },
          ],
        },
      },
    }),
  )
  await page.goto('/wallet')
  await page.getByRole('combobox', { name: '需要转余额的老套餐', exact: true }).selectOption('17')
  await page.getByRole('button', { name: '预览整份转余额', exact: true }).click()
  await expect(page.getByText('未来周期承诺已计入本次到账，转换后不会再次自动发放。')).toBeVisible()
  await expect(page.getByText(/本次比例：1,000 credits 老额度 → 100 credits/)).toBeVisible()
  await expect(page.getByRole('button', { name: '确认整份转余额', exact: true })).toBeDisabled()
})
