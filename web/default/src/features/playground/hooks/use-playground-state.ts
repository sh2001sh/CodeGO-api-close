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
import { useState, useCallback, useEffect } from 'react'
import { DEFAULT_CONFIG, DEFAULT_PARAMETER_ENABLED } from '../constants'
import { createSession, loadSessions, saveSessions } from '../lib/storage'
import type {
  Message,
  PlaygroundConfig,
  ParameterEnabled,
  ModelOption,
  GroupOption,
  PlaygroundSession,
} from '../types'

export function usePlaygroundState(accountId: number, initialGroup?: string) {
  const [sessions, setSessions] = useState(() => {
    const sessions = loadSessions(accountId)
    if (initialGroup)
      sessions[0] = {
        ...sessions[0],
        config: { ...sessions[0].config, group: initialGroup },
      }
    return sessions
  })
  const [activeId, setActiveId] = useState(() => sessions[0].id)
  const [models, setModels] = useState<ModelOption[]>([])
  const [groups, setGroups] = useState<GroupOption[]>([])
  const active =
    sessions.find((session) => session.id === activeId) || sessions[0]

  useEffect(() => {
    saveSessions(accountId, sessions)
  }, [accountId, sessions])

  const updateActive = useCallback(
    (updater: (session: PlaygroundSession) => PlaygroundSession) => {
      setSessions((previous) =>
        previous.map((session) =>
          session.id === activeId ? updater(session) : session
        )
      )
    },
    [activeId]
  )

  const updateConfig = useCallback(
    <K extends keyof PlaygroundConfig>(key: K, value: PlaygroundConfig[K]) => {
      updateActive((session) => ({
        ...session,
        config: { ...session.config, [key]: value },
      }))
    },
    [updateActive]
  )
  const updateParameterEnabled = useCallback(
    (key: keyof ParameterEnabled, value: boolean) => {
      updateActive((session) => ({
        ...session,
        parameterEnabled: { ...session.parameterEnabled, [key]: value },
      }))
    },
    [updateActive]
  )
  const updateMessages = useCallback(
    (updater: Message[] | ((previous: Message[]) => Message[])) => {
      updateActive((session) => {
        const messages =
          typeof updater === 'function' ? updater(session.messages) : updater
        const firstMessage = messages.find((message) => message.from === 'user')
          ?.versions[0]?.content
        return {
          ...session,
          messages,
          updatedAt: Date.now(),
          title: session.title || firstMessage?.slice(0, 60) || '',
        }
      })
    },
    [updateActive]
  )
  const updateDraft = useCallback(
    (draft: string) => {
      updateActive((session) => ({ ...session, draft }))
    },
    [updateActive]
  )
  const newSession = useCallback(() => {
    const session = createSession()
    setSessions((previous) => [session, ...previous])
    setActiveId(session.id)
  }, [])
  const renameSession = useCallback((id: string, title: string) => {
    setSessions((previous) =>
      previous.map((session) =>
        session.id === id ? { ...session, title } : session
      )
    )
  }, [])
  const deleteSession = useCallback(
    (id: string) => {
      const remaining = sessions.filter((session) => session.id !== id)
      const next = remaining.length ? remaining : [createSession()]
      setSessions(next)
      setActiveId((current) => (current === id ? next[0].id : current))
    },
    [sessions]
  )
  const clearSessions = useCallback(() => {
    const session = createSession()
    setSessions([session])
    setActiveId(session.id)
  }, [])
  const importMessages = useCallback(
    (messages: Message[]) => {
      const session = createSession()
      session.messages = messages
      session.title =
        messages
          .find((message) => message.from === 'user')
          ?.versions[0]?.content.slice(0, 60) || ''
      const next = [session, ...sessions]
      if (!saveSessions(accountId, next)) return false
      setSessions(next)
      setActiveId(session.id)
      return true
    },
    [accountId, sessions]
  )
  const resetConfig = useCallback(() => {
    updateActive((session) => ({
      ...session,
      config: { ...DEFAULT_CONFIG },
      parameterEnabled: { ...DEFAULT_PARAMETER_ENABLED },
    }))
  }, [updateActive])

  return {
    config: active.config,
    parameterEnabled: active.parameterEnabled,
    messages: active.messages,
    draft: active.draft,
    active,
    sessions,
    models,
    groups,
    setModels,
    setGroups,
    updateConfig,
    updateParameterEnabled,
    updateMessages,
    updateDraft,
    resetConfig,
    newSession,
    renameSession,
    deleteSession,
    clearSessions,
    importMessages,
    setActiveId,
  }
}
