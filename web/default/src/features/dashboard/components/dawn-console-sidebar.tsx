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
import { useMemo, useState } from 'react'
import { Link, useLocation } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { ChevronDown, ExternalLink, Loader2 } from 'lucide-react'
import { useAuthStore } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'
import { cn } from '@/lib/utils'
import { useSidebarData } from '@/hooks/use-sidebar-data'
import { useSidebarConfig } from '@/hooks/use-sidebar-config'
import { useChatPresets } from '@/features/chat/hooks/use-chat-presets'
import { fetchActiveChatKey } from '@/features/chat/hooks/use-active-chat-key'
import {
  chatLinkRequiresApiKey,
  resolveChatUrl,
  type ChatPreset,
} from '@/features/chat/lib/chat-links'
import { getNavGroupsForPath } from '@/components/layout/lib/workspace-registry'
import { checkIsActive } from '@/components/layout/lib/url-utils'
import type { NavGroup, NavItem } from '@/components/layout/types'

interface SidebarGroupState {
  [key: string]: boolean
}

/**
 * Dawn 控制台侧栏：复用真实导航数据 + 后端 SidebarModules 权限过滤，
 * 同时保留管理员导航与聊天预设入口。
 */
export function DawnConsoleSidebar(props: {
  open?: boolean
  onNavigate?: () => void
}) {
  const { t } = useTranslation()
  const location = useLocation()
  const userRole = useAuthStore((state) => state.auth.user?.role)
  const sidebarData = useSidebarData()

  const allNavGroups =
    getNavGroupsForPath(location.pathname, t) ?? sidebarData.navGroups
  const configFilteredNavGroups = useSidebarConfig(allNavGroups)

  const navGroups = useMemo(() => {
    const isAdmin = Boolean(userRole && userRole >= ROLE.ADMIN)
    return configFilteredNavGroups.filter((group) => {
      if (group.id === 'admin') return isAdmin
      return true
    })
  }, [configFilteredNavGroups, userRole])

  const [openGroups, setOpenGroups] = useState<SidebarGroupState>({})

  const isActive = (item: NavItem) => checkIsActive(location.href, item)

  return (
    <aside
      className={`dawn-console-sidebar${props.open ? ' open' : ''}`}
      onClickCapture={(event) => {
        if ((event.target as HTMLElement).closest('a')) props.onNavigate?.()
      }}
    >
      {navGroups.map((group) => (
        <SidebarGroup
          key={group.id || group.title}
          group={group}
          openGroups={openGroups}
          onToggle={(key) =>
            setOpenGroups((current) => ({ ...current, [key]: !current[key] }))
          }
          isActive={isActive}
        />
      ))}
    </aside>
  )
}

function SidebarGroup(props: {
  group: NavGroup
  openGroups: SidebarGroupState
  onToggle: (key: string) => void
  isActive: (item: NavItem) => boolean
}) {
  const { group } = props

  const singleCollapsible =
    group.items.length === 1 &&
    'items' in group.items[0] &&
    group.items[0].title === group.title

  return (
    <div>
      {singleCollapsible ? null : <div className='sgroup'>{group.title}</div>}
      {group.items.map((item) => (
        <SidebarItem
          key={item.title}
          item={item}
          open={Boolean(props.openGroups[item.title])}
          onToggle={() => props.onToggle(item.title)}
          isActive={props.isActive}
        />
      ))}
    </div>
  )
}

