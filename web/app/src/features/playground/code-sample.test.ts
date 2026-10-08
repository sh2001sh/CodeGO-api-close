import { expect, test } from 'vitest'
import { curlSample, pythonSample } from './code-sample'
import { defaultSettings, type ChatMessage } from './types'

const history: ChatMessage[] = [
  { id: 'one', role: 'user', content: "What's next?", reasoning: '', status: 'complete' },
]
const settings = { ...defaultSettings, model: 'alpha', group: 'premium' }

test('copied examples reproduce the selected group without embedding a key secret', () => {
  expect(curlSample('https://example.test', history, settings)).toContain(
    "-H 'X-CodeGo-Group: premium' \\\n",
  )
  expect(curlSample('https://example.test', history, settings)).toContain("What'\"'\"'s next?")
  expect(pythonSample('https://example.test', history, settings)).toContain(
    'extra_headers={"X-CodeGo-Group": "premium"}',
  )
  expect(pythonSample('https://example.test', history, settings)).toContain('payload = json.loads(')
  expect(pythonSample('https://example.test', history, settings)).toContain('api_key="$API_KEY"')
})

test('the key default sends no override header', () => {
  expect(curlSample('https://example.test', history, { ...settings, group: '' })).not.toContain(
    'X-CodeGo-Group',
  )
  expect(pythonSample('https://example.test', history, { ...settings, group: '' })).not.toContain(
    'extra_headers',
  )
})
