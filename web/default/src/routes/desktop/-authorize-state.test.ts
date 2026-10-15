import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { getDesktopAuthorizationStatus } from './-authorize-state.ts'

describe('getDesktopAuthorizationStatus', () => {
  test('maps known backend statuses', () => {
    assert.equal(getDesktopAuthorizationStatus('approved'), 'approved')
    assert.equal(getDesktopAuthorizationStatus('expired'), 'expired')
    assert.equal(getDesktopAuthorizationStatus('pending'), 'pending')
    assert.equal(getDesktopAuthorizationStatus('rejected'), 'rejected')
  })

  test('treats unknown or empty statuses conservatively', () => {
    assert.equal(getDesktopAuthorizationStatus('revoked'), 'unknown')
    assert.equal(getDesktopAuthorizationStatus(undefined), 'unknown')
    assert.equal(getDesktopAuthorizationStatus(null), 'unknown')
  })
})
