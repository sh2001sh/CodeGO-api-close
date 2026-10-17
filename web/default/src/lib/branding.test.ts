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
      'code-go-logo.svg?v=3#brand',
      '/codego-mark.svg',
      '/codego-mark.svg?v=old',
      'codego-mark.svg?v=old',
      '/logo.svg',
      '/logo.svg?v=old#brand',
      './logo.svg?v=old',
      'https://codegoai.com/codego-mark.svg?v=old',
      '//www.codegoai.com/code-go-logo.svg?v=old',
      'https://shu26.cfd/codego-mark.svg?v=old',
      'http://www.shu26.cfd/logo.svg?v=old',
      DEFAULT_LOGO,
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
      'https://cdn.example.com/codego-mark.svg?v=3',
      'https://codegoai.com.example.com/logo.svg?v=3',
      'https://shu26.cfd.example.com/logo.svg?v=3',
      'https://codegoai.com/uploads/company.svg?v=3',
      '/uploads/codego-mark.svg?v=3',
    ]) {
      assert.equal(normalizeLogoUrl(value), value)
    }
  })
})
