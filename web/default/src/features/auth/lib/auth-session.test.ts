import { describe, expect, it } from 'bun:test'
import { loadActiveAuthSession } from './auth-session'

describe('loadActiveAuthSession', () => {
  it('accepts only a server-confirmed positive user id', async () => {
    const user = { id: 42, username: 'member', role: 1 }
    await expect(
      loadActiveAuthSession(async () => ({ success: true, data: user }))
    ).resolves.toEqual(user)
    await expect(
      loadActiveAuthSession(async () => ({
        success: true,
        data: { ...user, id: 0 },
      }))
    ).resolves.toBeNull()
  })

  it('rejects stale cache and failed session requests', async () => {
    await expect(
      loadActiveAuthSession(async () => ({ success: false, data: null }))
    ).resolves.toBeNull()
    await expect(
      loadActiveAuthSession(async () => {
        throw new Error('session expired')
      })
    ).resolves.toBeNull()
  })
})
