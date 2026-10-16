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
import { useEffect, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { LayoutProvider } from '@/context/layout-provider'
import { SearchProvider } from '@/context/search-provider'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import '@/components/layout/components/console-shell.css'
import { AnimatedOutlet } from '@/components/page-transition'
import { SkipToMain } from '@/components/skip-to-main'
import { DawnConsoleSidebar } from './dawn-console-sidebar'
import { DawnConsoleTopNav } from './dawn-console-topnav'

interface DawnConsoleLayoutProps {
  children?: ReactNode
}

/**
 * 全站认证区统一 Dawn 外壳。根节点带 .dawn 作用域，让 dawn.css 中
 * 声明的 --dawn-* 变量与 .dawn-console-* 样式同时生效。
 */
export function DawnConsoleLayout({ children }: DawnConsoleLayoutProps) {
  const { t } = useTranslation()
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false)

  useEffect(() => {
    const desktop = window.matchMedia('(min-width: 769px)')
    const closeOnDesktop = () => {
      if (desktop.matches) setSidebarOpen(false)
    }
    desktop.addEventListener('change', closeOnDesktop)
    return () => desktop.removeEventListener('change', closeOnDesktop)
  }, [])

  return (
    <LayoutProvider>
      <SearchProvider>
        <div className='dawn dawn-console-layout'>
          <SkipToMain />
          <DawnConsoleTopNav
            menuOpen={sidebarOpen}
            onMenuClick={() => setSidebarOpen(true)}
          />
          <div className='dawn-console-body'>
            <div className='console-desktop-sidebar'>
              <DawnConsoleSidebar
                open={sidebarOpen}
                collapsed={sidebarCollapsed}
                onToggleCollapsed={() => setSidebarCollapsed((value) => !value)}
                onNavigate={() => setSidebarOpen(false)}
              />
            </div>
            <Sheet open={sidebarOpen} onOpenChange={setSidebarOpen}>
              <SheetContent
                side='left'
                className='dawn console-mobile-sidebar w-[min(320px,88vw)] gap-0 p-0'
                aria-describedby={undefined}
              >
                <SheetHeader className='border-b pr-16'>
                  <SheetTitle>{t('Navigation')}</SheetTitle>
                </SheetHeader>
                <DawnConsoleSidebar
                  open
                  onNavigate={() => setSidebarOpen(false)}
                />
              </SheetContent>
            </Sheet>
            <div className='dawn-console-main @container/content'>
              {children ?? <AnimatedOutlet />}
            </div>
          </div>
        </div>
      </SearchProvider>
    </LayoutProvider>
  )
}
