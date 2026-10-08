import { useCallback, useEffect, useRef, useState } from 'react'
import { streamChatCompletion, GatewayError } from './stream'
import {
  defaultSettings,
  type ChatMessage,
  type ChatRole,
  type PlaygroundSettings,
  type UsageInfo,
} from './types'

function newID(): string {
  return crypto.randomUUID()
}

function settingsError(apiKey: string | null, settings: PlaygroundSettings): string | null {
  if (!apiKey) return '请先选择并使用一个 API Key'
  if (!settings.model.trim()) return '请选择模型'
  const temperature = Number(settings.temperature)
  if (
    settings.temperature.trim() &&
    (!Number.isFinite(temperature) || temperature < 0 || temperature > 2)
  )
    return '温度须为 0 到 2 之间的数值'
  const maximum = Number(settings.maxTokens)
  if (settings.maxTokens.trim() && (!Number.isSafeInteger(maximum) || maximum < 1))
    return '最大输出长度须为正整数'
  return null
}

function requestBody(
  history: ChatMessage[],
  settings: PlaygroundSettings,
): Record<string, unknown> {
  const messages: { role: ChatRole; content: string }[] = []
  if (settings.systemPrompt.trim())
    messages.push({ role: 'system', content: settings.systemPrompt })
  for (const message of history) {
    if (message.role === 'system') continue
    messages.push({ role: message.role, content: message.content })
  }
  const body: Record<string, unknown> = { model: settings.model, messages }
  if (settings.temperature.trim()) body.temperature = Number(settings.temperature)
  if (settings.maxTokens.trim()) body.max_tokens = Number(settings.maxTokens)
  return body
}

/**
 * Owns the message thread and drives streaming requests against the gateway.
 * The API key secret is passed in by the caller and kept in memory only.
 */
export function usePlayground(apiKey: string | null) {
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [settings, setSettings] = useState<PlaygroundSettings>(defaultSettings)
  const [usage, setUsage] = useState<UsageInfo | null>(null)
  const [error, setError] = useState<Error | null>(null)
  const [sending, setSending] = useState(false)
  const controllerRef = useRef<AbortController | null>(null)
  useEffect(() => () => controllerRef.current?.abort(), [])

  const run = useCallback(
    async (history: ChatMessage[], assistantID: string) => {
      if (!apiKey) {
        setError(new Error('请先选择并使用一个 API Key'))
        return
      }
      const controller = new AbortController()
      controllerRef.current = controller
      setSending(true)
      setError(null)
      setUsage(null)
      try {
        await streamChatCompletion(
          apiKey,
          requestBody(history, settings),
          {
            onDelta: (text, reasoning) => {
              setMessages((current) =>
                current.map((message) =>
                  message.id === assistantID
                    ? {
                        ...message,
                        content: message.content + text,
                        reasoning: message.reasoning + reasoning,
                      }
                    : message,
                ),
              )
            },
            onUsage: setUsage,
            onDone: () => {
              setMessages((current) =>
                current.map((message) =>
                  message.id === assistantID ? { ...message, status: 'complete' } : message,
                ),
              )
            },
          },
          controller.signal,
          settings.group,
        )
      } catch (cause) {
        if (controller.signal.aborted) {
          setMessages((current) =>
            current.map((message) =>
              message.id === assistantID ? { ...message, status: 'complete' } : message,
            ),
          )
          return
        }
        const failure =
          cause instanceof GatewayError
            ? cause
            : cause instanceof Error
              ? cause
              : new Error('请求失败')
        setMessages((current) =>
          current.map((message) =>
            message.id === assistantID
              ? { ...message, status: 'error', errorMessage: failure.message }
              : message,
          ),
        )
        setError(failure)
      } finally {
        setSending(false)
        controllerRef.current = null
      }
    },
    [apiKey, settings],
  )

  const send = useCallback(
    (content: string) => {
      if (!content.trim() || sending) return false
      const invalid = settingsError(apiKey, settings)
      if (invalid) {
        setError(new Error(invalid))
        return false
      }
      const userMessage: ChatMessage = {
        id: newID(),
        role: 'user',
        content,
        reasoning: '',
        status: 'complete',
      }
      const assistantMessage: ChatMessage = {
        id: newID(),
        role: 'assistant',
        content: '',
        reasoning: '',
        status: 'streaming',
      }
      const history = [...messages, userMessage]
      setMessages([...history, assistantMessage])
      void run(history, assistantMessage.id)
      return true
    },
    [apiKey, messages, run, sending, settings],
  )

  const regenerate = useCallback(() => {
    if (sending) return
    const invalid = settingsError(apiKey, settings)
    if (invalid) {
      setError(new Error(invalid))
      return
    }
    let lastUserIndex = -1
    for (let index = messages.length - 1; index >= 0; index -= 1) {
      if (messages[index].role === 'user') {
        lastUserIndex = index
        break
      }
    }
    if (lastUserIndex === -1) return
    const history = messages.slice(0, lastUserIndex + 1)
    const assistantMessage: ChatMessage = {
      id: newID(),
      role: 'assistant',
      content: '',
      reasoning: '',
      status: 'streaming',
    }
    setMessages([...history, assistantMessage])
    void run(history, assistantMessage.id)
  }, [apiKey, messages, run, sending, settings])

  const stop = useCallback(() => controllerRef.current?.abort(), [])

  const clear = useCallback(() => {
    if (sending) return
    setMessages([])
    setUsage(null)
    setError(null)
  }, [sending])

  return {
    messages,
    settings,
    setSettings,
    usage,
    error,
    sending,
    send,
    stop,
    regenerate,
    clear,
    requestBody: (history: ChatMessage[]) => requestBody(history, settings),
  }
}
