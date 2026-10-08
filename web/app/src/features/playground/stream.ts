// Minimal SSE-over-fetch client for the gateway's /v1/chat/completions stream.
// No new dependencies: uses fetch + ReadableStream directly (contract rule: no new deps).
import type { ChatCompletionChunk, UsageInfo } from './types'

export class GatewayError extends Error {
  constructor(
    message: string,
    public readonly status: number,
  ) {
    super(message)
  }
}

export interface StreamHandlers {
  onDelta: (text: string, reasoning: string) => void
  onUsage: (usage: UsageInfo) => void
  onDone: () => void
}

async function readErrorBody(response: Response): Promise<string> {
  try {
    const text = await response.text()
    const parsed = JSON.parse(text) as { error?: { message?: string } }
    return parsed.error?.message || `HTTP ${response.status}`
  } catch {
    return `HTTP ${response.status}`
  }
}

/** Streams a chat completion. Throws GatewayError on a non-2xx response. */
export async function streamChatCompletion(
  apiKey: string,
  body: Record<string, unknown>,
  handlers: StreamHandlers,
  signal: AbortSignal,
  group?: string,
): Promise<void> {
  const response = await fetch('/v1/chat/completions', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${apiKey}`,
      ...(group ? { 'X-CodeGo-Group': group } : {}),
    },
    body: JSON.stringify({ ...body, stream: true }),
    signal,
  })
  if (!response.ok || !response.body)
    throw new GatewayError(await readErrorBody(response), response.status)

  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  let complete = false
  try {
    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() ?? ''
      for (const line of lines) complete = processLine(line, handlers) || complete
    }
    buffer += decoder.decode()
    for (const line of buffer.split('\n')) complete = processLine(line, handlers) || complete
    if (!complete) throw new GatewayError('响应提前中断，请重试。', 502)
    handlers.onDone()
  } finally {
    reader.releaseLock()
  }
}

function processLine(line: string, handlers: StreamHandlers): boolean {
  const trimmed = line.trim()
  if (!trimmed.startsWith('data:')) return false
  const payload = trimmed.slice(5).trim()
  if (payload === '[DONE]') return true
  let chunk: ChatCompletionChunk & { error?: { message?: string; status?: number } }
  try {
    chunk = JSON.parse(payload)
  } catch {
    throw new GatewayError('响应格式无效，请重试。', 502)
  }
  if (chunk.error)
    throw new GatewayError(chunk.error.message ?? '请求失败', chunk.error.status ?? 502)
  const delta = chunk.choices?.[0]?.delta
  if (delta?.content || delta?.reasoning_content)
    handlers.onDelta(delta.content ?? '', delta.reasoning_content ?? '')
  if (chunk.usage) handlers.onUsage(chunk.usage)
  return Boolean(chunk.choices?.[0]?.finish_reason)
}
