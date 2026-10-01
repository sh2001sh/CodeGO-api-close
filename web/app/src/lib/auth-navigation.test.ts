import { describe, expect, it } from 'vitest'
import { oauthCallbackTarget, oauthStartURL, safeLocalReturn } from './auth-navigation'

describe('authentication navigation boundaries', () => {
  it('preserves the complete local OIDC authorization query', () => {
    const target =
      '/api/oidc/authorize?client_id=nodebb&redirect_uri=https%3A%2F%2Fcommunity.test%2Fcallback&state=a%2Bb&scope=openid+profile'
    expect(safeLocalReturn(target)).toBe(target)
    const link = new URL(oauthStartURL('company', { returnTo: target }), 'https://codego.test')
    expect(link.pathname).toBe('/api/oauth/company')
    expect(link.searchParams.get('returnTo')).toBe(target)
  })
  it.each([
    '',
    '//evil.test',
    'https://evil.test',
    'javascript:alert(1)',
    'dashboard',
    '/\\evil.test',
    '/%5cevil.test',
    '/%2f/evil.test',
    '/%252f%252fevil.test',
    '/%0d%0aLocation:evil',
    '/%2e%2e//evil.test',
    ' /dashboard',
    '/%FF',
  ])('rejects unsafe return %s', (value) => {
    expect(safeLocalReturn(value)).toBeUndefined()
    expect(oauthStartURL('github', { returnTo: value })).toBe('/api/oauth/github')
  })
  it('forwards only the callback code and state, encoded once', () => {
    expect(
      oauthCallbackTarget(
        'company',
        '?code=a%2Bb%26c&state=opaque%3Dvalue&returnTo=//evil.test&redirect_uri=https://evil.test',
      ),
    ).toBe('/api/oauth/company?state=opaque%3Dvalue&code=a%2Bb%26c')
  })
  it.each([
    '?error=access_denied&error_description=%3Cscript%3E',
    '?code=a',
    '?state=a',
    '?state=&code=a',
    '?state=a&code=b&code=c',
  ])('rejects incomplete or denied callback %s', (search) => {
    expect(() => oauthCallbackTarget('company', search)).toThrow()
  })
  it('rejects callback providers containing a path or authority', () => {
    expect(() => oauthCallbackTarget('../github', '?state=a&code=b')).toThrow()
    expect(() => oauthCallbackTarget('\\evil.test', '?state=a&code=b')).toThrow()
    expect(safeLocalReturn('/' + 'a'.repeat(4096))).toBeUndefined()
  })
})
