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
import {
  useCallback,
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
} from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Download,
  History,
  SlidersHorizontal,
  ChevronDown,
  TriangleAlert,
  RefreshCw,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from '@/components/ui/dropdown-menu'
import { Sheet, SheetContent, SheetTitle } from '@/components/ui/sheet'
import { BoundedSelect } from '@/components/bounded-select'
import { getUserModels, getUserGroups } from './api'
import { PlaygroundChat } from './components/playground-chat'
import { PlaygroundInput } from './components/playground-input'
import { PlaygroundParameters } from './components/playground-parameters'
import {
  PlaygroundSessionDialog,
  type SessionDialogState,
} from './components/playground-session-dialog'
import { PlaygroundSessions } from './components/playground-sessions'
import { usePlaygroundState, useChatHandler } from './hooks'
import { createUserMessage, createLoadingAssistantMessage } from './lib'
import { exportSessions } from './lib/session-export'
import {
  hasLegacyMessages,
  readLegacyMessages,
  removeLegacyMessages,
} from './lib/storage'
import './playground.css'
import type { Message, PlaygroundSession } from './types'

function subscribeAccountChange(callback: () => void) {
  const handle = (event: StorageEvent) => {
    if (event.key === 'user' || event.key === null) callback()
  }
  window.addEventListener('storage', handle)
  return () => window.removeEventListener('storage', handle)
}

function getStoredAccount(): number | null {
  try {
    const stored = localStorage.getItem('user')
    return stored ? ((JSON.parse(stored) as { id?: number }).id ?? null) : null
  } catch {
    return null
  }
}

export function Playground(props: {
  initialModel?: string
  initialGroup?: string
}) {
  const user = useAuthStore((state) => state.auth.user)
  const storedAccount = useSyncExternalStore(
    subscribeAccountChange,
    getStoredAccount,
    () => null
  )
  if (!user || storedAccount !== user.id) return null
  return (
    <PlaygroundWorkspace
      key={user.id}
      accountId={user.id}
      account={user.display_name || user.username}
      initialModel={props.initialModel}
      initialGroup={props.initialGroup}
    />
  )
}

