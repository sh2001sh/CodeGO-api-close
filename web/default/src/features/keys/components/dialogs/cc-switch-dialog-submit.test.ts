import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { submitDesktopImportRequest } from './cc-switch-dialog-submit.ts'

const translate = (key: string) => key

describe('submitDesktopImportRequest', () => {
  test('warns when the primary model is missing', async () => {
    const result = await submitDesktopImportRequest(
      {
        app: 'claude',
        tokenId: 1,
        name: 'My Claude',
        models: {},
        target: 'codego',
      },
      {
        createDesktopImportLink: async () => {
          throw new Error('should not be called')
        },
        openDesktopImportDeepLink: () => {
          throw new Error('should not be called')
        },
        t: translate,
        windowLike: {
          location: { href: '' },
          open: () => null,
        },
      }
    )

    assert.deepEqual(result, {
      tone: 'warning',
      message: 'Please select a primary model',
    })
  })

  test('fails fast when the token is missing', async () => {
    const result = await submitDesktopImportRequest(
      {
        app: 'codex',
        tokenId: null,
        name: 'My Codex',
        models: { model: 'gpt-5.5' },
        target: 'codego',
      },
      {
        createDesktopImportLink: async () => {
          throw new Error('should not be called')
        },
        openDesktopImportDeepLink: () => {
          throw new Error('should not be called')
        },
        t: translate,
        windowLike: {
          location: { href: '' },
          open: () => null,
        },
      }
    )

    assert.deepEqual(result, {
      tone: 'error',
      message: 'Token not found',
    })
  })

  test('returns the backend error when the website cannot create a deep link', async () => {
    const result = await submitDesktopImportRequest(
      {
        app: 'gemini',
        tokenId: 8,
        name: 'My Gemini',
        models: { model: 'gemini-2.5-pro' },
        target: 'codego',
      },
      {
        createDesktopImportLink: async () => ({
          success: false,
          message: 'desktop import code expired',
        }),
        openDesktopImportDeepLink: () => {
          throw new Error('should not be called')
        },
        t: translate,
        windowLike: {
          location: { href: '' },
          open: () => null,
        },
      }
    )

    assert.deepEqual(result, {
      tone: 'error',
      message: 'desktop import code expired',
    })
  })

  test('opens the desktop deep link when the website returns a valid payload', async () => {
    const openedLinks: string[] = []
    const result = await submitDesktopImportRequest(
      {
        app: 'codex',
        tokenId: 9,
        name: 'My Codex',
        models: { model: 'gpt-5.5' },
        target: 'codego',
      },
      {
        createDesktopImportLink: async (payload) => {
          assert.deepEqual(payload, {
            target: 'codego',
            tool: 'codex',
            token_id: 9,
            name: 'My Codex',
            model: 'gpt-5.5',
            haiku_model: undefined,
            sonnet_model: undefined,
            opus_model: undefined,
            enabled: true,
          })

          return {
            success: true,
            data: {
              code: 'import-code',
              deep_link: 'codego://v1/import?resource=provider',
              config_url: 'https://shu26.cfd/api/desktop/import/config?code=1',
              expires_in_seconds: 300,
              tool: 'codex',
              token_name: 'My Codex',
              provider_name: 'Code Go Codex',
            },
          }
        },
        openDesktopImportDeepLink: (_windowLike, deepLink) => {
          openedLinks.push(deepLink)
        },
        t: translate,
        windowLike: {
          location: { href: '' },
          open: () => null,
        },
      }
    )

    assert.deepEqual(result, { tone: 'success' })
    assert.deepEqual(openedLinks, ['codego://v1/import?resource=provider'])
  })

  test('opens the CC Switch scheme for a CC Switch import', async () => {
    const openedLinks: string[] = []
    const result = await submitDesktopImportRequest(
      {
        app: 'codex',
        tokenId: 9,
        name: 'CodeGo',
        models: { model: 'gpt-5.6-luna' },
        target: 'ccswitch',
      },
      {
        createDesktopImportLink: async (payload) => {
          assert.equal(payload.target, 'ccswitch')
          return {
            success: true,
            data: {
              code: '',
              deep_link:
                'ccswitch://v1/import?resource=provider&app=codex&name=CodeGo&endpoint=https%3A%2F%2Fshu26.cfd%2Fv1&apiKey=sk-test&model=gpt-5.6-luna&enabled=true',
              config_url: '',
              expires_in_seconds: 0,
              tool: 'codex',
              token_name: 'CodeGo',
              provider_name: 'CodeGo',
            },
          }
        },
        openDesktopImportDeepLink: (_windowLike, deepLink) => {
          openedLinks.push(deepLink)
        },
        t: translate,
        windowLike: { location: { href: '' }, open: () => null },
      }
    )

    assert.deepEqual(result, { tone: 'success' })
    assert.deepEqual(openedLinks, [
      'ccswitch://v1/import?resource=provider&app=codex&name=CodeGo&endpoint=https%3A%2F%2Fshu26.cfd%2Fv1&apiKey=sk-test&model=gpt-5.6-luna&enabled=true',
    ])
  })

  test('falls back to a generic desktop-open error when the request throws', async () => {
    const result = await submitDesktopImportRequest(
      {
        app: 'claude',
        tokenId: 5,
        name: 'My Claude',
        models: {
          model: 'claude-sonnet-4',
          haikuModel: 'claude-haiku-4',
        },
        target: 'codego',
      },
      {
        createDesktopImportLink: async () => {
          throw new Error('network down')
        },
        openDesktopImportDeepLink: () => {
          throw new Error('should not be called')
        },
        t: translate,
        windowLike: {
          location: { href: '' },
          open: () => null,
        },
      }
    )

    assert.deepEqual(result, {
      tone: 'error',
      message: 'Failed to open Code Go Desktop',
    })
  })

  test('uses the CC Switch error when a CC Switch import request throws', async () => {
    const result = await submitDesktopImportRequest(
      {
        app: 'claude',
        tokenId: 5,
        name: 'My Claude',
        models: { model: 'claude-sonnet-4' },
        target: 'ccswitch',
      },
      {
        createDesktopImportLink: async () => {
          throw new Error('network down')
        },
        openDesktopImportDeepLink: () => {
          throw new Error('should not be called')
        },
        t: translate,
        windowLike: {
          location: { href: '' },
          open: () => null,
        },
      }
    )

    assert.deepEqual(result, {
      tone: 'error',
      message: 'Failed to open CC Switch',
    })
  })

  test('supports the additional desktop tools exposed by the desktop app', async () => {
    const openedLinks: string[] = []

    const result = await submitDesktopImportRequest(
      {
        app: 'opencode',
        tokenId: 17,
        name: 'My OpenCode',
        models: { model: 'gpt-5.5' },
        target: 'codego',
      },
      {
        createDesktopImportLink: async (payload) => {
          assert.deepEqual(payload, {
            target: 'codego',
            tool: 'opencode',
            token_id: 17,
            name: 'My OpenCode',
            model: 'gpt-5.5',
            haiku_model: undefined,
            sonnet_model: undefined,
            opus_model: undefined,
            enabled: true,
          })

          return {
            success: true,
            data: {
              code: 'import-opencode',
              deep_link: 'codego://v1/import?resource=provider&app=opencode',
              config_url:
                'https://shu26.cfd/api/desktop/import/config?code=opencode',
              expires_in_seconds: 300,
              tool: 'opencode',
              token_name: 'My OpenCode',
              provider_name: 'Code Go OpenCode',
            },
          }
        },
        openDesktopImportDeepLink: (_windowLike, deepLink) => {
          openedLinks.push(deepLink)
        },
        t: translate,
        windowLike: {
          location: { href: '' },
          open: () => null,
        },
      }
    )

    assert.deepEqual(result, { tone: 'success' })
    assert.deepEqual(openedLinks, [
      'codego://v1/import?resource=provider&app=opencode',
    ])
  })
})
