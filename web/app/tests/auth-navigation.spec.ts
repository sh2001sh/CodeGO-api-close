import { test, expect } from '@playwright/test'
import { fixtureAPI } from './fixtures'

test.beforeEach(async ({ page }) => fixtureAPI(page))
const oidcReturn =
  '/api/oidc/authorize?client_id=nodebb&response_type=code&redirect_uri=https%3A%2F%2Fcommunity.test%2Fauth%2Fcallback&scope=openid+profile&state=exact%2Bstate&code_challenge=opaque_challenge&code_challenge_method=S256'

test('password login resumes OIDC through browser navigation with its original query', async ({
  page,
}) => {
  await page.route('**/api/oidc/authorize?**', (route) =>
    route.fulfill({ contentType: 'text/html; charset=utf-8', body: '<h1>社区授权继续</h1>' }),
  )
  await page.goto(`/sign-in?returnTo=${encodeURIComponent(oidcReturn)}`)
  const link = page.getByRole('link', { name: 'GitHub', exact: true })
  const href = await link.getAttribute('href')
  expect(new URL(href!, 'http://127.0.0.1:3100').searchParams.get('returnTo')).toBe(oidcReturn)
  await expect(page.getByRole('link', { name: '创建账号', exact: true })).toHaveAttribute(
    'href',
    `/sign-up?returnTo=${encodeURIComponent(oidcReturn)}`,
  )
  await page.getByLabel('用户名', { exact: true }).fill('operator')
  await page.getByLabel('密码', { exact: true }).fill('test-password')
  const continued = page.waitForRequest(
    (request) => new URL(request.url()).pathname === '/api/oidc/authorize',
  )
  await page.getByRole('button', { name: '登录', exact: true }).click()
  const request = await continued
  expect(new URL(request.url()).search).toBe(new URL(oidcReturn, 'http://127.0.0.1:3100').search)
  expect(request.isNavigationRequest()).toBe(true)
  await expect(page.getByRole('heading', { name: '社区授权继续', exact: true })).toBeVisible()
})

for (const target of ['//evil.test', '/%2f/evil.test', '/%5cevil.test', 'https://evil.test'])
  test(`unsafe return falls back to dashboard: ${target}`, async ({ page }) => {
    await page.goto(`/sign-in?returnTo=${encodeURIComponent(target)}`)
    await expect(page.getByRole('link', { name: 'GitHub', exact: true })).toHaveAttribute(
      'href',
      '/api/oauth/github',
    )
    await page.getByLabel('用户名', { exact: true }).fill('operator')
    await page.getByLabel('密码', { exact: true }).fill('test-password')
    await page.getByRole('button', { name: '登录', exact: true }).click()
    await expect(page.getByRole('heading', { name: '仪表板', exact: true })).toBeVisible()
  })

test('the pre-cut callback forwards only selected fields as a browser navigation', async ({
  page,
}) => {
  await page.context().addCookies([
    {
      name: 'oauth-test-state',
      value: 'opaque-test-state',
      domain: '127.0.0.1',
      path: '/api/oauth',
      httpOnly: true,
      sameSite: 'Lax',
    },
  ])
  await page.route('**/api/oauth/company?**', (route) =>
    route.fulfill({ contentType: 'text/html; charset=utf-8', body: '<h1>服务器处理回调</h1>' }),
  )
  const forwarded = page.waitForRequest(
    (request) => new URL(request.url()).pathname === '/api/oauth/company',
  )
  await page.goto(
    '/oauth/company?state=state%2Bvalue&code=code%26value&returnTo=//evil.test&redirect_uri=https://evil.test',
  )
  const request = await forwarded
  expect(new URL(request.url()).search).toBe('?state=state%2Bvalue&code=code%26value')
  expect(request.isNavigationRequest()).toBe(true)
  expect(await request.headerValue('cookie')).toContain('oauth-test-state=opaque-test-state')
  await expect(page.getByRole('heading', { name: '服务器处理回调', exact: true })).toBeVisible()
})

test('native browser passkey login resumes the same OIDC return path', async ({ page }) => {
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
  await page.route('**/api/passkey/login/begin', (route) =>
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
  await page.route('**/api/oidc/authorize?**', (route) =>
    route.fulfill({ contentType: 'text/html; charset=utf-8', body: '<h1>通行密钥授权继续</h1>' }),
  )
  await page.goto(`http://localhost:3100/sign-in?returnTo=${encodeURIComponent(oidcReturn)}`)
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
  const finish = page.waitForRequest('**/api/passkey/login/finish')
  await page.getByRole('button', { name: '通行密钥登录', exact: true }).click()
  const assertion = (await finish).postDataJSON()
  expect(assertion.type).toBe('public-key')
  expect(assertion.response.signature).toBeTruthy()
  await expect(page.getByRole('heading', { name: '通行密钥授权继续', exact: true })).toBeVisible()
  expect(new URL(page.url()).search).toBe(new URL(oidcReturn, 'http://127.0.0.1:3100').search)
})

test('denied or incomplete callbacks render safe errors without contacting the provider', async ({
  page,
}) => {
  const forwarded: string[] = []
  page.on('request', (request) => {
    if (new URL(request.url()).pathname.startsWith('/api/oauth/company'))
      forwarded.push(request.url())
  })
  await page.goto(
    '/oauth/company?error=access_denied&error_description=%3Cscript%3Ealert(1)%3C/script%3E',
  )
  await expect(page.getByRole('alert')).toHaveText('外部账号未完成授权，请重新登录。')
  await expect(page.getByText('alert(1)', { exact: true })).toHaveCount(0)
  await page.goto('/oauth/company?state=only-state')
  await expect(page.getByRole('alert')).toHaveText('外部账号回调信息无效，请重新登录。')
  expect(forwarded).toEqual([])
})
