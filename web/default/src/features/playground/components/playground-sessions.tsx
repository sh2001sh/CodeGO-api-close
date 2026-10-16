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
import { useId } from 'react'
import {
  MessageSquare,
  Plus,
  Search,
  Pencil,
  Trash2,
  Download,
  Import,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { PlaygroundSession } from '../types'

interface PlaygroundSessionsProps {
  sessions: PlaygroundSession[]
  activeId: string
  query: string
  onQueryChange: (query: string) => void
  onSelect: (id: string) => void
  onNew: () => void
  onRename: (session: PlaygroundSession) => void
  onDelete: (session: PlaygroundSession) => void
  onClear: () => void
  onExportAll: () => void
  onImport: () => void
  hasLegacy: boolean
  disabled: boolean
  account: string
}

export function PlaygroundSessions(props: PlaygroundSessionsProps) {
  const { t, i18n } = useTranslation()
  const searchId = useId()
  const sessions = props.sessions
    .filter((session) =>
      session.title
        .toLocaleLowerCase()
        .includes(props.query.toLocaleLowerCase())
    )
    .sort((a, b) => b.updatedAt - a.updatedAt)
  return (
    <div className='pg-history-content'>
      <div className='pg-panel-heading'>
        <h2>{t('会话')}</h2>
        <span className='pg-count'>{props.sessions.length}</span>
      </div>
      <button
        className='pg-new-session'
        type='button'
        onClick={props.onNew}
        disabled={props.disabled}
      >
        <Plus size={19} aria-hidden='true' />
        {t('新建会话')}
      </button>
      <div className='pg-session-search'>
        <Search size={17} aria-hidden='true' />
        <label htmlFor={searchId} className='sr-only'>
          {t('搜索会话')}
        </label>
        <input
          id={searchId}
          value={props.query}
          onChange={(event) => props.onQueryChange(event.target.value)}
          aria-label={t('搜索会话')}
          placeholder={t('搜索会话')}
        />
      </div>
      <nav className='pg-session-list' aria-label={t('会话')}>
        {sessions.map((session) => (
          <div
            className='pg-session'
            data-active={session.id === props.activeId}
            key={session.id}
          >
            <button
              className='pg-session-select'
              type='button'
              onClick={() => props.onSelect(session.id)}
              disabled={props.disabled}
              aria-current={session.id === props.activeId ? 'page' : undefined}
            >
              <MessageSquare size={17} aria-hidden='true' />
              <span>
                <span
                  className='pg-session-title'
                  title={session.title || t('新建会话')}
                >
                  {session.title || t('新建会话')}
                </span>
                <time dateTime={new Date(session.updatedAt).toISOString()}>
                  {new Intl.DateTimeFormat(i18n.language, {
                    month: 'short',
                    day: 'numeric',
                    hour: '2-digit',
                    minute: '2-digit',
                  }).format(session.updatedAt)}
                </time>
              </span>
            </button>
            <div className='pg-session-actions'>
              <button
                type='button'
                onClick={() => props.onRename(session)}
                disabled={props.disabled}
                aria-label={`${t('Rename')} ${session.title || t('会话')}`}
              >
                <Pencil size={15} aria-hidden='true' />
              </button>
              <button
                type='button'
                onClick={() => props.onDelete(session)}
                disabled={props.disabled}
                aria-label={`${t('Delete')} ${session.title || t('会话')}`}
              >
                <Trash2 size={15} aria-hidden='true' />
              </button>
            </div>
          </div>
        ))}
        {sessions.length === 0 && (
          <div className='pg-no-sessions'>
            <span>{t('无匹配会话')}</span>
            <button type='button' onClick={() => props.onQueryChange('')}>
              {t('Clear')}
            </button>
          </div>
        )}
      </nav>
      <div className='pg-history-footer'>
        {props.hasLegacy && (
          <button
            type='button'
            onClick={props.onImport}
            disabled={props.disabled}
          >
            <Import size={17} aria-hidden='true' />
            {t('导入旧会话')}
          </button>
        )}
        <button type='button' onClick={props.onExportAll}>
          <Download size={17} aria-hidden='true' />
          {t('导出全部')}
        </button>
        <button type='button' onClick={props.onClear} disabled={props.disabled}>
          <Trash2 size={17} aria-hidden='true' />
          {t('清空本地')}
        </button>
        <details className='pg-session-id'>
          <summary>{t('会话 ID')}</summary>
          <code>{props.activeId}</code>
        </details>
        <span className='pg-account'>{props.account}</span>
      </div>
    </div>
  )
}
