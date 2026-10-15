import { describe, expect, it } from 'bun:test'
import {
  normalizeAuthRedirect,
  requiresDocumentNavigation,
} from './auth-redirect'

describe('auth redirect', () => {
  it('keeps safe same-origin paths and rejects external redirects', () => {
    expect(normalizeAuthRedirect('/dashboard')).toBe('/dashboard')
    expect(normalizeAuthRedirect()).toBe('/dashboard')
    expect(normalizeAuthRedirect('https://example.com')).toBe('/dashboard')
    expect(normalizeAuthRedirect('//example.com')).toBe('/dashboard')
  })

  it('uses document navigation for backend routes', () => {
    const authorizePath =
      '/api/oidc/authorize?response_type=code&client_id=nodebb-community'

    expect(requiresDocumentNavigation(authorizePath)).toBe(true)
    expect(requiresDocumentNavigation('/dashboard')).toBe(false)
    expect(requiresDocumentNavigation('/apiary')).toBe(false)
  })
})
