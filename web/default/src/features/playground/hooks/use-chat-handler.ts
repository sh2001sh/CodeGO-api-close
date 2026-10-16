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
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { sendChatCompletion } from '../api'
import { ERROR_MESSAGES } from '../constants'
import {
  buildChatCompletionPayload,
  updateAssistantMessageWithError,
  updateLastAssistantMessage,
  processStreamingContent,
  finalizeMessage,
} from '../lib'
import {
  maybeShowNightWorkshopToast,
  recordWorkshopCallFailure,
  recordWorkshopCallSuccess,
} from '../lib/workshop-feedback'
import type {
  Message,
  PlaygroundConfig,
  ParameterEnabled,
  ToolCall,
} from '../types'
import { useStreamRequest } from './use-stream-request'

interface UseChatHandlerOptions {
  config: PlaygroundConfig
  parameterEnabled: ParameterEnabled
  onMessageUpdate: (updater: (previous: Message[]) => Message[]) => void
  apiKey?: string
}

function mergeToolCalls(
  previous: ToolCall[] = [],
  updates: ToolCall[]
): ToolCall[] {
  const calls = previous.map((call) => ({
    ...call,
    function: { ...call.function },
  }))
  for (const update of updates) {
    const index = update.index ?? 0
    const current = calls[index] || {}
    calls[index] = {
      ...current,
      ...update,
      function: {
        name: update.function?.name || current.function?.name,
        arguments:
          (current.function?.arguments || '') +
          (update.function?.arguments || ''),
      },
    }
  }
  return calls
}

export function useChatHandler(options: UseChatHandlerOptions) {
  const { t } = useTranslation()
  const { sendStreamRequest, stopStream, isStreaming } = useStreamRequest()
  const [isRequesting, setIsRequesting] = useState(false)
  const requestRef = useRef<AbortController | null>(null)
  const busyRef = useRef(false)
  const requestGeneration = useRef(0)
  const onMessageUpdate = options.onMessageUpdate
  const config = options.config
  const parameterEnabled = options.parameterEnabled
  const apiKey = options.apiKey
  useEffect(() => () => requestRef.current?.abort(), [])

  const updateMetadata = useCallback(
    (metadata: Partial<Message>) => {
      onMessageUpdate((previous) =>
        updateLastAssistantMessage(previous, (message) => ({
          ...message,
          ...metadata,
          ...(metadata.toolCalls
            ? {
                toolCalls: mergeToolCalls(
                  message.toolCalls,
                  metadata.toolCalls
                ),
              }
            : {}),
        }))
      )
    },
    [onMessageUpdate]
  )

  const sendChat = useCallback(
    async (messages: Message[]) => {
      if (busyRef.current) return
      busyRef.current = true
      const generation = ++requestGeneration.current
      maybeShowNightWorkshopToast()
      const payload = buildChatCompletionPayload(
        messages,
        config,
        parameterEnabled
      )
      const onError = (error: string, errorCode?: string) => {
        recordWorkshopCallFailure()
        toast.error(t(error))
        onMessageUpdate((previous) =>
          updateAssistantMessageWithError(previous, t(error), errorCode)
        )
      }
      const onComplete = () => {
        recordWorkshopCallSuccess()
        onMessageUpdate((previous) =>
          updateLastAssistantMessage(previous, (message) => ({
            ...finalizeMessage(message),
            status: 'complete',
          }))
        )
      }
      try {
        if (config.stream) {
          await sendStreamRequest(
            payload,
            (type, chunk) => {
              onMessageUpdate((previous) =>
                updateLastAssistantMessage(previous, (message) => {
                  if (type === 'reasoning')
                    return {
                      ...message,
                      reasoning: {
                        content: (message.reasoning?.content || '') + chunk,
                        duration: 0,
                      },
                      isReasoningStreaming: true,
                      status: 'streaming',
                    }
                  return {
                    ...processStreamingContent(message, chunk),
                    status: 'streaming',
                  }
                })
              )
            },
            onComplete,
            onError,
            updateMetadata,
            apiKey
          )
        } else {
          const controller = new AbortController()
          requestRef.current = controller
          setIsRequesting(true)
          try {
            const response = await sendChatCompletion(payload, {
              signal: controller.signal,
              apiKey: apiKey,
            })
            if (controller.signal.aborted) return
            const choice = response.choices?.[0]
            if (!choice) throw new Error(ERROR_MESSAGES.PARSE_ERROR)
            onMessageUpdate((previous) =>
              updateLastAssistantMessage(previous, (message) => ({
                ...finalizeMessage(
                  {
                    ...message,
                    versions: [
                      {
                        ...message.versions[0],
                        content: choice.message?.content || '',
                      },
                    ],
                  },
                  choice.message?.reasoning_content
                ),
                status: 'complete',
                requestId: response.id,
                model: response.model || message.model,
                usage: response.usage,
                toolCalls: choice.message?.tool_calls,
              }))
            )
            recordWorkshopCallSuccess()
          } catch (error) {
            if (controller.signal.aborted) return
            const failure = error as {
              response?: {
                status?: number
                data?: {
                  message?: string
                  error?: { message?: string; code?: string }
                }
              }
              message?: string
            }
            if (failure.response?.status === 401 && !apiKey)
              useAuthStore.getState().auth.reset()
            onError(
              failure.response?.data?.error?.message ||
                failure.response?.data?.message ||
                failure.message ||
                ERROR_MESSAGES.API_REQUEST_ERROR,
              failure.response?.data?.error?.code
            )
          } finally {
            if (requestRef.current === controller) {
              requestRef.current = null
              setIsRequesting(false)
            }
          }
        }
      } finally {
        if (generation === requestGeneration.current) busyRef.current = false
      }
    },
    [
      config,
      parameterEnabled,
      onMessageUpdate,
      apiKey,
      sendStreamRequest,
      updateMetadata,
      t,
    ]
  )

  const stopGeneration = useCallback(() => {
    requestGeneration.current += 1
    stopStream()
    requestRef.current?.abort()
    requestRef.current = null
    busyRef.current = false
    setIsRequesting(false)
    onMessageUpdate((previous) =>
      updateLastAssistantMessage(previous, (message) =>
        message.status === 'loading' || message.status === 'streaming'
          ? { ...finalizeMessage(message), status: 'cancelled' }
          : message
      )
    )
  }, [stopStream, onMessageUpdate])

  return { sendChat, stopGeneration, isGenerating: isStreaming || isRequesting }
}