function PlaygroundWorkspace(props: {
  accountId: number
  account: string
  initialModel?: string
  initialGroup?: string
}) {
  const { t } = useTranslation()
  const state = usePlaygroundState(props.accountId, props.initialGroup)
  const {
    config,
    parameterEnabled,
    messages,
    models,
    groups,
    updateMessages,
    updateConfig,
    setModels,
    setGroups,
  } = state
  const appliedInitialGroup = useRef(props.initialGroup)
  const appliedInitialModel = useRef<string | undefined>(undefined)
  const [apiKey, setApiKey] = useState('')
  const [query, setQuery] = useState('')
  const [historyOpen, setHistoryOpen] = useState(false)
  const [parametersOpen, setParametersOpen] = useState(false)
  const [legacy, setLegacy] = useState(hasLegacyMessages)
  const [dialog, setDialog] = useState<SessionDialogState | null>(null)
  const [sessionName, setSessionName] = useState('')
  const [editingKey, setEditingKey] = useState<string | null>(null)
  const { sendChat, stopGeneration, isGenerating } = useChatHandler({
    config,
    parameterEnabled,
    onMessageUpdate: updateMessages,
    apiKey: apiKey.trim(),
  })

  const modelsQuery = useQuery({
    queryKey: ['playground-models', props.accountId, config.group],
    queryFn: () => getUserModels(config.group),
    enabled: Boolean(config.group),
  })
  const groupsQuery = useQuery({
    queryKey: ['playground-groups', props.accountId],
    queryFn: getUserGroups,
  })
  const modelsData = modelsQuery.data
  const groupsData = groupsQuery.data
  useEffect(() => {
    if (!modelsData) return
    setModels(modelsData)
    if (
      props.initialModel &&
      appliedInitialModel.current !== props.initialModel &&
      modelsData.some((model) => model.value === props.initialModel)
    ) {
      appliedInitialModel.current = props.initialModel
      updateConfig('model', props.initialModel)
      return
    }
    if (
      modelsData.length &&
      !modelsData.some((model) => model.value === config.model)
    )
      updateConfig('model', modelsData[0].value)
  }, [modelsData, config.model, props.initialModel, setModels, updateConfig])
  useEffect(() => {
    if (!groupsData) return
    setGroups(groupsData)
    if (
      props.initialGroup &&
      appliedInitialGroup.current !== props.initialGroup &&
      groupsData.some((group) => group.value === props.initialGroup)
    ) {
      appliedInitialGroup.current = props.initialGroup
      updateConfig('group', props.initialGroup)
      return
    }
    if (
      !groupsData.some((group) => group.value === config.group) &&
      groupsData.length
    )
      updateConfig(
        'group',
        groupsData.find((group) => group.value === 'auto')?.value ||
          groupsData.find((group) => group.value === 'default')?.value ||
          groupsData[0].value
      )
  }, [groupsData, config.group, props.initialGroup, setGroups, updateConfig])

  const startRequest = (nextMessages: Message[], attempts?: Message[]) => {
    if (
      isGenerating ||
      modelsQuery.isPending ||
      !models.some((model) => model.value === config.model)
    )
      return
    const response = {
      ...createLoadingAssistantMessage(),
      model: config.model,
      group: config.group,
      attempts,
    }
    const next = [...nextMessages, response]
    updateMessages(next)
    void sendChat(next)
  }
  const sendMessage = (text: string) =>
    startRequest([...messages, createUserMessage(text)])
  const regenerate = (message: Message) => {
    const index = messages.findIndex((item) => item.key === message.key)
    if (index >= 0)
      startRequest(messages.slice(0, index), [
        ...(message.attempts || []),
        { ...message, attempts: undefined },
      ])
  }
  const applyEdit = (content: string, submit: boolean) => {
    const index = messages.findIndex((message) => message.key === editingKey)
    if (index < 0 || isGenerating) return
    const edited = messages.map((message) =>
      message.key === editingKey
        ? { ...message, versions: [{ ...message.versions[0], content }] }
        : message
    )
    setEditingKey(null)
    if (submit && edited[index].from === 'user')
      startRequest(edited.slice(0, index + 1))
    else updateMessages(edited)
  }
  const editMessage = useCallback(
    (message: Message) => setEditingKey(message.key),
    []
  )
  const cancelEdit = useCallback((open: boolean) => {
    if (!open) setEditingKey(null)
  }, [])
  const selectSession = (id: string) => {
    state.setActiveId(id)
    setEditingKey(null)
    setHistoryOpen(false)
  }
  const newSession = () => {
    state.newSession()
    setEditingKey(null)
    setHistoryOpen(false)
    setQuery('')
  }
  const renameSession = (session: PlaygroundSession) => {
    setSessionName(session.title)
    setDialog({ action: 'rename', session })
  }
  const confirmDialog = () => {
    if (!dialog) return
    if (dialog.action === 'rename')
      state.renameSession(dialog.session.id, sessionName.trim())
    if (dialog.action === 'delete') state.deleteSession(dialog.session.id)
    if (dialog.action === 'clear') state.clearSessions()
    if (dialog.action === 'import') {
      try {
        if (!state.importMessages(readLegacyMessages())) return
        removeLegacyMessages()
        setLegacy(false)
      } catch {
        toast.error(t('会话导入失败'))
        return
      }
    }
    setEditingKey(null)
    setDialog(null)
  }
  const sessionsPanel = (
    <PlaygroundSessions
      sessions={state.sessions}
      activeId={state.active.id}
      account={props.account}
      query={query}
      onQueryChange={setQuery}
      onSelect={selectSession}
      onNew={newSession}
      onRename={renameSession}
      onDelete={(session) => setDialog({ action: 'delete', session })}
      onClear={() => setDialog({ action: 'clear' })}
      onExportAll={() => exportSessions(state.sessions, 'json')}
      hasLegacy={legacy}
      onImport={() => setDialog({ action: 'import' })}
      disabled={isGenerating}
    />
  )
  const parametersPanel = (
    <PlaygroundParameters
      config={config}
      enabled={parameterEnabled}
      disabled={isGenerating}
      onConfigChange={updateConfig}
      onEnabledChange={state.updateParameterEnabled}
      onReset={state.resetConfig}
      apiKey={apiKey}
      onApiKeyChange={setApiKey}
    />
  )
  const unavailable =
    modelsQuery.isPending ||
    modelsQuery.isError ||
    !models.some((model) => model.value === config.model)

  return (
    <div className='pg-workspace' data-generating={isGenerating}>
      <aside className='pg-history'>{sessionsPanel}</aside>
      <section className='pg-main' aria-label={t('Playground')}>
        <header className='pg-toolbar'>
          <button
            className='pg-icon-button pg-history-trigger'
            type='button'
            onClick={() => setHistoryOpen(true)}
            aria-label={t('会话')}
          >
            <History size={21} aria-hidden='true' />
          </button>
          <div className='pg-workspace-title'>
            <h1>{t('Playground')}</h1>
            <p>{state.active.title || t('新建会话')}</p>
          </div>
          <div className='pg-toolbar-actions'>
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <button
                    type='button'
                    className='pg-icon-button'
                    aria-label={t('Export')}
                  />
                }
              >
                <Download size={19} aria-hidden='true' />
              </DropdownMenuTrigger>
              <DropdownMenuContent align='end'>
                <DropdownMenuItem
                  onClick={() => exportSessions([state.active], 'json')}
                >
                  JSON
                </DropdownMenuItem>
                <DropdownMenuItem
                  onClick={() => exportSessions([state.active], 'md')}
                >
                  Markdown
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
            <button
              className='pg-icon-button pg-parameters-trigger'
              type='button'
              onClick={() => setParametersOpen(true)}
              aria-label={t('参数')}
            >
              <SlidersHorizontal size={21} aria-hidden='true' />
            </button>
          </div>
        </header>
        <div className='pg-model-bar'>
          <div className='pg-model-field'>
            <label htmlFor='pg-model'>{t('模型')}</label>
            <div>
              <BoundedSelect
                id='pg-model'
                options={models}
                aria-label={t('模型')}
                className='h-11 w-full border-0 bg-transparent px-0 font-mono shadow-none'
                placeholder={
                  modelsQuery.isPending ? t('Loading...') : t('无可用模型')
                }
                value={
                  models.some((model) => model.value === config.model)
                    ? config.model
                    : ''
                }
                disabled={
                  isGenerating || modelsQuery.isPending || !models.length
                }
                onValueChange={(value) => updateConfig('model', value)}
              />
            </div>
          </div>
          <div className='pg-model-field pg-group-field'>
            <label htmlFor='pg-group'>{t('分组')}</label>
            <div>
              <select
                id='pg-group'
                value={config.group}
                disabled={isGenerating || !groups.length}
                onChange={(event) => updateConfig('group', event.target.value)}
              >
                {!groups.length && (
                  <option value={config.group}>{config.group}</option>
                )}
                {groups.map((group) => (
                  <option key={group.value} value={group.value}>
                    {group.label} · ×{group.ratio}
                  </option>
                ))}
              </select>
              <ChevronDown size={15} aria-hidden='true' />
            </div>
          </div>
        </div>
        {(modelsQuery.isError || groupsQuery.isError) && (
          <div className='pg-load-error' role='alert'>
            <TriangleAlert size={19} aria-hidden='true' />
            <span>{t('加载失败')}</span>
            <button
              type='button'
              onClick={() => {
                void modelsQuery.refetch()
                void groupsQuery.refetch()
              }}
            >
              <RefreshCw size={17} aria-hidden='true' />
              {t('Retry')}
            </button>
          </div>
        )}
        <div className='pg-chat-region' aria-busy={isGenerating}>
          <PlaygroundChat
            key={state.active.id}
            messages={messages}
            onRegenerateMessage={regenerate}
            onEditMessage={editMessage}
            onDeleteMessage={(message) => {
              if (!isGenerating)
                updateMessages(
                  messages.filter((item) => item.key !== message.key)
                )
            }}
            isGenerating={isGenerating}
            editingKey={editingKey}
            onCancelEdit={cancelEdit}
            onSaveEdit={(content) => applyEdit(content, false)}
            onSaveEditAndSubmit={(content) => applyEdit(content, true)}
          />
        </div>
        <PlaygroundInput
          value={state.draft}
          onChange={state.updateDraft}
          disabled={unavailable}
          isGenerating={isGenerating}
          onSubmit={sendMessage}
          onStop={stopGeneration}
          empty={!messages.length}
        />
      </section>
      <aside className='pg-parameters'>{parametersPanel}</aside>
      <Sheet open={historyOpen} onOpenChange={setHistoryOpen}>
        <SheetContent
          side='left'
          className='pg-sheet pg-history-sheet w-full sm:max-w-sm'
        >
          <SheetTitle className='sr-only'>{t('会话')}</SheetTitle>
          {sessionsPanel}
        </SheetContent>
      </Sheet>
      <Sheet open={parametersOpen} onOpenChange={setParametersOpen}>
        <SheetContent
          side='right'
          className='pg-sheet pg-parameters-sheet w-full sm:max-w-lg'
        >
          <SheetTitle className='sr-only'>{t('参数')}</SheetTitle>
          {parametersPanel}
        </SheetContent>
      </Sheet>
      <PlaygroundSessionDialog
        state={dialog}
        name={sessionName}
        onNameChange={setSessionName}
        onClose={() => setDialog(null)}
        onConfirm={confirmDialog}
        count={state.sessions.length}
      />
    </div>
  )
}
