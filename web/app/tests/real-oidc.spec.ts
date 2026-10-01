import { createHash } from 'node:crypto'
import { test, expect } from '@playwright/test'

const url = process.env.V3_REAL_URL
const clientID = process.env.V3_REAL_OIDC_CLIENT_ID
const redirectURI = process.env.V3_REAL_OIDC_REDIRECT_URI
test.skip(
  !url || !clientID || !redirectURI,
  'Real OIDC requires the explicitly configured isolated issuer and registered local callback',
)

test('real OIDC issuer resumes authorization after password login and preserves NodeBB state', async ({
  page,
}) => {
  if (
    !url ||
    !/^http:\/\/(localhost|127\.0\.0\.1):1808[34]$/.test(url) ||
    !clientID ||
    !redirectURI ||
    new URL(redirectURI).origin !== url
  )
    throw new Error(
      'Real OIDC must use the selected isolated local stack and a registered callback on the same test origin',
    )
  const username = `oidc_${crypto.randomUUID().replaceAll('-', '').slice(0, 12)}`
  const password = `Test-${crypto.randomUUID()}`
  await page.goto('/sign-up')
  await page.getByLabel('用户名', { exact: true }).fill(username)
  await page.getByLabel('邮箱', { exact: true }).fill(`${username}@example.test`)
  await page.getByLabel('密码', { exact: true }).fill(password)
  await page.getByRole('button', { name: '注册', exact: true }).click()
  await expect(page.getByRole('heading', { name: '仪表板', exact: true })).toBeVisible()
  await page.context().clearCookies()

  const state = `nodebb_${crypto.randomUUID()}`
  const verifier = crypto.randomUUID().replaceAll('-', '').repeat(2)
  const query = new URLSearchParams({
    client_id: clientID,
    redirect_uri: redirectURI,
    response_type: 'code',
    scope: 'openid profile',
    state,
    nonce: crypto.randomUUID(),
    code_challenge: createHash('sha256').update(verifier).digest('base64url'),
    code_challenge_method: 'S256',
  })
  await page.goto(`/api/oidc/authorize?${query}`)
  await expect(page.getByRole('heading', { name: '登录', exact: true })).toBeVisible()
  const returnTo = new URL(page.url()).searchParams.get('returnTo')
  expect(returnTo).toBeTruthy()
  const authorization = new URL(returnTo!, url)
  expect(authorization.origin).toBe(url)
  expect(authorization.pathname).toBe('/api/oidc/authorize')
  expect(Object.fromEntries(authorization.searchParams)).toEqual(Object.fromEntries(query))
  await page.getByLabel('用户名', { exact: true }).fill(username)
  await page.getByLabel('密码', { exact: true }).fill(password)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await page.waitForURL(
    (target) =>
      target.origin === url &&
      target.pathname === new URL(redirectURI).pathname &&
      target.searchParams.get('state') === state,
  )
  expect(new URL(page.url()).searchParams.get('code')).toBeTruthy()
  expect(new URL(page.url()).searchParams.has('error')).toBe(false)
})
