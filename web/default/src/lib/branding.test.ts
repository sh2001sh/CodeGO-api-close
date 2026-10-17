import { strict as assert } from 'node:assert'
import { describe, test } from 'node:test'
import { normalizeLogoUrl } from './branding'
import { DEFAULT_LOGO } from './constants'

describe('brand asset migration', () => {
  test('replaces stored legacy and versioned defaults', () => {
    for (const value of [
      undefined,
      null,
      '',
      '/code-go-logo.svg',
      '/code-go-logo.svg?v=3',
      '/logo.png',
      '/favicon.ico?v=3',
    ]) {
      assert.equal(normalizeLogoUrl(value), DEFAULT_LOGO)
    }
  })
  test('preserves explicitly configured custom logos', () => {
    for (const value of [
      '/uploads/company.svg?v=3',
      'https://example.com/logo.svg',
      '//cdn.example.com/logo.png',
    ]) {
      assert.equal(normalizeLogoUrl(value), value)
    }
  })
})
