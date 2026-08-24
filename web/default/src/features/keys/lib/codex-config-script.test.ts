import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  buildCodexProviderBlock,
  buildLinuxScript,
  buildWindowsScript,
} from './codex-config-script.ts'

const serverAddress = 'https://shu26.cfd'

describe('Codex WebSocket setup', () => {
  test('advertises Responses WebSocket support in the provider block', () => {
    assert.match(
      buildCodexProviderBlock(serverAddress),
      /supports_websockets = true/
    )
  })

  test('adds WebSocket support to the Windows-generated provider block', () => {
    const script = buildWindowsScript(serverAddress, 'sk-test', 'gpt-test')

    assert.ok(
      script.includes(
        "'supports_websockets = true','# END CODEXFORALL MANAGED PROVIDER'"
      )
    )
  })

  test('adds WebSocket support to the Linux and macOS provider block', () => {
    const script = buildLinuxScript(serverAddress, 'sk-test', 'gpt-test')

    assert.match(script, /supports_websockets = true/)
  })
})