function SidebarItem(props: {
  item: NavItem
  open: boolean
  onToggle: () => void
  isActive: (item: NavItem) => boolean
}) {
  const { item } = props

  if ('type' in item && item.type === 'chat-presets') {
    return <ChatPresetsNav item={item} />
  }

  if ('items' in item && item.items?.length) {
    return (
      <div>
        <button className='sgroup tog' onClick={props.onToggle}>
          <span style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
            {item.icon ? <item.icon size={14} /> : null}
            {item.title}
          </span>
          <ChevronDown
            size={14}
            style={{
              transform: props.open ? 'rotate(180deg)' : 'none',
              transition: 'transform 0.2s',
            }}
          />
        </button>
        {props.open && (
          <div className='moreitems'>
            {item.items.map((subItem) => {
              const Icon = subItem.icon
              return (
                <Link
                  key={subItem.title}
                  to={subItem.url}
                  className={cn('sitem', props.isActive(subItem) && 'on')}
                  style={{ paddingLeft: 30 }}
                >
                  {Icon ? <Icon size={15} /> : null}
                  <span>{subItem.title}</span>
                  {subItem.badge ? (
                    <span className='badge'>{subItem.badge}</span>
                  ) : null}
                </Link>
              )
            })}
          </div>
        )}
      </div>
    )
  }

  if ('url' in item && item.url) {
    const Icon = item.icon
    return (
      <Link
        to={item.url}
        className={cn('sitem', props.isActive(item) && 'on')}
      >
        {Icon ? <Icon size={16} /> : null}
        <span>{item.title}</span>
        {item.badge ? <span className='badge'>{item.badge}</span> : null}
      </Link>
    )
  }

  return null
}

function ChatPresetsNav({ item }: { item: NavItem }) {
  const { t } = useTranslation()
  const location = useLocation()
  const { chatPresets, serverAddress } = useChatPresets()
  const [open, setOpen] = useState(false)
  const [loadingId, setLoadingId] = useState<string | null>(null)

  const presets = useMemo(
    () => chatPresets.filter((preset) => preset.type !== 'fluent'),
    [chatPresets]
  )

  if (!presets.length) {
    return (
      <Link to='/playground' className='sitem'>
        {item.icon ? <item.icon size={16} /> : null}
        <span>{item.title}</span>
      </Link>
    )
  }

  const openExternal = async (preset: ChatPreset) => {
    if (preset.type === 'web') return
    const needsKey = chatLinkRequiresApiKey(preset.url)
    if (loadingId) return

    let activeKey: string | undefined
    if (needsKey) {
      setLoadingId(preset.id)
      try {
        activeKey = await fetchActiveChatKey()
      } catch (error) {
        toast.error(
          error instanceof Error
            ? error.message
            : t('Unable to prepare chat link. Please ensure you have an enabled API key.')
        )
        return
      } finally {
        setLoadingId(null)
      }
    }

    const url = resolveChatUrl({
      template: preset.url,
      apiKey: needsKey ? activeKey : undefined,
      serverAddress,
    })
    if (url) window.open(url, '_blank', 'noopener')
  }

  return (
    <div>
      <button className='sgroup tog' onClick={() => setOpen(!open)}>
        <span style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
          {item.icon ? <item.icon size={14} /> : null}
          {item.title}
        </span>
        <ChevronDown
          size={14}
          style={{
            transform: open ? 'rotate(180deg)' : 'none',
            transition: 'transform 0.2s',
          }}
        />
      </button>
      {open && (
        <div className='moreitems'>
          {presets.map((preset) => {
            const active = location.pathname === `/chat/${preset.id}`
            if (preset.type === 'web') {
              return (
                <Link
                  key={preset.id}
                  to='/chat/$chatId'
                  params={{ chatId: preset.id }}
                  className={cn('sitem', active && 'on')}
                  style={{ paddingLeft: 30 }}
                >
                  <span>{preset.name}</span>
                </Link>
              )
            }
            return (
              <button
                key={preset.id}
                className='sitem'
                style={{ paddingLeft: 30, width: '100%', textAlign: 'left' }}
                onClick={() => void openExternal(preset)}
              >
                <span>{preset.name}</span>
                {loadingId === preset.id ? (
                  <Loader2 size={14} className='animate-spin' />
                ) : (
                  <ExternalLink size={14} />
                )}
              </button>
            )
          })}
        </div>
      )}
    </div>
  )
}
