import assert from 'node:assert/strict'
import { beforeEach, test } from 'node:test'
import {
  DEFAULT_CONFIG,
  DEFAULT_PARAMETER_ENABLED,
  STORAGE_KEYS,
} from '../constants'
import {
  createLoadingAssistantMessage,
  createUserMessage,
  updateAssistantMessageWithError,
} from './message-utils'
import { buildChatCompletionPayload } from './payload-builder'
import {
  createSession,
  hasLegacyMessages,
  loadSessions,
  readLegacyMessages,
  saveSessions,
} from './storage'

const storage = new Map<string, string>()
Object.defineProperty(globalThis, 'localStorage', {
  configurable: true,
  value: {
    getItem: (key: string) => storage.get(key) ?? null,
    setItem: (key: string, value: string) => {
      storage.set(key, value)
    },
    removeItem: (key: string) => {
      storage.delete(key)
    },
  },
})
beforeEach(() => storage.clear())

test('sessions, drafts, parameters and messages stay within account ID namespaces', () => {
  const first = createSession()
  first.messages = [createUserMessage('account A private message')]
  first.draft = 'account A draft'
  first.config.temperature = 1.5
  assert.equal(saveSessions(11, [first]), true)
  assert.deepEqual(loadSessions(11)[0], first)
  const second = loadSessions(22)[0]
  assert.equal(second.messages.length, 0)
  assert.equal(second.draft, '')
  assert.notEqual(second.id, first.id)
  assert.equal(loadSessions(11)[0].draft, 'account A draft')
})

test('unowned legacy messages require an explicit import', () => {
  const legacy = [createUserMessage('unowned history')]
  storage.set(STORAGE_KEYS.MESSAGES, JSON.stringify(legacy))
  assert.equal(hasLegacyMessages(), true)
  assert.equal(loadSessions(11)[0].messages.length, 0)
  assert.deepEqual(readLegacyMessages(), legacy)
  assert.ok(storage.has(STORAGE_KEYS.MESSAGES))
})

test('invalid legacy content is rejected rather than assigned to an account', () => {
  storage.set(
    STORAGE_KEYS.MESSAGES,
    JSON.stringify([{ from: 'user', versions: null }])
  )
  assert.throws(() => readLegacyMessages(), /Invalid legacy messages/)
  assert.equal(loadSessions(11)[0].messages.length, 0)
})

test('an interrupted partial response loads as cancelled and preserves its content', () => {
  const session = createSession()
  const assistant = createLoadingAssistantMessage()
  assistant.status = 'streaming'
  assistant.versions[0].content = 'partial response'
  session.messages = [createUserMessage('hello'), assistant]
  saveSessions(11, [session])
  const restored = loadSessions(11)[0].messages[1]
  assert.equal(restored.status, 'cancelled')
  assert.equal(restored.versions[0].content, 'partial response')
  assert.equal(restored.usage, undefined)
})

test('requests carry the actual model, group, system prompt and enabled parameters', () => {
  const config = {
    ...DEFAULT_CONFIG,
    model: 'actual-model',
    group: 'actual-group',
    systemPrompt: 'Be precise.',
    max_tokens: 128,
  }
  const payload = buildChatCompletionPayload(
    [createUserMessage('Hello'), createLoadingAssistantMessage()],
    config,
    { ...DEFAULT_PARAMETER_ENABLED, max_tokens: true, temperature: false }
  )
  assert.equal(payload.model, 'actual-model')
  assert.equal(payload.group, 'actual-group')
  assert.equal(payload.max_tokens, 128)
  assert.equal(payload.temperature, undefined)
  assert.deepEqual(payload.messages, [
    { role: 'system', content: 'Be precise.' },
    { role: 'user', content: 'Hello' },
  ])
  assert.deepEqual(payload.stream_options, { include_usage: true })
})

test('non-streaming requests omit stream options and disabled parameters', () => {
  const payload = buildChatCompletionPayload(
    [createUserMessage('Hello')],
    { ...DEFAULT_CONFIG, stream: false },
    { ...DEFAULT_PARAMETER_ENABLED, seed: false }
  )
  assert.equal(payload.stream_options, undefined)
  assert.equal(payload.seed, undefined)
  assert.equal(payload.max_tokens, undefined)
})

test('a failed stream preserves partial output and the actual usage received', () => {
  const message = createLoadingAssistantMessage()
  message.versions[0].content = 'partial visible answer'
  message.usage = { prompt_tokens: 12, completion_tokens: 3, total_tokens: 15 }
  const failed = updateAssistantMessageWithError(
    [message],
    'Network interrupted'
  )[0]
  assert.equal(failed.versions[0].content, 'partial visible answer')
  assert.equal(failed.errorMessage, 'Network interrupted')
  assert.equal(failed.status, 'error')
  assert.equal(failed.usage?.total_tokens, 15)
})
