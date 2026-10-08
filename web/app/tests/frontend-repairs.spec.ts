import { test, expect, type Page } from '@playwright/test'
import { fixtureAPI, user } from './fixtures'
import en from '../src/locales/en'
import hk from '../src/locales/zh-HK.json' with { type: 'json' }
import ar from '../src/locales/ar.json' with { type: 'json' }

test('reauthenticating as another account starts a fresh dashboard without previous personal caches', async ({
  page,
}) => {
  await fixtureAPI(page)
  let account = 'A'
  const calls: string[] = []
  await page.route('**/api/user/self', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: { ...user, id: account === 'A' ? 1 : 2, display_name: `account-${account}` },
      },
    }),
  )
  await page.route('**/api/wallet', (route) => {
    calls.push(account)
    return route.fulfill({
      json: {
        success: true,
        data: {
          account_id: account === 'A' ? 1 : 2,
          balance_micro_credits: account === 'A' ? '111000000' : '222000000',
          version: 0,
        },
      },
    })
  })
  await page.route('**/api/token/', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: [
          {
            id: account === 'A' ? 1 : 2,
            name: `private-key-${account}`,
            status: 'active',
            key_prefix: 'sk-test',
            allowed_models: [],
            allowed_cidrs: [],
          },
        ],
      },
    }),
  )
  await page.route('**/api/user/login', (route) => {
    account = 'B'
    return route.fulfill({ json: { success: true, data: { user: { ...user, id: 2 } } } })
  })
  await page.goto('/dashboard')
  await expect(page.locator('.overview-wallet-value')).toHaveText('111 credits')
  // Simulate browser history navigation, retaining the same JavaScript document and cache.
  await page.evaluate(() => {
    document.body.dataset.sameDocument = 'yes'
    history.pushState(null, '', '/sign-in')
    window.dispatchEvent(new PopStateEvent('popstate'))
  })
  await page.getByLabel('用户名', { exact: true }).fill('account-B')
  await page.getByLabel('密码', { exact: true }).fill('account-B-password')
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.locator('.overview-wallet-value')).toHaveText('222 credits')
  expect(calls).toEqual(['A', 'B'])
  expect(await page.locator('body').getAttribute('data-same-document')).toBe(null)
  await page.evaluate(() => {
    history.pushState(null, '', '/keys')
    window.dispatchEvent(new PopStateEvent('popstate'))
  })
  await expect(page.getByText('private-key-B', { exact: true })).toBeVisible()
  await expect(page.getByText('private-key-A', { exact: true })).toHaveCount(0)
})

for (const [locale, messages] of Object.entries({ en, 'zh-HK': hk, ar })) {
  test(`${locale} localizes dynamic deletion confirmation and payment errors`, async ({ page }) => {
    await fixtureAPI(page, locale)
    const t = (key: string) => messages[key as keyof typeof messages] ?? key
    await page.goto('/keys')
    await page.getByRole('checkbox', { name: t('全选当前页'), exact: true }).check()
    await page.getByRole('button', { name: t('批量删除'), exact: true }).click()
    await expect(page.getByRole('dialog')).toContainText(
      t('将删除 {count} 个 Key，删除后无法恢复，确认继续？').replace('{count}', '1'),
    )
    await page
      .getByRole('dialog')
      .getByRole('button', { name: t('取消'), exact: true })
      .click()
    await page.goto('/wallet')
    await page.getByLabel(t('支付金额'), { exact: true }).fill('1.234')
    await page.getByRole('button', { name: t('前往支付'), exact: true }).click()
    await expect(page.getByRole('alert')).toHaveText(
      t('支付金额最多支持 {digits} 位小数').replace('{digits}', '2'),
    )
    await page.goto('/transfers')
    await page.getByLabel(t('收款人 ID'), { exact: true }).fill('ABC123')
    await page.getByLabel(t('转账 credits'), { exact: true }).fill('1.5')
    await page.getByLabel(t('支付密码'), { exact: true }).first().fill('payment-password')
    await page.getByRole('button', { name: t('核对收款人'), exact: true }).click()
    await expect(page.getByRole('alert')).toHaveText(
      t('转账金额需按 {amount} 递增').replace('{amount}', '1 credits'),
    )
  })
}

