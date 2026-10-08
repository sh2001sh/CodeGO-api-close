import { test, expect } from '@playwright/test'
import { fixtureAPI } from './fixtures'

test.beforeEach(async ({ page }) => fixtureAPI(page))

test('new key blank restrictions are unrestricted nulls', async ({ page }) => {
  await page.goto('/keys')
  await page.getByRole('button', { name: '创建', exact: true }).click()
  await page.getByLabel('名称', { exact: true }).fill('无限制测试密钥')
  const sent = page.waitForRequest(
    (request) => new URL(request.url()).pathname === '/api/token/' && request.method() === 'POST',
  )
  await page.getByRole('dialog').getByRole('button', { name: '创建', exact: true }).click()
  expect((await sent).postDataJSON()).toMatchObject({
    allowed_models: null,
    allowed_cidrs: null,
    group: null,
  })
})

test('editing a deny-all key preserves empty restrictions and its group', async ({ page }) => {
  await page.route('**/api/token/', (route) =>
    route.request().method() !== 'GET'
      ? route.fallback()
      : route.fulfill({
          json: {
            success: true,
            data: [
              {
                id: 1,
                name: '受限密钥',
                status: 'active',
                key_prefix: 'sk-test',
                group: 'private-granted',
                allowed_models: [],
                allowed_cidrs: [],
                expires_at: null,
              },
            ],
          },
        }),
  )
  await page.goto('/keys')
  await page.getByRole('button', { name: '编辑', exact: true }).first().click()
  await page.getByLabel('名称', { exact: true }).fill('重命名受限密钥')
  await expect(page.getByText('当前禁止全部模型；填写模型名以开放访问。')).toBeVisible()
  const sent = page.waitForRequest(
    (request) => new URL(request.url()).pathname === '/api/token/' && request.method() === 'PUT',
  )
  await page.getByRole('dialog').getByRole('button', { name: '保存', exact: true }).click()
  expect((await sent).postDataJSON()).toMatchObject({
    allowed_models: [],
    allowed_cidrs: [],
    group: 'private-granted',
  })
})
