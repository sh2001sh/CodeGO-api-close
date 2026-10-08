import { test, expect } from '@playwright/test'
import { prepareRealStack } from './real-stack-helper'

const url = process.env.V3_REAL_URL
test.skip(!url, 'Only run against the explicitly selected isolated test container')
test.beforeEach(async ({ page }) => prepareRealStack(page))

test('real container registration, key creation, pages and authorization boundary', async ({
  page,
}) => {
  const username = `browser_${crypto.randomUUID().replaceAll('-', '').slice(0, 12)}`
  await page.goto('/sign-up')
  await page.getByLabel('用户名', { exact: true }).fill(username)
  await page.getByLabel('邮箱', { exact: true }).fill(`${username}@example.test`)
  await page.getByLabel('密码', { exact: true }).fill(`Test-${crypto.randomUUID()}`)
  await page.getByRole('checkbox', { name: /我已阅读并同意/ }).check()
  await page.getByRole('button', { name: '注册', exact: true }).click()
  await expect(page.getByRole('heading', { name: '仪表板', exact: true })).toBeVisible()

  await page.goto('/keys')
  await page.getByRole('button', { name: '创建', exact: true }).click()
  await page.getByLabel('名称', { exact: true }).fill('容器浏览器验收')
  await page.getByRole('button', { name: '创建', exact: true }).last().click()
  await expect(page.getByText('新 API Key', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '关闭', exact: true }).click()
  await expect(page.getByText('容器浏览器验收', { exact: true })).toBeVisible()

  for (const [path, title] of [
    ['/wallet', '钱包'],
    ['/orders', '订单'],
    ['/usage-logs', '使用日志'],
    ['/blind-box', '盲盒'],
    ['/profile', '个人资料'],
    ['/channel-market', '渠道市场'],
    ['/my-channels', '渠道工作台'],
    ['/transfers', '钱包转账'],
    ['/billing', '账单明细'],
  ]) {
    await page.goto(path)
    await expect(page.getByRole('heading', { name: title, exact: true, level: 1 })).toBeVisible()
    await expect(page.getByRole('alert')).toHaveCount(0)
  }
  for (const path of ['/users', '/market-admin', '/redemptions', '/subscriptions']) {
    await page.goto(path)
    await expect(page.getByRole('alert')).toHaveText('无权执行此操作')
  }
  await page.goto('/dashboard')
  await page.getByRole('button', { name: '账户菜单' }).click()
  await page.getByRole('menuitem', { name: '退出登录' }).click()
  await expect(page.getByRole('heading', { name: '登录', exact: true })).toBeVisible()
})
