import { Dialog as BaseDialog } from '@base-ui/react/dialog'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { CornerDownLeft, Search } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { useTranslation } from '../../lib/i18n'
import { publicNav, resourceNav, visibleGroups } from '../../lib/navigation'

export type PaletteAction = {
  id: string
  label: string
  icon: LucideIcon
  keywords?: string
  run: () => void
}

type Entry = {
  id: string
  group: string
  label: string
  hint?: string
  icon: LucideIcon
  haystack: string
  run: () => void
}

function score(entry: Entry, query: string): number {
  if (!query) return 1
  const label = entry.label.toLowerCase()
  if (label.startsWith(query)) return 3
  if (label.includes(query)) return 2
  return query.split(/\s+/).every((part) => entry.haystack.includes(part)) ? 1 : 0
}

/**
 * Global ⌘K / Ctrl+K navigator over every page the user can access, so features stay
 * findable even when the sidebar is collapsed or the user does not know the section name.
 */
export function CommandPalette(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
  role?: string
  actions: readonly PaletteAction[]
  /** Anonymous visitor: console pages stay searchable but are grouped as needing sign-in. */
  publicOnly?: boolean
}) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [query, setQuery] = useState('')
  const [active, setActive] = useState(0)
  const listRef = useRef<HTMLDivElement>(null)

  const entries = useMemo<Entry[]>(() => {
    const go = (to: string) => () => void navigate({ to })
    const pages = visibleGroups(props.role).flatMap((group) =>
      group.items.map((item) => ({
        id: item.to,
        group: props.publicOnly ? t('需要登录') : t('页面'),
        label: t(item.label),
        hint: t(group.title),
        icon: item.icon,
        haystack: `${t(item.label)} ${item.label} ${item.keywords ?? ''} ${item.to}`.toLowerCase(),
        run: go(item.to),
      })),
    )
    const site = [...publicNav, ...resourceNav]
      .filter((item) => !pages.some((page) => page.id === item.to))
      .map((item) => ({
        id: `public:${item.to}`,
        group: t('站点'),
        label: t(item.label),
        icon: item.icon,
        haystack: `${t(item.label)} ${item.label} ${item.keywords ?? ''}`.toLowerCase(),
        run: go(item.to),
      }))
    const actions = props.actions.map((action) => ({
      id: `action:${action.id}`,
      group: t('操作'),
      label: t(action.label),
      icon: action.icon,
      haystack: `${t(action.label)} ${action.label} ${action.keywords ?? ''}`.toLowerCase(),
      run: action.run,
    }))
    return props.publicOnly ? [...site, ...actions, ...pages] : [...pages, ...site, ...actions]
  }, [navigate, props.actions, props.publicOnly, props.role, t])

  const normalized = query.trim().toLowerCase()
  const results = useMemo(
    () =>
      entries
        .map((entry) => ({ entry, score: score(entry, normalized) }))
        .filter((item) => item.score > 0)
        .sort((a, b) => b.score - a.score)
        .map((item) => item.entry),
    [entries, normalized],
  )

  useEffect(() => setActive(0), [normalized, props.open])
  useEffect(() => {
    if (!props.open) setQuery('')
  }, [props.open])
  useEffect(() => {
    listRef.current?.querySelector(`[data-index="${active}"]`)?.scrollIntoView({ block: 'nearest' })
  }, [active])

  const select = (entry: Entry | undefined) => {
    if (!entry) return
    props.onOpenChange(false)
    entry.run()
  }

  let lastGroup = ''
  return (
    <BaseDialog.Root open={props.open} onOpenChange={(open) => props.onOpenChange(open)}>
      <BaseDialog.Portal>
        <BaseDialog.Backdrop className="overlay-backdrop" />
        <BaseDialog.Popup className="command-popup" aria-label={t('搜索页面和功能')}>
          <BaseDialog.Title className="sr-only">{t('搜索页面和功能')}</BaseDialog.Title>
          <div className="command-input">
            <Search size={18} aria-hidden />
            <input
              autoFocus
              role="combobox"
              aria-expanded
              aria-controls="command-results"
              aria-activedescendant={results[active] ? `cmd-${active}` : undefined}
              aria-autocomplete="list"
              placeholder={t('搜索页面、功能或设置…')}
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === 'ArrowDown') {
                  event.preventDefault()
                  setActive((index) => Math.min(results.length - 1, index + 1))
                } else if (event.key === 'ArrowUp') {
                  event.preventDefault()
                  setActive((index) => Math.max(0, index - 1))
                } else if (event.key === 'Enter') {
                  event.preventDefault()
                  select(results[active])
                }
              }}
            />
          </div>
          <div className="command-list" id="command-results" role="listbox" ref={listRef}>
            {results.length === 0 && (
              <div className="command-empty">{t('没有匹配的页面或功能')}</div>
            )}
            {results.map((entry, index) => {
              const heading = entry.group !== lastGroup ? entry.group : null
              lastGroup = entry.group
              return (
                <div key={entry.id}>
                  {heading && <div className="command-group-title">{heading}</div>}
                  <button
                    type="button"
                    id={`cmd-${index}`}
                    role="option"
                    data-index={index}
                    aria-selected={index === active}
                    className="command-item"
                    onMouseMove={() => setActive(index)}
                    onClick={() => select(entry)}
                    tabIndex={-1}
                  >
                    <entry.icon size={16} aria-hidden />
                    {entry.label}
                    {entry.hint && <small>{entry.hint}</small>}
                  </button>
                </div>
              )
            })}
          </div>
          <div className="command-footer" aria-hidden>
            <span>
              <kbd>↑</kbd>
              <kbd>↓</kbd> {t('选择')}
            </span>
            <span>
              <kbd>
                <CornerDownLeft size={10} />
              </kbd>{' '}
              {t('打开')}
            </span>
            <span>
              <kbd>Esc</kbd> {t('关闭')}
            </span>
          </div>
        </BaseDialog.Popup>
      </BaseDialog.Portal>
    </BaseDialog.Root>
  )
}