test('invalid income reclamation amounts stay in the form error boundary before confirmation', async ({
  page,
}) => {
  await fixtureAPI(page)
  const requests: string[] = []
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  page.on('request', (request) => {
    if (new URL(request.url()).pathname.endsWith('/owner-income/release'))
      requests.push(request.url())
  })
  await page.goto('/market-admin')
  await page.getByText('回收已结算收入', { exact: true }).click()
  await page.getByLabel('渠道主 ID（逗号分隔）', { exact: true }).fill('1')
  for (const amount of ['-1', '1.1234567', 'invalid']) {
    await page.getByLabel('回收 credits（留空回收全部）', { exact: true }).fill(amount)
    await page.getByRole('button', { name: '回收收入', exact: true }).click()
    await expect(page.getByRole('alert')).toHaveText('请输入最多六位小数的正数')
    await expect(page.getByRole('dialog')).toHaveCount(0)
  }
  expect(requests).toEqual([])
  expect(errors).toEqual([])
})

test('partial settings failures refresh committed values and state exactly what was saved', async ({
  page,
}) => {
  await fixtureAPI(page)
  let serverAddress = 'https://old.example.test'
  let reads = 0
  await page.route('**/api/settings', (route) => {
    reads++
    return route.fulfill({
      json: {
        success: true,
        data: [
          { key: 'ServerAddress', value: serverAddress, sensitive: false },
          { key: 'Notice', value: 'old notice', sensitive: false },
        ],
      },
    })
  })
  await page.route('**/api/settings/ServerAddress', (route) => {
    serverAddress = route.request().postDataJSON().value
    return route.fulfill({ json: { success: true, data: null } })
  })
  await page.route('**/api/settings/Notice', (route) =>
    route.fulfill({ status: 503, json: { success: false, message: '保存公告失败' } }),
  )
  await page.goto('/settings')
  await page.getByLabel('站点公开地址', { exact: true }).fill('https://new.example.test')
  await page.getByLabel('系统公告', { exact: true }).fill('new notice')
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('保存公告失败')
  await expect(page.getByRole('status')).toContainText('已保存 1 项：ServerAddress。其余配置未保存')
  await expect.poll(() => reads).toBe(2)
  expect(serverAddress).toBe('https://new.example.test')
})

test('settings search accepts the label displayed in the selected language', async ({ page }) => {
  await fixtureAPI(page, 'en')
  await page.goto('/settings')
  await page.getByLabel('Search', { exact: true }).fill('Registration email verification code')
  await expect(
    page.getByLabel('Registration email verification code', { exact: true }),
  ).toBeVisible()
})

async function removalFixture(page: Page) {
  await fixtureAPI(page)
  let registered = true
  await page.route('**/api/passkey', (route) =>
    route.fulfill({ json: { success: true, data: { enabled: true, count: registered ? 1 : 0 } } }),
  )
  await page.route('**/api/user/passkey/verify/begin', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          publicKey: {
            challenge: 'AQIDBA',
            rpId: 'localhost',
            userVerification: 'required',
            allowCredentials: [{ type: 'public-key', id: 'AQIDBA' }],
          },
        },
      },
    }),
  )
  const cdp = await page.context().newCDPSession(page)
  await cdp.send('WebAuthn.enable')
  const { authenticatorId } = await cdp.send('WebAuthn.addVirtualAuthenticator', {
    options: {
      protocol: 'ctap2',
      transport: 'internal',
      hasResidentKey: true,
      hasUserVerification: true,
      isUserVerified: true,
      automaticPresenceSimulation: true,
    },
  })
  await page.goto('http://localhost:3100/profile')
  await expect(page.getByRole('button', { name: '移除通行密钥', exact: true })).toBeVisible()
  const privateKey = await page.evaluate(async () => {
    const pair = await crypto.subtle.generateKey({ name: 'ECDSA', namedCurve: 'P-256' }, true, [
      'sign',
      'verify',
    ])
    const encoded = await crypto.subtle.exportKey('pkcs8', pair.privateKey)
    return btoa(String.fromCharCode(...new Uint8Array(encoded)))
  })
  await cdp.send('WebAuthn.addCredential', {
    authenticatorId,
    credential: {
      credentialId: 'AQIDBA==',
      isResidentCredential: true,
      rpId: 'localhost',
      privateKey,
      userHandle: 'MQ==',
      signCount: 0,
    },
  })
  return {
    removed: () => {
      registered = false
    },
  }
}

