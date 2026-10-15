import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  buildDesktopAuthorizeViewModel,
  formatDesktopAuthorizePlatform,
  type DesktopAuthorizeSession,
} from './-authorize-view.ts'

function createSession(
  overrides: Partial<DesktopAuthorizeSession> = {}
): DesktopAuthorizeSession {
  return {
    app_version: '1.6.0',
    approved_at: 0,
    created_at: 1710000000,
    device_name: 'Office Surface',
    expires_at: 1710003600,
    permissions: ['Use your existing Code Go API tokens'],
    platform: 'windows',
    session_id: 'session-1',
    status: 'pending',
    user_code: 'ABCD-1234',
    ...overrides,
  }
}

describe('desktop authorize view model', () => {
  test('keeps the loading state conservative', () => {
    const viewModel = buildDesktopAuthorizeViewModel({
      isLoading: true,
    })

    assert.equal(viewModel.titleKey, 'Authorization request')
    assert.equal(viewModel.status, 'pending')
    assert.equal(viewModel.primaryActionKey, null)
    assert.equal(viewModel.noticeKey, null)
    assert.equal(viewModel.canReview, false)
    assert.equal(viewModel.noticeTone, 'none')
  })

  test('surfaces session fetch errors', () => {
    const viewModel = buildDesktopAuthorizeViewModel({
      error: new Error('Desktop authorization session not found'),
      isLoading: false,
    })

    assert.equal(
      viewModel.errorMessage,
      'Desktop authorization session not found'
    )
    assert.equal(viewModel.status, 'unknown')
    assert.equal(viewModel.primaryActionKey, null)
    assert.equal(viewModel.canReview, false)
    assert.equal(viewModel.noticeTone, 'none')
  })

  test('shows approval actions for pending sessions', () => {
    const viewModel = buildDesktopAuthorizeViewModel({
      isLoading: false,
      session: createSession(),
    })

    assert.equal(viewModel.titleKey, 'Authorization request')
    assert.equal(viewModel.status, 'pending')
    assert.equal(viewModel.platformLabel, 'windows · 1.6.0')
    assert.equal(viewModel.primaryActionKey, 'Approve desktop')
    assert.equal(viewModel.secondaryActionKey, 'Reject desktop')
    assert.equal(viewModel.canReview, true)
    assert.equal(viewModel.noticeTone, 'none')
  })

  test('locks the screen into an approved confirmation', () => {
    const viewModel = buildDesktopAuthorizeViewModel({
      isLoading: false,
      session: createSession({ status: 'approved' }),
    })

    assert.equal(viewModel.titleKey, 'Desktop approved')
    assert.equal(viewModel.status, 'approved')
    assert.match(
      viewModel.noticeKey || '',
      /can now access your Code Go account/
    )
    assert.equal(viewModel.primaryActionKey, null)
    assert.equal(viewModel.canReview, false)
    assert.equal(viewModel.noticeTone, 'success')
  })

  test('shows rejected sessions as terminal', () => {
    const viewModel = buildDesktopAuthorizeViewModel({
      isLoading: false,
      session: createSession({ status: 'rejected' }),
    })

    assert.equal(viewModel.titleKey, 'Authorization request')
    assert.equal(viewModel.status, 'rejected')
    assert.match(viewModel.noticeKey || '', /request was rejected/)
    assert.equal(viewModel.primaryActionKey, null)
    assert.equal(viewModel.canReview, false)
    assert.equal(viewModel.noticeTone, 'danger')
  })

  test('marks expired sessions clearly', () => {
    const viewModel = buildDesktopAuthorizeViewModel({
      isLoading: false,
      session: createSession({ status: 'expired' }),
    })

    assert.equal(viewModel.titleKey, 'Session expired')
    assert.equal(viewModel.status, 'expired')
    assert.match(viewModel.noticeKey || '', /has expired/)
    assert.equal(viewModel.primaryActionKey, null)
    assert.equal(viewModel.canReview, false)
    assert.equal(viewModel.noticeTone, 'danger')
  })

  test('treats unknown statuses conservatively and blocks further approval', () => {
    const viewModel = buildDesktopAuthorizeViewModel({
      isLoading: false,
      session: createSession({ status: 'revoked' }),
    })

    assert.equal(viewModel.titleKey, 'Authorization request')
    assert.equal(viewModel.status, 'unknown')
    assert.match(viewModel.noticeKey || '', /unknown state/)
    assert.equal(viewModel.primaryActionKey, null)
    assert.equal(viewModel.secondaryActionKey, null)
    assert.equal(viewModel.canReview, false)
    assert.equal(viewModel.noticeTone, 'danger')
  })
})

describe('formatDesktopAuthorizePlatform', () => {
  test('joins platform and version for known desktop builds', () => {
    assert.equal(
      formatDesktopAuthorizePlatform(
        createSession({ app_version: '2.0.0', platform: 'macOS' })
      ),
      'macOS · 2.0.0'
    )
  })

  test('falls back when platform information is absent', () => {
    assert.equal(
      formatDesktopAuthorizePlatform(
        createSession({ app_version: '', platform: '' })
      ),
      'Unknown'
    )
  })
})
