/* Copyright (C) 2023-2026 QuantumNous. AGPL-3.0-or-later. */
import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  createCodeExample,
  DEFAULT_ENDPOINT,
  endpointFormSchema,
} from './code-example'

test('normalizes HTTPS paths without duplicating the API version', () => {
  for (const endpoint of [
    'https://api.example.test',
    'https://api.example.test/v1/',
    '  https://api.example.test/v1///  ',
  ]) {
    assert.equal(
      endpointFormSchema.parse({ endpoint }).endpoint,
      'https://api.example.test/v1'
    )
  }
  assert.equal(
    endpointFormSchema.parse({ endpoint: 'https://api.example.test/gateway' })
      .endpoint,
    'https://api.example.test/gateway/v1'
  )
})

test('empty and malformed addresses explain the failure', () => {
  for (const [endpoint, message] of [
    [' ', 'marketHome.endpointRequired'],
    ['not-a-url', 'marketHome.endpointInvalid'],
  ]) {
    const result = endpointFormSchema.safeParse({ endpoint })
    assert.equal(result.success, false)
    if (!result.success) assert.equal(result.error.issues[0].message, message)
  }
})

test('permits HTTP only for local development hosts', () => {
  for (const endpoint of [
    'http://localhost:3000',
    'http://127.0.0.1:8080',
    'http://[::1]:8080',
  ]) {
    assert.equal(endpointFormSchema.safeParse({ endpoint }).success, true)
  }
  for (const endpoint of [
    'http://api.example.test',
    'http://localhost.example.test',
    'ftp://localhost',
  ]) {
    const result = endpointFormSchema.safeParse({ endpoint })
    assert.equal(result.success, false)
    if (!result.success)
      assert.equal(
        result.error.issues[0].message,
        'marketHome.endpointProtocol'
      )
  }
})

test('rejects embedded credentials, query parameters and fragments', () => {
  for (const endpoint of [
    'https://user:secret@api.example.test',
    'https://api.example.test/?token=example',
    'https://api.example.test/#example',
  ]) {
    const result = endpointFormSchema.safeParse({ endpoint })
    assert.equal(result.success, false)
    if (!result.success)
      assert.equal(result.error.issues[0].message, 'marketHome.endpointPrivate')
  }
})

test('all three examples use environment keys and the selected endpoint', () => {
  const endpoint = 'https://api.example.test/v1'
  for (const language of ['curl', 'python', 'javascript'] as const) {
    const source = createCodeExample(
      language,
      endpoint,
      'List available models'
    )
    assert.ok(source.includes(endpoint))
    assert.ok(source.includes('CODEGO_API_KEY'))
    assert.ok(source.includes('List available models'))
    assert.equal(source.includes(DEFAULT_ENDPOINT), false)
  }
})

test('shell metacharacters in a URL remain inside the quoted argument', () => {
  const source = createCodeExample(
    'curl',
    "https://api.example.test/team'$(id)/v1",
    'Example'
  )
  assert.ok(
    source.includes(
      "CODEGO_BASE_URL='https://api.example.test/team'\\''$(id)/v1'"
    )
  )
  assert.ok(source.includes('curl "$CODEGO_BASE_URL/models"'))
})

test('unconfigured examples use the base URL environment variable', () => {
  for (const language of ['curl', 'python', 'javascript'] as const) {
    const source = createCodeExample(language, DEFAULT_ENDPOINT, '')
    assert.ok(source.includes('CODEGO_BASE_URL'))
    assert.equal(source.includes('YOUR_CODEGO_HOST'), false)
  }
})

test('chat examples preserve selected model IDs and never include a credential', () => {
  const modelId = "model'with-quotes"
  for (const language of ['curl', 'python', 'javascript'] as const) {
    const source = createCodeExample(language, DEFAULT_ENDPOINT, '', modelId)
    assert.ok(source.includes('CODEGO_API_KEY'))
    assert.ok(source.includes('chat'))
    if (language === 'curl') assert.ok(source.includes("model'\\''with-quotes"))
    else assert.ok(source.includes(modelId))
    assert.equal(source.includes('sk-'), false)
  }
})
