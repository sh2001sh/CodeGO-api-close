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
import { t } from 'i18next'
import { toast } from 'sonner'
import {
  DEFAULT_CONFIG,
  DEFAULT_PARAMETER_ENABLED,
  STORAGE_KEYS,
} from '../constants'
import type { Message, PlaygroundSession } from '../types'
import { sanitizeMessagesOnLoad } from './message-utils'

export function createSession(): PlaygroundSession {
  return {
    id: crypto.randomUUID(),
    title: '',
    updatedAt: Date.now(),
    messages: [],
    draft: '',
    config: { ...DEFAULT_CONFIG },
    parameterEnabled: { ...DEFAULT_PARAMETER_ENABLED },
  }
}

function sessionKey(accountId: number): string {
  return `codego:playground:v2:${accountId}:sessions`
}

function isMessage(value: unknown): value is Message {
  if (!value || typeof value !== 'object') return false
  const message = value as Message
  return (
    typeof message.key === 'string' &&
    ['user', 'assistant', 'system'].includes(message.from) &&
    Array.isArray(message.versions) &&
    message.versions.every(
      (version) =>
        version &&
        typeof version.id === 'string' &&
        typeof version.content === 'string'
    )
  )
}

export function loadSessions(accountId: number): PlaygroundSession[] {
  try {
    const saved = localStorage.getItem(sessionKey(accountId))
    if (!saved) return [createSession()]
    const parsed: unknown = JSON.parse(saved)
    if (!Array.isArray(parsed)) throw new Error('Invalid session storage')
    const sessions = parsed.map((value: unknown) => {
      if (!value || typeof value !== 'object')
        throw new Error('Invalid session')
      const session = value as PlaygroundSession
      if (
        typeof session.id !== 'string' ||
        typeof session.title !== 'string' ||
        !Array.isArray(session.messages) ||
        !session.messages.every(isMessage) ||
        typeof session.updatedAt !== 'number'
      )
        throw new Error('Invalid session')
      return {
        ...session,
        draft: typeof session.draft === 'string' ? session.draft : '',
        config: { ...DEFAULT_CONFIG, ...session.config },
        parameterEnabled: {
          ...DEFAULT_PARAMETER_ENABLED,
          ...session.parameterEnabled,
        },
        messages: sanitizeMessagesOnLoad(session.messages),
      }
    })
    return sessions.length ? sessions : [createSession()]
  } catch {
    toast.error(t('会话读取失败'))
    return [createSession()]
  }
}

export function saveSessions(
  accountId: number,
  sessions: PlaygroundSession[]
): boolean {
  try {
    localStorage.setItem(sessionKey(accountId), JSON.stringify(sessions))
    return true
  } catch {
    toast.error(t('会话保存失败；请导出后清理本地记录'), {
      id: 'playground-storage-error',
    })
    return false
  }
}

export function hasLegacyMessages(): boolean {
  try {
    return Boolean(localStorage.getItem(STORAGE_KEYS.MESSAGES))
  } catch {
    toast.error(t('会话读取失败'))
    return false
  }
}

export function readLegacyMessages(): Message[] {
  const parsed: unknown = JSON.parse(
    localStorage.getItem(STORAGE_KEYS.MESSAGES) || '[]'
  )
  if (!Array.isArray(parsed) || !parsed.every(isMessage))
    throw new Error('Invalid legacy messages')
  return sanitizeMessagesOnLoad(parsed)
}

export function removeLegacyMessages(): void {
  localStorage.removeItem(STORAGE_KEYS.MESSAGES)
}
