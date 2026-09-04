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
import { useEffect, useState } from 'react'
import { Link, useLocation } from '@tanstack/react-router'
import { toast } from 'sonner'
import {
  Activity,
  Home,
  LogOut,
  Menu,
  Settings,
  Store,
  TrendingUp,
  User,
} from 'lucide-react'
import { useAuthStore } from '@/stores/auth-store'
import { normalizeSystemName } from '@/lib/branding'
import { useNotifications } from '@/hooks/use-notifications'
import { LanguageSwitcher } from '@/components/language-switcher'
import { NotificationButton } from '@/components/notification-button'
import { NotificationDialog } from '@/components/notification-dialog'
import { Search } from '@/components/search'
import { LuckyRewardNotifier } from '@/features/daily-lucky-number/components/lucky-reward-notifier'

const TOP_NAV_ITEMS = [
  { id: 'home', icon: Home, label: '首页', path: '/' },
  { id: 'market', icon: Store, label: '市场', path: '/market' },
  { id: 'pricing', icon: TrendingUp, label: '模型', path: '/pricing' },
  { id: 'status', icon: Activity, label: '状态', path: '/status' },
]

export function DawnConsoleTopNav(props: { onMenuClick?: () => void }) {
  const location = useLocation()
  const user = useAuthStore((state) => state.auth.user)
  const logout = useAuthStore((state) => state.auth.reset)
  const notifications = useNotifications()
  const [userMenuOpen, setUserMenuOpen] = useState(false)
  const isAdmin = (user?.role ?? 0) >= 10
  const userId = user?.id

  useEffect(() => {
    if (!userId) return
    const now = new Date()
    const token = `${now.getFullYear()}-${`${now.getMonth() + 1}`.padStart(2, '0')}-${`${now.getDate()}`.padStart(2, '0')}`
    const key = `workshop:welcome:${userId}:${token}`
    if (window.localStorage.getItem(key) === '1') return
    window.localStorage.setItem(key, '1')
    toast.success('早安工匠，今天也是充满活力的一天')
  }, [userId])

  const handleLogout = () => {
    logout()
    window.location.href = '/sign-in'
  }

  const isActive = (path: string) =>
    path === '/' ? location.pathname === '/' : location.pathname.startsWith(path)

  return (
    <>
      <header className='dawn-console-topnav'>
        <Link className='logo' to='/dashboard'>
          <svg className='logo-icon' viewBox='0 0 256 256' width={20} height={20}>
            <defs>
              <linearGradient
                id='gearGlow'
                x1='40'
                y1='36'
                x2='216'
                y2='220'
                gradientUnits='userSpaceOnUse'
              >
                <stop stopColor='#FF9A5C' />
                <stop offset='0.55' stopColor='#F06738' />
                <stop offset='1' stopColor='#467FF2' />
              </linearGradient>
              <linearGradient
                id='sparkCore'
                x1='110'
                y1='88'
                x2='170'
                y2='170'
                gradientUnits='userSpaceOnUse'
              >
                <stop stopColor='#FFF4C4' />
                <stop offset='1' stopColor='#F1B548' />
              </linearGradient>
            </defs>
            <rect width='256' height='256' rx='64' fill='#FFF8EF' />
            <circle cx='128' cy='128' r='88' fill='url(#gearGlow)' opacity='0.14' />
            <path
              d='M133.905 31.16C129.971 25.6133 121.756 25.6133 117.822 31.16L107.218 46.112C103.142 51.8576 95.9024 54.4697 89.0308 52.744L71.1508 48.254C64.5172 46.5886 57.6638 51.121 56.7736 57.9012L54.375 76.1574C53.4546 83.1678 48.602 89.0164 41.8716 91.21L24.3425 96.9247C17.8294 99.0472 15.2906 106.86 18.9379 112.644L28.7523 128.21C32.5219 134.19 32.5219 141.81 28.7523 147.79L18.9379 163.356C15.2906 169.14 17.8294 176.953 24.3425 179.075L41.8716 184.79C48.602 186.984 53.4546 192.832 54.375 199.843L56.7736 218.099C57.6638 224.879 64.5172 229.411 71.1508 227.746L89.0308 223.256C95.9024 221.53 103.142 224.142 107.218 229.888L117.822 244.84C121.756 250.387 129.971 250.387 133.905 244.84L144.509 229.888C148.585 224.142 155.825 221.53 162.696 223.256L180.576 227.746C187.21 229.411 194.063 224.879 194.954 218.099L197.352 199.843C198.272 192.832 203.125 186.984 209.855 184.79L227.384 179.075C233.897 176.953 236.436 169.14 232.789 163.356L222.975 147.79C219.205 141.81 219.205 134.19 222.975 128.21L232.789 112.644C236.436 106.86 233.897 99.0472 227.384 96.9247L209.855 91.21C203.125 89.0164 198.272 83.1678 197.352 76.1574L194.954 57.9012C194.063 51.121 187.21 46.5886 180.576 48.254L162.696 52.744C155.825 54.4697 148.585 51.8576 144.509 46.112L133.905 31.16Z'
              fill='url(#gearGlow)'
            />
            <circle cx='128' cy='138' r='48' fill='#FFFDF8' />
            <path
              d='M128 84L138.769 113.231L168 124L138.769 134.769L128 164L117.231 134.769L88 124L117.231 113.231L128 84Z'
              fill='url(#sparkCore)'
            />
            <circle cx='99' cy='84' r='13' fill='#FFF4C4' />
            <circle cx='157' cy='84' r='13' fill='#FFF4C4' />
            <circle cx='88' cy='104' r='11' fill='#FFF4C4' />
            <circle cx='168' cy='104' r='11' fill='#FFF4C4' />
          </svg>
          <span>{normalizeSystemName()}</span>
        </Link>

        <button
          className='menu-btn'
          aria-label='打开菜单'
          onClick={props.onMenuClick}
        >
          <Menu size={18} />
        </button>
        <span className='crumb'>CONSOLE</span>
        <span className='sp' />

        <nav className='pill'>
          {TOP_NAV_ITEMS.map((item) => {
            const Icon = item.icon
            return (
              <Link
                key={item.id}
                to={item.path}
                className={`nav-link${isActive(item.path) ? ' active' : ''}`}
              >
                <Icon size={13} />
                {item.label}
              </Link>
            )
          })}
        </nav>

        <div className='right-actions'>
          <Search />
          <LuckyRewardNotifier />
          <NotificationButton
            unreadCount={notifications.unreadCount}
            onClick={() => notifications.openDialog()}
          />
          <LanguageSwitcher />
          {isAdmin && (
            <Link className='icon-btn' to='/system-settings/site' title='设置'>
              <Settings size={16} />
            </Link>
          )}
          <div
            className={`user-menu${userMenuOpen ? ' open' : ''}`}
            onMouseLeave={() => setUserMenuOpen(false)}
          >
            <button
              className='user-btn'
              aria-expanded={userMenuOpen}
              aria-haspopup='true'
              onClick={() => setUserMenuOpen((current) => !current)}
            >
              <User size={14} />
              <span>{user?.display_name || user?.username || '用户'}</span>
            </button>
            <div className='user-dropdown' onClick={() => setUserMenuOpen(false)}>
              <Link to='/profile' className='dropdown-item'>
                <User size={14} />
                个人资料
              </Link>
              {isAdmin && (
                <Link to='/system-settings/site' className='dropdown-item'>
                  <Settings size={14} />
                  系统设置
                </Link>
              )}
              <button className='dropdown-item' onClick={handleLogout}>
                <LogOut size={14} />
                退出登录
              </button>
            </div>
          </div>
        </div>
      </header>

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
    </>
  )
}
