import { createHmac } from 'node:crypto'
import { test, expect } from '@playwright/test'
import { prepareRealStack } from './real-stack-helper'
import type { Schema } from '../src/lib/types'

const url = process.env.V3_REAL_URL
test.skip(!url, 'Only run against the explicitly selected isolated test container')
test.beforeEach(async ({ page }) => prepareRealStack(page))

function totp(secret: string): string {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'
  let bits = ''
  for (const char of secret.toUpperCase().replaceAll('=', '')) {
    const index = alphabet.indexOf(char)
    if (index < 0) throw new Error('Invalid server TOTP secret')
    bits += index.toString(2).padStart(5, '0')
  }
  const bytes = []
  for (let index = 0; index + 8 <= bits.length; index += 8)
    bytes.push(parseInt(bits.slice(index, index + 8), 2))
  const counter = Buffer.alloc(8)
  counter.writeBigUInt64BE(BigInt(Math.floor(Date.now() / 30000)))
  const digest = createHmac('sha1', Buffer.from(bytes)).update(counter).digest()
  const offset = digest[digest.length - 1] & 15
  return String((digest.readUInt32BE(offset) & 0x7fffffff) % 1000000).padStart(6, '0')
}

test('real restored TOTP backup replay, retained desktop API revoke and reward pages use the new backend', async ({
  page,
}) => {
  const username = `restore_${crypto.randomUUID().replaceAll('-', '').slice(0, 12)}`
  const password = `Test-${crypto.randomUUID()}`
  await page.goto('/sign-up')
  await page.getByLabel('用户名', { exact: true }).fill(username)
  await page.getByLabel('邮箱', { exact: true }).fill(`${username}@example.test`)
  await page.getByLabel('密码', { exact: true }).fill(password)
  await page.getByRole('checkbox', { name: /我已阅读并同意/ }).check()
  await page.getByRole('button', { name: '注册', exact: true }).click()
  await expect(page.getByRole('heading', { name: '仪表板', exact: true })).toBeVisible()

  await page.goto('/profile')
  const setupResponse = page.waitForResponse('**/api/user/2fa/setup')
  await page.getByRole('button', { name: '设置两步验证', exact: true }).click()
  const setup: Schema['TwoFactorSetup'] = (await (await setupResponse).json()).data
  expect(setup.backup_codes.length).toBe(4)
  await page.getByLabel('两步验证码或备用码', { exact: true }).fill(totp(setup.secret))
  await page.getByRole('button', { name: '启用两步验证', exact: true }).click()
  await expect(page.getByRole('heading', { name: '登录', exact: true })).toBeVisible()

  const login = async (backup: string) => {
    await page.getByLabel('用户名', { exact: true }).fill(username)
    await page.getByLabel('密码', { exact: true }).fill(password)
    await page.getByRole('button', { name: '登录', exact: true }).click()
    await page.getByLabel('两步验证码或备用码', { exact: true }).fill(backup)
    await page.getByRole('button', { name: '验证并登录', exact: true }).click()
  }
  await login(setup.backup_codes[0])
  await expect(page.getByRole('heading', { name: '仪表板', exact: true })).toBeVisible()
  await page.getByRole('button', { name: '账户菜单' }).click()
  await page.getByRole('menuitem', { name: '退出登录' }).click()
  await expect(page.getByRole('heading', { name: '登录', exact: true })).toBeVisible()
  await login(setup.backup_codes[0])
  await expect(page.getByRole('alert')).toBeVisible()
  expect(new URL(page.url()).pathname).toBe('/sign-in')
  await page.getByLabel('两步验证码或备用码', { exact: true }).fill(setup.backup_codes[1])
  await page.getByRole('button', { name: '验证并登录', exact: true }).click()
  await expect(page.getByRole('heading', { name: '仪表板', exact: true })).toBeVisible()

  const started = await page.request.post('/api/desktop/auth/session', {
    data: { device_name: `Browser ${username}`, platform: 'test', app_version: 'restoration' },
  })
  expect(started.ok()).toBe(true)
  const session: Schema['DesktopStartResult'] = (await started.json()).data
  const approved = await page.request.post('/api/desktop/auth/approve', {
    data: { session_id: session.session_id },
  })
  expect(approved.ok()).toBe(true)
  const polled = await page.request.post('/api/desktop/auth/poll', {
    data: { session_id: session.session_id },
  })
  expect(polled.ok()).toBe(true)
  const grant: Schema['DesktopPollResult'] = (await polled.json()).data
  expect(grant.authenticated).toBe(true)
  expect(grant.access_token).toBeTruthy()
  const revoked = await page.request.delete(`/api/desktop/devices/${grant.device_id}`)
  expect(revoked.ok()).toBe(true)
  await page.goto('/desktop/devices')
  await expect(page).toHaveURL(/\/profile$/)
  const denied = await page.request.get('/api/desktop/account/summary', {
    headers: { Authorization: `Bearer ${grant.access_token}`, 'X-CodeGo-API-Version': '3' },
  })
  expect(denied.status()).toBe(401)

  for (const [path, title] of [
    ['/referral-rewards', '邀请奖励'],
    ['/group-favorites', '分组收藏'],
  ]) {
    await page.goto(path)
    await expect(page.getByRole('heading', { name: title, exact: true, level: 1 })).toBeVisible()
    await expect(page.getByRole('alert')).toHaveCount(0)
    if (path === '/referral-rewards')
      await expect(page.getByRole('textbox', { name: '邀请链接', exact: true })).toHaveValue(
        /\/sign-up\?ref=[A-Za-z0-9_-]+$/,
      )
  }
})
