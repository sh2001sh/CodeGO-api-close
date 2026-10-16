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
import { useState } from 'react'
import { Link, useNavigate } from '@tanstack/react-router'
import { Ellipsis, Menu } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { cn } from '@/lib/utils'
import { useNotifications } from '@/hooks/use-notifications'
import { useSystemConfig } from '@/hooks/use-system-config'
import { useTopNavLinks } from '@/hooks/use-top-nav-links'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { LanguageSwitcher } from '@/components/language-switcher'
import { NotificationButton } from '@/components/notification-button'
import { NotificationDialog } from '@/components/notification-dialog'
import { ProfileDropdown } from '@/components/profile-dropdown'
import { Search } from '@/components/search'
import { ThemeSwitch } from '@/components/theme-switch'
import type { TopNavLink } from '../types'
import { GlobalNavigation } from './global-navigation'
import { HeaderLogo } from './header-logo'
import './public-header.css'

export interface PublicHeaderProps {
  navLinks?: TopNavLink[]
  mobileLinks?: TopNavLink[]
  navContent?: React.ReactNode
  showThemeSwitch?: boolean
  showLanguageSwitcher?: boolean
  logo?: React.ReactNode
  siteName?: string
  homeUrl?: string
  beforeBrand?: React.ReactNode
  leftContent?: React.ReactNode
  rightContent?: React.ReactNode
  actionContent?: React.ReactNode
  showSearch?: boolean
  showNavigation?: boolean
  showAuthButtons?: boolean
  showNotifications?: boolean
  showProfileDropdown?: boolean
  className?: string
}

/** Canonical site chrome, shared by public pages and the authenticated console. */
export function PublicHeader(props: PublicHeaderProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [mobileOpen, setMobileOpen] = useState(false)
  const user = useAuthStore((state) => state.auth.user)
  const config = useSystemConfig({ autoLoad: true })
  const notifications = useNotifications()
  const defaultLinks = useTopNavLinks()
  const links = props.navLinks ?? defaultLinks
  const primaryLinks = links.filter((link) => !link.secondary)
  const secondaryLinks = links.filter((link) => link.secondary)

  function handleUtilityClick(
    event: Pick<
      React.MouseEvent,
      'preventDefault' | 'metaKey' | 'ctrlKey' | 'shiftKey' | 'altKey'
    >,
    link: TopNavLink
  ): void {
    if (link.disabled) {
      event.preventDefault()
      return
    }
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
    if (link.requiresAuth && !user) {
      event.preventDefault()
      void navigate({ to: '/sign-in', search: { redirect: link.href } })
    }
  }

  return (
    <>
      <header className={cn('codego-public-header', props.className)}>
        <div className='codego-public-header-inner'>
          <div className='codego-public-brand-group'>
            {props.beforeBrand}
            <Link className='codego-public-brand' to={props.homeUrl ?? '/'}>
              {props.logo ?? (
                <HeaderLogo
                  src={config.logo}
                  loading={config.loading}
                  logoLoaded={config.logoLoaded}
                  className='size-8 object-contain'
                />
              )}
              <span>
                {config.loading ? (
                  <Skeleton className='h-6 w-20' />
                ) : (
                  (props.siteName ?? config.systemName)
                )}
              </span>
            </Link>
            {props.leftContent}
          </div>
          {props.showNavigation !== false && (
            <nav
              className='codego-public-desktop-nav'
              aria-label={t('gateway.navigation')}
            >
              {props.navContent ?? <GlobalNavigation links={primaryLinks} />}
            </nav>
          )}
          <div className='codego-public-actions'>
            {props.showNavigation !== false && secondaryLinks.length > 0 && (
              <DropdownMenu modal={false}>
                <DropdownMenuTrigger
                  render={
                    <Button
                      variant='ghost'
                      size='icon'
                      className='codego-public-more'
                      aria-label={t('More')}
                    />
                  }
                >
                  <Ellipsis size={19} aria-hidden='true' />
                </DropdownMenuTrigger>
                <DropdownMenuContent
                  align='end'
                  sideOffset={12}
                  className='codego-public-utility-menu'
                >
                  {secondaryLinks.map((link) => (
                    <DropdownMenuItem
                      key={link.href}
                      disabled={link.disabled}
                      render={
                        link.external ? (
                          <a
                            href={link.href}
                            target='_blank'
                            rel='noopener noreferrer'
                          />
                        ) : (
                          <Link to={link.href} />
                        )
                      }
                      onClick={(event) => handleUtilityClick(event, link)}
                    >
                      {t(link.title)}
                    </DropdownMenuItem>
                  ))}
                </DropdownMenuContent>
              </DropdownMenu>
            )}
            {props.rightContent ?? (
              <>
                {props.showSearch && (
                  <Search iconOnly className='codego-global-search' />
                )}
                {props.showNotifications !== false && user && (
                  <NotificationButton
                    unreadCount={notifications.unreadCount}
                    onClick={() => notifications.openDialog()}
                  />
                )}
                <div className='codego-public-preferences'>
                  {props.showLanguageSwitcher !== false && <LanguageSwitcher />}
                  {props.showThemeSwitch !== false && <ThemeSwitch />}
                </div>
                {props.actionContent}
                {props.showAuthButtons !== false &&
                  (user ? (
                    props.showProfileDropdown !== false && <ProfileDropdown />
                  ) : (
                    <Button
                      render={<Link to='/sign-in' />}
                      className='codego-public-signin'
                    >
                      {t('Sign in')}
                    </Button>
                  ))}
              </>
            )}
            {props.showNavigation !== false && (
              <Button
                variant='ghost'
                size='icon'
                className='codego-public-menu'
                aria-label={t('Open menu')}
                aria-expanded={mobileOpen}
                onClick={() => setMobileOpen(true)}
              >
                <Menu size={20} aria-hidden='true' />
              </Button>
            )}
          </div>
        </div>
      </header>
      <Sheet open={mobileOpen} onOpenChange={setMobileOpen}>
        <SheetContent
          side='right'
          aria-describedby={undefined}
          className='codego-public-mobile-sheet w-[min(360px,90vw)] gap-5'
        >
          <SheetHeader>
            <SheetTitle>{props.siteName ?? config.systemName}</SheetTitle>
          </SheetHeader>
          <nav
            className='codego-public-mobile-nav'
            aria-label={t('gateway.navigation')}
          >
            <GlobalNavigation
              mobile
              links={props.mobileLinks ?? links}
              onNavigate={() => setMobileOpen(false)}
            />
          </nav>
          <div className='codego-public-mobile-preferences'>
            {props.showLanguageSwitcher !== false && <LanguageSwitcher />}
            {props.showThemeSwitch !== false && <ThemeSwitch />}
          </div>
          {props.showAuthButtons !== false && !user && (
            <Button
              className='mx-4'
              render={<Link to='/sign-in' />}
              onClick={() => setMobileOpen(false)}
            >
              {t('Sign in')}
            </Button>
          )}
        </SheetContent>
      </Sheet>
      {props.showNotifications !== false && (
        <NotificationDialog
          open={notifications.dialogOpen}
          onOpenChange={notifications.setDialogOpen}
          activeTab={notifications.activeTab}
          onTabChange={notifications.setActiveTab}
          notice={notifications.notice}
          announcements={notifications.announcements}
          loading={notifications.loading}
          onCloseToday={notifications.closeToday}
        />
      )}
    </>
  )
}
