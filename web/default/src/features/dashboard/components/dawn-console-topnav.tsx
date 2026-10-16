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
import { Link } from '@tanstack/react-router'
import { Menu, Settings, Wallet } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { Button } from '@/components/ui/button'
import { PublicHeader } from '@/components/layout/components/public-header'

export function DawnConsoleTopNav(props: {
  onMenuClick?: () => void
  menuOpen?: boolean
}) {
  const { t } = useTranslation()
  const isAdmin = useAuthStore((state) => (state.auth.user?.role ?? 0) >= 10)

  return (
    <PublicHeader
      className='codego-global-console-header'
      showSearch
      beforeBrand={
        <Button
          variant='ghost'
          size='icon'
          className='codego-console-sidebar-menu'
          aria-label={t('Navigation')}
          aria-expanded={props.menuOpen}
          onClick={props.onMenuClick}
        >
          <Menu size={20} aria-hidden='true' />
        </Button>
      }
      actionContent={
        <>
          <Button
            variant='ghost'
            size='icon'
            className='codego-console-wallet'
            aria-label={t('Wallet')}
            render={<Link to='/wallet' />}
          >
            <Wallet size={18} aria-hidden='true' />
          </Button>
          {isAdmin && (
            <Button
              variant='ghost'
              size='icon'
              className='codego-console-settings'
              aria-label={t('Settings')}
              render={<Link to='/system-settings/site' />}
            >
              <Settings size={18} aria-hidden='true' />
            </Button>
          )}
        </>
      }
    />
  )
}
