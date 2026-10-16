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
import { useEffect } from 'react'
import interfaceI18n from '@/i18n/config'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { SidebarTrigger } from '@/components/ui/sidebar'
import { ConfigDrawer } from '@/components/config-drawer'
import type { TopNavLink } from '../types'
import './console-shell.css'
import { PublicHeader } from './public-header'

type AppHeaderProps = {
  navLinks?: TopNavLink[]
  showTopNav?: boolean
  leftContent?: React.ReactNode
  showSearch?: boolean
  rightContent?: React.ReactNode
  showNotifications?: boolean
  showConfigDrawer?: boolean
  showProfileDropdown?: boolean
}

export function AppHeader(props: AppHeaderProps) {
  const userId = useAuthStore((state) => state.auth.user?.id)

  useEffect(() => {
    if (!userId) return
    const now = new Date()
    const token = `${now.getFullYear()}-${`${now.getMonth() + 1}`.padStart(2, '0')}-${`${now.getDate()}`.padStart(2, '0')}`
    const key = `workshop:welcome:${userId}:${token}`
    if (window.localStorage.getItem(key) === '1') return
    window.localStorage.setItem(key, '1')
    toast.success(interfaceI18n.t('早安工匠，今天也是充满活力的一天'))
  }, [userId])

  return (
    <PublicHeader
      className='codego-global-console-header'
      navLinks={props.navLinks}
      showNavigation={props.showTopNav}
      beforeBrand={
        <SidebarTrigger variant='ghost' className='codego-console-menu-icon' />
      }
      leftContent={props.leftContent}
      rightContent={props.rightContent}
      showSearch={props.showSearch !== false}
      showNotifications={props.showNotifications}
      showProfileDropdown={props.showProfileDropdown}
      actionContent={props.showConfigDrawer !== false && <ConfigDrawer />}
    />
  )
}
