import { afterEach, expect, test, vi } from 'vitest'
import { GatewayError, streamChatCompletion } from './stream'

afterEach(() => vi.unstubAllGlobals())

function mockStream(parts: string[]) {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue(
      new Response(
        new ReadableStream({
          start(controller) {
            for (const part of parts) controller.enqueue(new TextEncoder().encode(part))
            controller.close()
          },
        }),
      ),
    ),
  )
}

function handlers() {
  return { onDelta: vi.fn(), onUsage: vi.fn(), onDone: vi.fn() }
}

test('split streaming frames preserve content, usage and the request group', async () => {
  mockStream([
    ': ping\ndata: {"choices":[{"delta":{"content":"Hel',
    'lo"}}]}\n\ndata: {"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}\n\ndata: [DONE]\n',
  ])
  const callbacks = handlers()
  await streamChatCompletion(
    'sk-memory-only',
    { model: 'alpha' },
    callbacks,
    new AbortController().signal,
    'premium',
  )
  expect(callbacks.onDelta).toHaveBeenCalledWith('Hello', '')
  expect(callbacks.onUsage).toHaveBeenCalledWith({
    prompt_tokens: 5,
    completion_tokens: 2,
    total_tokens: 7,
  })
  expect(callbacks.onDone).toHaveBeenCalledOnce()
  expect(vi.mocked(fetch).mock.calls[0][1]?.headers).toMatchObject({ 'X-CodeGo-Group': 'premium' })
})

test('in-band stream errors reject after partial output and never signal completion', async () => {
  mockStream([
    'data: {"choices":[{"delta":{"content":"partial"}}]}\n\nevent: error\ndata: {"error":{"message":"upstream disconnected"}}\n\n',
  ])
  const callbacks = handlers()
  await expect(
    streamChatCompletion('sk-memory-only', {}, callbacks, new AbortController().signal),
  ).rejects.toEqual(new GatewayError('upstream disconnected', 502))
  expect(callbacks.onDelta).toHaveBeenCalledWith('partial', '')
  expect(callbacks.onDone).not.toHaveBeenCalled()
})

test('EOF without a terminal event cannot appear as a completed answer', async () => {
  mockStream(['data: {"choices":[{"delta":{"content":"partial"}}]}\n\n'])
  const callbacks = handlers()
  await expect(
    streamChatCompletion('sk-memory-only', {}, callbacks, new AbortController().signal),
  ).rejects.toThrow('响应提前中断，请重试。')
  expect(callbacks.onDone).not.toHaveBeenCalled()
})

test('malformed data is visible as an error', async () => {
  mockStream(['data: invalid-json\n\ndata: [DONE]\n\n'])
  await expect(
    streamChatCompletion('sk-memory-only', {}, handlers(), new AbortController().signal),
  ).rejects.toThrow('响应格式无效，请重试。')
})
