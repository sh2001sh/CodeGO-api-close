import { useEffect, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useRouterState } from '@tanstack/react-router'
import { Menu as BaseMenu } from '@base-ui/react/menu'
import { Menu, Moon, Search, Sun, Wallet, MessagesSquare, ChevronDown } from 'lucide-react'
import { useTranslation } from '../../lib/i18n'
import { useTheme } from '../../lib/preferences'
import { publicNav, resourceNav, isActive } from '../../lib/navigation'
import { walletOptions } from '../../lib/queries'
import { credits } from '../../lib/format'
import { IconButton } from '../primitives/button'
import { LanguageMenu } from './language-menu'
import { NotificationCenter } from './notification-center'

export const communityURL = 'https://community.codegoai.com'

/** Product wordmark with the open gateway and copper route endpoint. */
export function Brand(props: { to?: string }) {
  return (
    <Link className="brand" to={props.to ?? '/'} aria-label="CodeGo AI" dir="ltr">
      <svg className="brand-mark" viewBox="0 0 32 32" fill="none" aria-hidden="true">
        <path
          fill="currentColor"
          d="M26 4H12C6.48 4 2 8.48 2 14v4c0 5.52 4.48 10 10 10h14a2 2 0 0 0 2-2v-2a2 2 0 0 0-2-2H12a4 4 0 0 1-4-4v-4a4 4 0 0 1 4-4h14a2 2 0 0 0 2-2V6a2 2 0 0 0-2-2Z"
        />
        <path fill="currentColor" d="M17 13h6v6h-6a3 3 0 0 1 0-6Z" />
        <rect x="25" y="13" width="6" height="6" rx="1.5" fill="var(--brand-color)" />
      </svg>
      <span className="brand-name">
        CodeGo <span className="brand-ai">AI</span>
      </span>
    </Link>
  )
}

export function CompanyIdentity() {
  const { t, locale } = useTranslation()
  return (
    <div className="company-identity">
      <strong dir="ltr">CodeGo AI Limited</strong>
      <span>
        <bdi lang={locale === 'zh-CN' ? 'zh-Hans' : 'zh-Hant'}>
          {locale === 'zh-CN' ? '码高智能有限公司' : '碼高智能有限公司'}
        </bdi>{' '}
        <span className="company-location">· {t('香港')}</span>
      </span>
    </div>
  )
}

/** Opens the command palette on Ctrl/⌘K and "/" (outside text inputs). */
export function usePaletteHotkey(open: () => void) {
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null
      const typing = target?.closest('input, textarea, select, [contenteditable="true"]')
      if (
        (event.key === 'k' && (event.metaKey || event.ctrlKey)) ||
        (event.key === '/' && !typing)
      ) {
        event.preventDefault()
        open()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open])
}

export function BalanceChip() {
  const { t } = useTranslation()
  const wallet = useQuery(walletOptions())
  if (!wallet.data) return null
  const label = credits(wallet.data.balance_micro_credits).replace(' credits', '')
  return (
    <Link to="/wallet" className="balance-chip" aria-label={`${t('钱包余额')} ${label} credits`}>
      <Wallet size={14} aria-hidden />
      {label}
      <span>credits</span>
    </Link>
  )
}

/**
 * The one top bar shared by the public site and the console. Left: brand and site sections.
 * Centre-right: page/feature search. Right: whatever the caller puts in `account`.
 */
export function GlobalTopbar(props: {
  account: ReactNode
  onSearch: () => void
  onOpenNav?: () => void
  /** Accessible label of the nav trigger; defaults to the console wording. */
  navLabel?: string
  brandTo?: string
}) {
  const { t } = useTranslation()
  const { theme, setTheme } = useTheme()
  const pathname = useRouterState({ select: (state) => state.location.pathname })
  const dark = theme === 'dark'
  return (
    <header className="topbar">
      {props.onOpenNav && (
        <IconButton
          className="nav-trigger"
          label={t(props.navLabel ?? '打开导航')}
          onClick={props.onOpenNav}
        >
          <Menu size={18} aria-hidden />
        </IconButton>
      )}
      <Brand to={props.brandTo} />
      <button
        type="button"
        className="search-trigger"
        onClick={props.onSearch}
        aria-label={t('搜索页面和功能')}
      >
        <Search size={15} aria-hidden />
        <span>{t('搜索…')}</span>
        <kbd>Ctrl K</kbd>
      </button>
      <div className="topbar-spacer" />
      <nav className="topnav" aria-label={t('站点导航')}>
        {publicNav.map((item) => (
          <Link
            key={item.to}
            to={item.to}
            data-status={isActive(item, pathname) ? 'active' : undefined}
          >
            {t(item.label)}
          </Link>
        ))}
        <BaseMenu.Root>
          <BaseMenu.Trigger
            className="resource-trigger"
            data-status={
              resourceNav.some((item) => isActive(item, pathname)) ? 'active' : undefined
            }
          >
            {t('资源')} <ChevronDown size={14} aria-hidden />
          </BaseMenu.Trigger>
          <BaseMenu.Portal>
            <BaseMenu.Positioner sideOffset={8} align="start">
              <BaseMenu.Popup className="menu-popup">
                {resourceNav.map((item) => (
                  <BaseMenu.Item key={item.to} className="menu-item" render={<Link to={item.to} />}>
                    <item.icon size={16} aria-hidden />
                    {t(item.label)}
                  </BaseMenu.Item>
                ))}
              </BaseMenu.Popup>
            </BaseMenu.Positioner>
          </BaseMenu.Portal>
        </BaseMenu.Root>
      </nav>
      <div className="topbar-actions">
        <a
          className="community-link"
          href={communityURL}
          target="_blank"
          rel="noopener noreferrer"
          aria-label={t('社区')}
        >
          <MessagesSquare size={17} aria-hidden />
          <span>{t('社区')}</span>
        </a>
        <IconButton
          label={dark ? t('切换到浅色模式') : t('切换到深色模式')}
          onClick={() => setTheme(dark ? 'light' : 'dark')}
        >
          {dark ? <Sun size={17} aria-hidden /> : <Moon size={17} aria-hidden />}
        </IconButton>
        <LanguageMenu />
        <NotificationCenter />
        {props.account}
      </div>
    </header>
  )
}
