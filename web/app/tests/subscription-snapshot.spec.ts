import { test, expect } from '@playwright/test'
import { fixtureAPI, plan } from './fixtures'

test.beforeEach(async ({ page }) => fixtureAPI(page))

const original = {
  ...plan,
  name: '购买时的燃料月卡',
  policy_version: 'legacy',
  fuel_enabled: true,
  fuel_min_credits: 1000000,
  fuel_credit_step: 1000000,
}

test('old fuel rights and original amount step survive changes to the live plan', async ({
  page,
}) => {
  await page.route('**/api/subscription/plans', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: [
          {
            ...original,
            name: '修改后的套餐',
            plan_type: 'fixed',
            duration_unit: 'day',
            fuel_enabled: false,
            fuel_min_credits: 100000000,
            fuel_credit_step: 100000000,
          },
        ],
      },
    }),
  )
  await page.route('**/api/subscription/self', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: [
          {
            id: 1,
            plan_id: 1,
            policy_version: 'legacy',
            state: 'active',
            balance: 90000000,
            expires_at: '2026-10-30T08:00:00Z',
            plan_snapshot: original,
          },
        ],
      },
    }),
  )
  await page.route('**/api/subscription/fuel/quote', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          subscription_id: 1,
          credits: 2000000,
          amount_minor: 17,
          currency: 'usd',
          expires_at: '2026-10-30T08:00:00Z',
          min_credits: 1000000,
          credit_step: 1000000,
        },
      },
    }),
  )
  await page.goto('/wallet')
  await expect(page.getByRole('combobox', { name: '月卡订阅', exact: true })).toContainText(
    '购买时的燃料月卡',
  )
  await page.getByLabel('购买燃料 credits', { exact: true }).fill('1.5')
  await page.getByRole('button', { name: '获取燃料报价', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('步长')
  await page.getByLabel('购买燃料 credits', { exact: true }).fill('2')
  const sent = page.waitForRequest('**/api/subscription/fuel/quote')
  await page.getByRole('button', { name: '获取燃料报价', exact: true }).click()
  expect((await sent).postDataJSON()).toMatchObject({ subscription_id: 1, credits: 2000000 })
  await expect(page.getByText(/订阅 1 增加 2 credits，应付 USD 0.17/)).toBeVisible()
})

test('a later live-plan fuel option does not grant rights absent from the frozen purchase', async ({
  page,
}) => {
  await page.route('**/api/subscription/plans', (route) =>
    route.fulfill({ json: { success: true, data: [original] } }),
  )
  await page.route('**/api/subscription/self', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: [
          {
            id: 1,
            plan_id: 1,
            policy_version: 'legacy',
            state: 'active',
            balance: 90000000,
            expires_at: '2026-10-30T08:00:00Z',
            plan_snapshot: { ...original, fuel_enabled: false },
          },
        ],
      },
    }),
  )
  await page.goto('/wallet')
  await expect(page.getByText('暂无支持购买燃料的有效月卡。', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '获取燃料报价', exact: true })).toHaveCount(0)
})
