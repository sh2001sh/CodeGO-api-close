import { test, expect } from '@playwright/test'
import { fixtureAPI } from './fixtures'

test.beforeEach(async ({ page }) => fixtureAPI(page))

test('wallet sections stay reachable with no eligible subscriptions and no payment provider', async ({
  page,
}) => {
  await page.route('**/api/commerce/providers', (route) =>
    route.fulfill({ json: { success: true, data: [] } }),
  )
  await page.route('**/api/subscription/self', (route) =>
    route.fulfill({ json: { success: true, data: [] } }),
  )
  await page.goto('/wallet')
  const sections = page.getByRole('tablist', { name: '钱包分区' })
  await expect(sections.getByRole('tab')).toHaveCount(6)
  await expect(page.getByRole('button', { name: '前往支付', exact: true })).toBeDisabled()
  await sections.getByRole('tab', { name: '我的订阅', exact: true }).click()
  await expect(page).toHaveURL(/\?section=subscriptions$/)
  await expect(page.getByRole('heading', { name: '我的订阅', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '前往支付', exact: true })).toHaveCount(0)
  await sections.getByRole('tab', { name: '购买套餐', exact: true }).click()
  await expect(page.getByRole('button', { name: '购买', exact: true })).toBeDisabled()
  await sections.getByRole('tab', { name: '额度转换', exact: true }).click()
  await expect(page.getByRole('button', { name: '预览整份转余额', exact: true })).toBeDisabled()
  await sections.getByRole('tab', { name: '兑换', exact: true }).click()
  await expect(page.getByRole('heading', { name: '兑换码', exact: true })).toBeVisible()
  await sections.getByRole('tab', { name: '退款', exact: true }).click()
  await expect(page.getByRole('button', { name: '查询退款', exact: true })).toBeVisible()
  await sections.getByRole('tab', { name: '充值', exact: true }).click()
  await expect(page.getByRole('button', { name: '前往支付', exact: true })).toBeDisabled()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  )
})

test('key drawer presents aligned fields, separate actions and restores focus on cancellation', async ({
  page,
  isMobile,
}) => {
  await page.goto('/keys')
  const create = page.getByRole('button', { name: '创建', exact: true })
  await create.click()
  const drawer = page.getByRole('dialog', { name: '创建 API Key' })
  const name = drawer.getByLabel('名称', { exact: true })
  await drawer.getByLabel('模型访问', { exact: true }).selectOption('selected')
  const models = drawer.getByLabel('允许的模型', { exact: true })
  const nameBox = await name.boundingBox()
  const modelsBox = await models.boundingBox()
  expect(nameBox).not.toBeNull()
  expect(modelsBox!.y).toBeGreaterThan(nameBox!.y + nameBox!.height)
  expect(Math.abs(modelsBox!.x - nameBox!.x)).toBeLessThan(2)
  if (isMobile) expect(nameBox!.height).toBeGreaterThanOrEqual(44)
  await drawer.getByRole('button', { name: '取消', exact: true }).click()
  await expect(drawer).not.toBeVisible()
  await expect(create).toBeFocused()
})

test('usage rows open details from the keyboard', async ({ page }) => {
  await page.goto('/usage-logs')
  const row = page.getByRole('row').filter({ hasText: 'gpt-4o' })
  await row.focus()
  await row.press('Enter')
  await expect(page.getByRole('dialog', { name: '调用详情' })).toBeVisible()
  await page.getByRole('button', { name: '关闭', exact: true }).click()
  await expect(row).toBeFocused()
  await row.press('Space')
  await expect(page.getByRole('dialog', { name: '调用详情' })).toBeVisible()
})
