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

For commercial licensing, please contact support@quantumnous.com.
*/
import { useEffect, useState } from 'react'
import { Link, useLocation } from '@tanstack/react-router'
import { LayoutDashboard } from 'lucide-react'
import { useAuthStore } from '@/stores/auth-store'
import { normalizeSystemName } from '@/lib/branding'

const NAV_ITEMS = [
  { title: '主页', href: '/' },
  { title: '市场', href: '/market' },
  { title: '模型', href: '/pricing' },
  { title: '控制台', href: '/dashboard' },
  { title: '状态', href: '/status' },
]

/** 悬浮胶囊导航：logo 居左、胶囊居中、账户区居右，下滑隐藏。 */
export function DawnNav({ variant = 'light' }: { variant?: 'light' | 'hero' }) {
  const location = useLocation()
  const user = useAuthStore((state) => state.auth.user)
  const [hidden, setHidden] = useState(false)

  useEffect(() => {
    let lastY = window.scrollY
    const onScroll = () => {
      const y = window.scrollY
      setHidden(y > 80 && y > lastY)
      lastY = y
    }
    window.addEventListener('scroll', onScroll, { passive: true })
    return () => window.removeEventListener('scroll', onScroll)
  }, [])

  const isActive = (href: string) =>
    href === '/' ? location.pathname === '/' : location.pathname.startsWith(href)

  return (
    <nav className={`dawn-nav${hidden ? ' hidden-down' : ''}`}>
      <Link className='logo' to='/' style={variant === 'hero' ? { color: '#FFF7EE' } : undefined}>
        <svg className='dot' viewBox='0 0 256 256' width={22} height={22}>
          <defs>
            <linearGradient
              id='gearGlow-nav'
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
              id='sparkCore-nav'
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
          <circle cx='128' cy='128' r='88' fill='url(#gearGlow-nav)' opacity='0.14' />
          <path
            d='M133.905 31.16C129.971 25.6133 121.756 25.6133 117.822 31.16L107.218 46.112C103.142 51.8576 95.9024 54.4697 89.0308 52.744L71.1508 48.254C64.5172 46.5886 57.6638 51.121 56.7736 57.9012L54.375 76.1574C53.4546 83.1678 48.602 89.0164 41.8716 91.21L24.3425 96.9247C17.8294 99.0472 15.2906 106.86 18.9379 112.644L28.7523 128.21C32.5219 134.19 32.5219 141.81 28.7523 147.79L18.9379 163.356C15.2906 169.14 17.8294 176.953 24.3425 179.075L41.8716 184.79C48.602 186.984 53.4546 192.832 54.375 199.843L56.7736 218.099C57.6638 224.879 64.5172 229.411 71.1508 227.746L89.0308 223.256C95.9024 221.53 103.142 224.142 107.218 229.888L117.822 244.84C121.756 250.387 129.971 250.387 133.905 244.84L144.509 229.888C148.585 224.142 155.825 221.53 162.696 223.256L180.576 227.746C187.21 229.411 194.063 224.879 194.954 218.099L197.352 199.843C198.272 192.832 203.125 186.984 209.855 184.79L227.384 179.075C233.897 176.953 236.436 169.14 232.789 163.356L222.975 147.79C219.205 141.81 219.205 134.19 222.975 128.21L232.789 112.644C236.436 106.86 233.897 99.0472 227.384 96.9247L209.855 91.21C203.125 89.0164 198.272 83.1678 197.352 76.1574L194.954 57.9012C194.063 51.121 187.21 46.5886 180.576 48.254L162.696 52.744C155.825 54.4697 148.585 51.8576 144.509 46.112L133.905 31.16Z'
            fill='url(#gearGlow-nav)'
          />
          <circle cx='128' cy='138' r='48' fill='#FFFDF8' />
          <path
            d='M128 84L138.769 113.231L168 124L138.769 134.769L128 164L117.231 134.769L88 124L117.231 113.231L128 84Z'
            fill='url(#sparkCore-nav)'
          />
          <circle cx='99' cy='84' r='13' fill='#FFF4C4' />
          <circle cx='157' cy='84' r='13' fill='#FFF4C4' />
          <circle cx='88' cy='104' r='11' fill='#FFF4C4' />
          <circle cx='168' cy='104' r='11' fill='#FFF4C4' />
        </svg>
        <span className='logo-text'>{normalizeSystemName()}</span>
      </Link>
      <div className='pill'>
        {NAV_ITEMS.map((item) => (
          <Link
            key={item.href}
            to={item.href}
            className={isActive(item.href) ? 'on' : ''}
          >
            {item.title}
          </Link>
        ))}
      </div>
      <div className='right'>
        {user ? (
          <Link className='btn mini primary' to='/dashboard'>
            <LayoutDashboard size={14} />
            {user.display_name || user.username}
          </Link>
        ) : (
          <Link className='btn mini primary' to='/sign-in'>
            登录 / 注册
          </Link>
        )}
      </div>
    </nav>
  )
}