test('passkey removal verifies a native assertion before DELETE and refreshes account state', async ({
  page,
}) => {
  const state = await removalFixture(page)
  const steps: string[] = []
  await page.route('**/api/user/passkey/verify/finish', (route) => {
    expect(route.request().postDataJSON().response.signature).toBeTruthy()
    steps.push('verified')
    return route.fulfill({ json: { success: true, data: { verified: true } } })
  })
  await page.route('**/api/user/passkey', (route) => {
    expect(route.request().method()).toBe('DELETE')
    steps.push('removed')
    state.removed()
    return route.fulfill({ json: { success: true, data: null } })
  })
  await page.getByRole('button', { name: '移除通行密钥', exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: '取消', exact: true }).click()
  expect(steps).toEqual([])
  await page.getByRole('button', { name: '移除通行密钥', exact: true }).click()
  await page.getByRole('button', { name: '确认移除', exact: true }).click()
  await expect.poll(() => steps).toEqual(['verified', 'removed'])
  await expect(page.getByRole('button', { name: '移除通行密钥', exact: true })).toHaveCount(0)
  expect(steps).toEqual(['verified', 'removed'])
})

for (const failure of ['验证失败', '验证已过期或未保留其他登录方式']) {
  test(`passkey removal preserves account state on failure: ${failure}`, async ({ page }) => {
    await removalFixture(page)
    let deleted = false
    await page.route('**/api/user/passkey/verify/finish', (route) =>
      failure === '验证失败'
        ? route.fulfill({ status: 401, json: { success: false, message: failure } })
        : route.fulfill({ json: { success: true, data: { verified: true } } }),
    )
    await page.route('**/api/user/passkey', (route) => {
      deleted = true
      return route.fulfill({ status: 403, json: { success: false, message: failure } })
    })
    await page.getByRole('button', { name: '移除通行密钥', exact: true }).click()
    await page.getByRole('button', { name: '确认移除', exact: true }).click()
    await expect(page.getByRole('alert')).toHaveText(failure)
    await expect(page.getByRole('button', { name: '移除通行密钥', exact: true })).toBeVisible()
    expect(deleted).toBe(failure !== '验证失败')
  })
}

test('canceling native passkey verification never finishes proof or deletes credentials', async ({
  page,
}) => {
  await removalFixture(page)
  const sent: string[] = []
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname
    if (path === '/api/user/passkey/verify/finish' || path === '/api/user/passkey') sent.push(path)
  })
  await page.evaluate(() => {
    Object.defineProperty(navigator.credentials, 'get', {
      value: async () => {
        throw new DOMException('User canceled', 'NotAllowedError')
      },
    })
  })
  await page.getByRole('button', { name: '移除通行密钥', exact: true }).click()
  await page.getByRole('button', { name: '确认移除', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('通行密钥验证已取消')
  await expect(page.getByRole('button', { name: '移除通行密钥', exact: true })).toBeVisible()
  expect(sent).toEqual([])
})
