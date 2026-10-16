/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useCallback, useEffect, useRef, useState } from 'react'
import { useAuthStore } from '@/stores/auth-store'
import { getCommonHeaders } from '@/lib/api'
import { API_ENDPOINTS, ERROR_MESSAGES } from '../constants'
import type {
  ChatCompletionRequest,
  ChatCompletionChunk,
  Message,
} from '../types'

export function useStreamRequest() {
  const controllerRef = useRef<AbortController | null>(null)
  const [isStreaming, setIsStreaming] = useState(false)
  useEffect(() => () => controllerRef.current?.abort(), [])

  const sendStreamRequest = useCallback(
    async (
      payload: ChatCompletionRequest,
      onUpdate: (type: 'reasoning' | 'content', chunk: string) => void,
      onComplete: () => void,
      onError: (error: string, errorCode?: string) => void,
      onMetadata: (metadata: Partial<Message>) => void,
      apiKey = ''
    ) => {
      controllerRef.current?.abort()
      const controller = new AbortController()
      controllerRef.current = controller
      setIsStreaming(true)
      try {
        const response = await fetch(
          apiKey ? '/v1/chat/completions' : API_ENDPOINTS.CHAT_COMPLETIONS,
          {
            method: 'POST',
            credentials: 'same-origin',
            signal: controller.signal,
            headers: {
              ...getCommonHeaders(),
              'Content-Type': 'application/json',
              ...(apiKey ? { Authorization: `Bearer ${apiKey}` } : {}),
            },
            body: JSON.stringify(payload),
          }
        )
        if (!response.ok) {
          if (response.status === 401 && !apiKey)
            useAuthStore.getState().auth.reset()
          const body = await response.text()
          let message = `HTTP ${response.status}`
          let code: string | undefined
          try {
            const data = JSON.parse(body) as {
              message?: string
              error?: { message?: string; code?: string }
            }
            message = data.error?.message || data.message || message
            code = data.error?.code
          } catch {
            message = body || message
          }
          if (!controller.signal.aborted) onError(message, code)
          return
        }
        const requestId = response.headers.get('x-request-id')
        if (requestId) onMetadata({ requestId })
        if (!response.body) throw new Error(ERROR_MESSAGES.CONNECTION_CLOSED)
        const reader = response.body.getReader()
        const decoder = new TextDecoder()
        let buffer = ''
        let finished = false
        let terminalChunk = false
        const processEvent = (event: string) => {
          const data = event
            .split(/\r?\n/)
            .filter((line) => line.startsWith('data:'))
            .map((line) => line.slice(5).trimStart())
            .join('\n')
          if (!data) return
          if (data === '[DONE]') {
            finished = true
            return
          }
          const chunk = JSON.parse(data) as ChatCompletionChunk & {
            error?: { message?: string; code?: string }
          }
          if (chunk.error)
            throw new Error(
              chunk.error.message || ERROR_MESSAGES.API_REQUEST_ERROR
            )
          if (chunk.id || chunk.usage || chunk.model)
            onMetadata({
              ...(chunk.id ? { requestId: chunk.id } : {}),
              ...(chunk.model ? { model: chunk.model } : {}),
              ...(chunk.usage ? { usage: chunk.usage } : {}),
            })
          if (
            chunk.usage ||
            chunk.choices?.some((choice) => choice.finish_reason)
          )
            terminalChunk = true
          const delta = chunk.choices?.[0]?.delta
          if (delta?.reasoning_content)
            onUpdate('reasoning', delta.reasoning_content)
          if (delta?.content) onUpdate('content', delta.content)
          if (delta?.tool_calls) onMetadata({ toolCalls: delta.tool_calls })
        }
        while (!finished) {
          const result = await reader.read()
          if (controller.signal.aborted) return
          buffer += decoder.decode(result.value, { stream: !result.done })
          let boundary = buffer.match(/\r?\n\r?\n/)
          while (boundary?.index !== undefined) {
            const event = buffer.slice(0, boundary.index)
            buffer = buffer.slice(boundary.index + boundary[0].length)
            processEvent(event)
            boundary = buffer.match(/\r?\n\r?\n/)
          }
          if (result.done) {
            if (buffer.trim()) processEvent(buffer)
            if (!finished && !terminalChunk)
              throw new Error(ERROR_MESSAGES.INTERRUPTED)
            break
          }
        }
        await reader.cancel()
        if (!controller.signal.aborted) onComplete()
      } catch (error) {
        if (!controller.signal.aborted)
          onError(
            error instanceof Error
              ? error.message
              : ERROR_MESSAGES.NETWORK_ERROR
          )
      } finally {
        if (controllerRef.current === controller) {
          controllerRef.current = null
          setIsStreaming(false)
        }
      }
    },
    []
  )

  const stopStream = useCallback(() => {
    controllerRef.current?.abort()
    controllerRef.current = null
    setIsStreaming(false)
  }, [])
  return { sendStreamRequest, stopStream, isStreaming }
}
