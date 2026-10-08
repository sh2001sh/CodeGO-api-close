import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { Outlet, useNavigate, useRouterState } from '@tanstack/react-router'
import { Dialog as BaseDialog } from '@base-ui/react/dialog'
import { KeyRound, Languages, LogOut, Moon, Sun, X } from 'lucide-react'
import { api } from '../lib/api'
import { sessionOptions } from '../lib/queries'
import { useTranslation } from '../lib/i18n'
import { languages } from '../lib/locales'
import { useTheme } from '../lib/preferences'
import { ErrorMessage, IconButton } from './ui'
import { SidebarNav } from './app/sidebar-nav'
import { UserMenu } from './app/user-menu'
import { CommandPalette, type PaletteAction } from './app/command-palette'
import {
  BalanceChip,
  Brand,
  CompanyIdentity,
  GlobalTopbar,
  usePaletteHotkey,
} from './app/global-topbar'

export { Brand } from './app/global-topbar'

/** Signed-in frame: the shared top bar, the grouped sidebar beneath it, and the page. */
export function Shell() {
  const { t, locale, change } = useTranslation()
  const { theme, setTheme } = useTheme()
  const user = useSuspenseQuery(sessionOptions()).data
  const pathname = useRouterState({ select: (state) => state.location.pathname })
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [paletteOpen, setPaletteOpen] = useState(false)
  const [drawerOpen, setDrawerOpen] = useState(false)
  usePaletteHotkey(() => setPaletteOpen(true))
  useEffect(() => setDrawerOpen(false), [pathname])

  const logout = useMutation({
    mutationFn: () => api.POST('/api/user/logout'),
    onSuccess: () => {
      queryClient.clear()
      void navigate({ to: '/sign-in' })
    },
  })
  const dark = theme === 'dark'
  const actions = useMemo<PaletteAction[]>(
    () => [
      {
        id: 'new-key',
        label: '创建 API Key',
        icon: KeyRound,
        keywords: 'create key new token',
        run: () => void navigate({ to: '/keys' }),
      },
      {
        id: 'theme',
        label: dark ? '切换到浅色模式' : '切换到深色模式',
        icon: dark ? Sun : Moon,
        keywords: 'theme dark light 主题 深色 浅色',
        run: () => setTheme(dark ? 'light' : 'dark'),
      },
      ...languages
        .filter((language) => language.code !== locale)
        .map((language) => ({
          id: `locale-${language.code}`,
          label: language.name,
          icon: Languages,
          keywords: `language 语言 ${language.code}`,
          run: () => void change(language.code),
        })),
      {
        id: 'logout',
        label: '退出登录',
        icon: LogOut,
        keywords: 'logout sign out',
        run: () => logout.mutate(),
      },
    ],
    // Rebuilt only when labels change; navigate/setTheme/mutate are stable references.
    [dark, locale],
  )

  return (
    <div className="app-shell">
      <a className="skip-link" href="#main-content">
        {t('跳转到内容')}
      </a>
      <GlobalTopbar
        brandTo="/"
        onSearch={() => setPaletteOpen(true)}
        onOpenNav={() => setDrawerOpen(true)}
        account={
          <>
            <BalanceChip />
            <UserMenu
              name={user.display_name || user.username}
              email={user.email}
              role={user.role}
              loggingOut={logout.isPending}
              onLogout={() => logout.mutate()}
            />
          </>
        }
      />
      <aside className="sidebar">
        <SidebarNav role={user.role} />
        <div className="sidebar-footer">
          <CompanyIdentity />
        </div>
      </aside>
      <main className="page" id="main-content" tabIndex={-1}>
        <ErrorMessage error={logout.error} />
        <Outlet />
      </main>
      <BaseDialog.Root open={drawerOpen} onOpenChange={setDrawerOpen}>
        <BaseDialog.Portal>
          <BaseDialog.Backdrop className="overlay-backdrop" />
          <BaseDialog.Popup className="drawer-popup" data-side="left">
            <div className="drawer-header">
              <BaseDialog.Title className="sr-only">{t('主导航')}</BaseDialog.Title>
              <Brand to="/" />
              <BaseDialog.Close render={<IconButton label={t('关闭导航')} />}>
                <X size={18} aria-hidden />
              </BaseDialog.Close>
            </div>
            <div className="sidebar">
              <SidebarNav role={user.role} onNavigate={() => setDrawerOpen(false)} />
            </div>
          </BaseDialog.Popup>
        </BaseDialog.Portal>
      </BaseDialog.Root>
      <CommandPalette
        open={paletteOpen}
        onOpenChange={setPaletteOpen}
        role={user.role}
        actions={actions}
      />
    </div>
  )
}
