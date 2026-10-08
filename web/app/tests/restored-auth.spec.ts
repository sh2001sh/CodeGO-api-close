import { test, expect } from '@playwright/test'
import { fixtureAPI, user } from './fixtures'

test.beforeEach(async ({ page }) => fixtureAPI(page))

test('password login waits for a successful second factor and keeps rejected proofs on the challenge', async ({
  page,
}) => {
  await page.route('**/api/user/login', (route) =>
    route.fulfill({
      json: { success: true, data: { require_2fa: true, challenge_token: 'single-use-challenge' } },
    }),
  )
  let verified = false
  await page.route('**/api/user/login/2fa', (route) => {
    const body = route.request().postDataJSON()
    expect(body.challenge_token).toBe('single-use-challenge')
    if (body.code !== '123456')
      return route.fulfill({ status: 401, json: { success: false, message: '验证码无效' } })
    verified = true
    return route.fulfill({ json: { success: true, data: { user } } })
  })
  await page.goto('/sign-in')
  await page.getByLabel('用户名', { exact: true }).fill('operator')
  await page.getByLabel('密码', { exact: true }).fill('valid-password')
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.getByRole('button', { name: '验证并登录', exact: true })).toBeVisible()
  expect(verified).toBe(false)
  expect(new URL(page.url()).pathname).toBe('/sign-in')
  await page.getByLabel('两步验证码或备用码', { exact: true }).fill('wrong')
  await page.getByRole('button', { name: '验证并登录', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('验证码无效')
  expect(new URL(page.url()).pathname).toBe('/sign-in')
  await page.getByLabel('两步验证码或备用码', { exact: true }).fill('123456')
  await page.getByRole('button', { name: '验证并登录', exact: true }).click()
  await expect(page.getByRole('heading', { name: '仪表板', exact: true })).toBeVisible()
  expect(verified).toBe(true)
})

test('email registration reports sender failure and forwards invitation and verification proof', async ({
  page,
}) => {
  await page.route('**/api/verification?**', (route) =>
    route.fulfill({ status: 503, json: { success: false, message: '邮箱服务未配置' } }),
  )
  await page.goto('/sign-up?ref=invite-code')
  await page.getByLabel('邮箱', { exact: true }).fill('new@example.test')
  await page.getByRole('button', { name: '发送邮箱验证码', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('邮箱服务未配置')
  await expect(page.getByText('验证码已发送，请查看邮箱。', { exact: true })).toHaveCount(0)
  await page.getByLabel('用户名', { exact: true }).fill('newuser')
  await page.getByLabel('密码', { exact: true }).fill('long-password')
  await page.getByLabel('邮箱验证码', { exact: true }).fill('mail-proof')
  await page.getByRole('checkbox', { name: /我已阅读并同意/ }).check()
  const registration = page.waitForRequest('**/api/user/register')
  await page.getByRole('button', { name: '注册', exact: true }).click()
  expect((await registration).postDataJSON()).toMatchObject({
    aff_code: 'invite-code',
    verification_code: 'mail-proof',
    accepted_terms_version: '2026-10-07',
    accepted_privacy_version: '2026-10-07',
    agreement_locale: 'zh-CN',
  })
})

test('password recovery preserves opaque tokens and refuses expired or replayed reset proofs', async ({
  page,
}) => {
  await page.route('**/api/user/reset', (route) => {
    expect(route.request().postDataJSON()).toEqual({
      email: 'o@example.test',
      token: 'opaque+proof&value',
      password: 'new-password',
    })
    return route.fulfill({ status: 400, json: { success: false, message: '重置链接已失效' } })
  })
  await page.goto('/user/reset?email=o%40example.test&token=opaque%2Bproof%26value')
  await page.getByLabel('新密码', { exact: true }).fill('new-password')
  await page.getByRole('button', { name: '重置密码', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('重置链接已失效')
  await expect(page.getByText('密码已重置，原有登录已失效。请用新密码登录。')).toHaveCount(0)
  await page.goto('/reset?email=o%40example.test')
  await expect(page.getByRole('alert')).toHaveText('重置链接信息不完整，请重新发送重置链接。')
  await expect(page.getByRole('button', { name: '重置密码', exact: true })).toHaveCount(0)
})
