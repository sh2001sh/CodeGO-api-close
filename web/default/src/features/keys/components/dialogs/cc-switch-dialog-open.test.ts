import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { openDesktopImportDeepLink } from './cc-switch-dialog-open.ts'

describe('openDesktopImportDeepLink', () => {
  test('opens the deep link in the current page without a blank popup', () => {
    const location = { href: '' }
    const result = openDesktopImportDeepLink(
      {
        open: () => {
          throw new Error('should not open a popup')
        },
        location,
      },
      'ccswitch://v1/import?resource=provider'
    )

    assert.equal(result, 'protocol')
    assert.equal(location.href, 'ccswitch://v1/import?resource=provider')
  })
})
