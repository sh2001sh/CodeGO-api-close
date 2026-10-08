import { execFileSync } from 'node:child_process'
import { test, expect } from '@playwright/test'
import { prepareRealStack } from './real-stack-helper'

const url = process.env.V3_REAL_URL
const project = process.env.V3_REAL_PROJECT
test.skip(!url || !project, 'Requires an explicitly named isolated subscription test stack')
test.beforeEach(async ({ page }) => prepareRealStack(page))

test('real whole conversion and bound card preserve consent, expiry and independent rights', async ({
  page,
}) => {
  const suffix = crypto.randomUUID().replaceAll('-', '').slice(0, 12)
  const username = `v2_browser_${suffix}`
  const sql = (statement: string) =>
    execFileSync(
      'docker',
      [
        'exec',
        `${project}-postgres-1`,
        'psql',
        '-U',
        'codego',
        '-d',
        'codego',
        '-At',
        '-v',
        'ON_ERROR_STOP=1',
        '-c',
        statement,
      ],
      { encoding: 'utf8' },
    ).trim()
  await page.goto('/sign-up')
  await page.getByLabel('用户名', { exact: true }).fill(username)
  await page.getByLabel('邮箱', { exact: true }).fill(`${username}@example.test`)
  await page.getByLabel('密码', { exact: true }).fill(`Test-${crypto.randomUUID()}`)
  await page.getByRole('checkbox', { name: /我已阅读并同意/ }).check()
  const registration = page.waitForResponse('**/api/user/register')
  await page.getByRole('button', { name: '注册', exact: true }).click()
  const registered = await registration
  expect(registered.status()).toBe(200)
  await expect(page.getByRole('heading', { name: '仪表板', exact: true })).toBeVisible()
  // Authentication now starts a new document; use the committed session
  // cookie rather than a response body invalidated by browser navigation.
  const self = await page.request.get('/api/user/self', {
    headers: { 'X-CodeGo-API-Version': '3' },
  })
  expect(self.status()).toBe(200)
  const user: number = (await self.json()).data.id
  expect(Number.isSafeInteger(user)).toBe(true)
  sql(`UPDATE v3_identity.users SET role='root' WHERE id=${user}`)
  const call = async (path: string, data: unknown) => {
    const response = await page.request.post(path, {
      data,
      headers: {
        'X-CodeGo-API-Version': '3',
        Origin: url,
      },
    })
    expect(response.status()).toBe(200)
    return (await response.json()).data
  }
  const legacy = await call('/api/subscription/admin/plans', {
    name: `Browser legacy ${suffix}`,
    policy_version: 'legacy',
    currency: 'usd',
    price_minor: 100,
    credits: 300000000,
    duration_unit: 'day',
    duration_value: 30,
    reset_period: 'never',
    enabled: true,
  })
  const grant = async (key: string) =>
    call('/api/subscription/admin/bind', {
      plan_id: legacy.id,
      user_id: user,
      request_id: `${key}-${suffix}`,
    })
  const old = await grant('old')
  const expired = await grant('expired')
  const basis = await call('/api/subscription/self/wallet-conversion/quote', {
    subscription_id: old.id,
  })
  expect(basis.state).toBe('needs_review')
  const fixed = await call('/api/subscription/admin/plans', {
    name: `Browser fixed ${suffix}`,
    policy_version: 'standard_v2',
    currency: 'usd',
    price_minor: 100,
    credits: 20000000,
    duration_unit: 'day',
    duration_value: 90,
    reset_period: 'never',
    enabled: true,
  })
  const ruleResponse = await page.request.put('/api/subscription/admin/redesign-rules', {
    headers: {
      'X-CodeGo-API-Version': '3',
      Origin: url,
    },
    data: {
      conversion_rules: [
        {
          plan_id: legacy.id,
          basis_key: basis.basis_key,
          source_credits: 300000000,
          wallet_credits: 30000000,
          paid_wallet_credits: 0,
          enabled: true,
          reviewed: true,
          note: 'Isolated grant fixture, reward provenance only',
        },
      ],
      card_rules: [
        {
          name: `Browser card ${suffix}`,
          reference_plan_id: legacy.id,
          card_plan_id: fixed.id,
          credits: 20000000,
          cost_per_card: 7000000,
          baseline_cost: 1000000,
          budget_total: 14000000,
          incremental_budget_total: 12000000,
          enabled: true,
          reviewed: true,
          note: 'Isolated voluntary compensation fixture',
        },
      ],
    },
  })
  expect(ruleResponse.status()).toBe(200)
  sql(`UPDATE v3_commerce.subscriptions SET expires_at=now() WHERE id=${expired.id}`)
  sql(
    `INSERT INTO v3_commerce.subscription_reset_opportunity_accounts(user_id,earned_total,available_total) VALUES(${user},2,2)`,
  )
  await page.goto('/wallet')
  const select = page.getByRole('combobox', { name: '需要转余额的老套餐', exact: true })
  await expect(select.locator(`option[value="${expired.id}"]`)).toHaveAttribute('disabled')
  await select.selectOption(String(old.id))
  await page.getByRole('button', { name: '预览整份转余额', exact: true }).click()
  await expect(page.getByText(/300 credits 老额度 → 30 credits 钱包额度/)).toBeVisible()
  await expect(page.getByRole('button', { name: '确认整份转余额', exact: true })).toBeDisabled()
  await page.getByRole('checkbox', { name: /我确认将整份剩余权益转入本人余额/ }).check()
  await page.getByRole('button', { name: '确认整份转余额', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: '整份转换完成' })).toContainText(
    '钱包到账 30 credits',
    { timeout: 20000 },
  )
  expect(
    sql(`SELECT state,benefits_until=expires_at FROM v3_commerce.subscriptions WHERE id=${old.id}`),
  ).toBe('canceled|t')
  await page.goto('/referral-rewards')
  await page
    .getByRole('combobox', { name: '可兑换套餐卡', exact: true })
    .selectOption({ label: `Browser card ${suffix} · 每张 20 credits · 激活后 90 天` })
  await page.getByRole('button', { name: '预览次数换卡', exact: true }).click()
  await expect(page.getByRole('button', { name: '确认次数换卡', exact: true })).toBeDisabled()
  await page.getByRole('checkbox', { name: /我同意消耗上述次数/ }).check()
  await page.getByRole('button', { name: '确认次数换卡', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: '已换取 1 张套餐卡' })).toContainText(
    '剩余 1 次',
  )
  await page.getByRole('button', { name: '激活此卡', exact: true }).click()
  await page.getByRole('button', { name: '确认激活套餐卡', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: '套餐卡已激活' })).toBeVisible()
  await expect(page.getByRole('button', { name: '激活此卡', exact: true })).toBeDisabled()
  expect(
    sql(
      `SELECT earned_total,used_total,exchanged_total,available_total FROM v3_commerce.subscription_reset_opportunity_accounts WHERE user_id=${user}`,
    ),
  ).toBe('2|0|1|1')
  expect(
    sql(
      `SELECT count(*) FROM v3_commerce.subscriptions WHERE user_id=${user} AND policy_version='standard_v2' AND total_credits=20000000 AND reset_period='never'`,
    ),
  ).toBe('1')

  const periodicPlan = await call('/api/subscription/admin/plans', {
    name: `Browser periodic ${suffix}`,
    policy_version: 'legacy',
    currency: 'usd',
    price_minor: 100,
    credits: 20000000,
    period_credits: 10000000,
    duration_unit: 'day',
    duration_value: 3,
    reset_period: 'daily',
    enabled: true,
  })
  const periodic = await call('/api/subscription/admin/bind', {
    plan_id: periodicPlan.id,
    user_id: user,
    request_id: `review-periodic-${suffix}`,
  })
  const evidenceResponse = await page.request.get(
    `/api/subscription/admin/wallet-conversion-review/${periodic.id}`,
    { headers: { 'X-CodeGo-API-Version': '3' } },
  )
  expect(evidenceResponse.status()).toBe(200)
  const evidence = (await evidenceResponse.json()).data
  expect(evidence.current_credits).toBe(10000000)
  expect(evidence.future_credits).toBe(10000000)
  const total = evidence.current_credits + evidence.future_credits
  const toCredits = (micro: number) => String(micro / 1000000)

  await page.goto('/subscriptions')
  await page.getByLabel('订阅', { exact: true }).fill(String(periodic.id))
  await page.getByRole('button', { name: '核定分段转换', exact: true }).click()
  await expect(page.getByText('未发放的周期承诺', { exact: true })).toBeVisible()
  await page.getByLabel('来源说明').fill('Isolated periodic grant')
  await page.getByLabel('该来源整包老额度 credits').fill(toCredits(total))
  await page.getByLabel('该段当前未消耗老额度 credits').fill(toCredits(evidence.current_credits))
  await page.getByLabel('该段未发周期额度 credits').fill(toCredits(evidence.future_credits - 1))
  await page.getByLabel('该来源整包可兑余额 credits').fill(toCredits(total / 10))
  await page.getByLabel('其中原付费 credits', { exact: true }).fill('0')
  await page
    .getByLabel('核对依据与说明', { exact: true })
    .fill('Audited synthetic grant, reward only')
  await page.getByLabel('已核对权益与资金来源', { exact: true }).check()
  await page.getByLabel('启用此核定报价', { exact: true }).check()
  await page.getByRole('button', { name: '保存分段核定', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('分段合计必须覆盖全部当前额度及未来周期承诺')
  expect(
    sql(
      `SELECT count(*) FROM v3_commerce.subscription_wallet_reviews WHERE subscription_id=${periodic.id}`,
    ),
  ).toBe('0')
  await page.getByLabel('该段未发周期额度 credits').fill(toCredits(evidence.future_credits))
  await page.getByRole('button', { name: '保存分段核定', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: '分段核定已保存' })).toBeVisible()
  expect(sql(`SELECT state FROM v3_commerce.subscriptions WHERE id=${periodic.id}`)).toBe('active')

  await page.goto('/wallet')
  await page
    .getByRole('combobox', { name: '需要转余额的老套餐', exact: true })
    .selectOption(String(periodic.id))
  await page.getByRole('button', { name: '预览整份转余额', exact: true }).click()
  await expect(
    page.getByText('未来周期承诺已计入本次到账，转换后不会再次自动发放。', { exact: true }),
  ).toBeVisible()
  await expect(page.getByRole('button', { name: '确认整份转余额', exact: true })).toBeDisabled()
  await page.getByRole('checkbox', { name: /我确认将整份剩余权益转入本人余额/ }).check()
  await page.getByRole('button', { name: '确认整份转余额', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: '整份转换完成' })).toContainText(
    '钱包到账 2 credits',
    { timeout: 20000 },
  )
  expect(
    sql(
      `SELECT state,next_reset_at IS NULL,reset_period,benefits_until=expires_at FROM v3_commerce.subscriptions WHERE id=${periodic.id}`,
    ),
  ).toBe('canceled|t|never|t')
})
