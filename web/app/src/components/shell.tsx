import { useState } from 'react'
import { useMutation, useSuspenseQuery, useQueryClient } from '@tanstack/react-query'
import { Link, Outlet, useNavigate, useRouterState } from '@tanstack/react-router'
import {
  Activity,
  Box,
  CreditCard,
  KeyRound,
  LayoutDashboard,
  ListOrdered,
  LogOut,
  Settings,
  Users,
  Waypoints,
} from 'lucide-react'
import { api } from '../lib/api'
import { sessionOptions } from '../lib/queries'
import { useTranslation } from '../lib/i18n'
import { Button, ErrorMessage } from './ui'

const browse = [
  { to: '/channel-market', label: '渠道市场', icon: Waypoints },
  { to: '/wallet', label: '钱包', icon: CreditCard },
  { to: '/group-buy', label: '拼团', icon: Users },
  { to: '/blind-box', label: '盲盒', icon: Box },
  { to: '/community', label: '社区', icon: Users },
] as const
const manage = [
  { to: '/dashboard', label: '仪表板', icon: LayoutDashboard },
  { to: '/keys', label: 'API Key', icon: KeyRound },
  { to: '/usage-logs', label: '使用日志', icon: Activity },
  { to: '/orders', label: '订单', icon: ListOrdered },
  { to: '/my-channels', label: '我的渠道', icon: Waypoints },
  { to: '/transfers', label: '钱包转账', icon: CreditCard },
  { to: '/invoices', label: '发票', icon: ListOrdered },
  { to: '/profile', label: '个人资料', icon: Users },
] as const
const adminLinks = [
  { to: '/channels', label: '渠道', icon: Waypoints },
  { to: '/users', label: '用户', icon: Users },
  { to: '/settings', label: '系统设置', icon: Settings },
  { to: '/market-admin', label: '市场审核', icon: Waypoints },
  { to: '/subscriptions', label: '套餐管理', icon: CreditCard },
  { to: '/redemptions', label: '兑换码管理', icon: KeyRound },
] as const

export function Shell() {
  const { t, locale, change } = useTranslation()
  const user = useSuspenseQuery(sessionOptions()).data
  const pathname = useRouterState({ select: (state) => state.location.pathname })
  const [zone, setZone] = useState<'browse' | 'manage'>(
    browse.some((link) => link.to === pathname) ? 'browse' : 'manage',
  )
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const logout = useMutation({
    mutationFn: () => api.POST('/api/user/logout'),
    onSuccess: () => {
      queryClient.clear()
      void navigate({ to: '/sign-in' })
    },
  })
  const links =
    zone === 'browse'
      ? browse
      : [...manage, ...(user.role === 'admin' || user.role === 'root' ? adminLinks : [])]
  return (
    <>
      <a className="sr-only focus:not-sr-only" href="#main-content">
        {t('跳转到内容')}
      </a>
      <header className="topbar">
        <Link className="brand" to="/dashboard">
          CodeGo <span>new-api</span>
        </Link>
        <div className="zone-switch" aria-label={t('工作区域')}>
          <button
            aria-pressed={zone === 'browse'}
            onClick={() => {
              setZone('browse')
              void navigate({ to: '/wallet' })
            }}
          >
            {t('逛')}
          </button>
          <button
            aria-pressed={zone === 'manage'}
            onClick={() => {
              setZone('manage')
              void navigate({ to: '/dashboard' })
            }}
          >
            {t('管')}
          </button>
        </div>
        <div className="topbar-actions">
          <span className="user-name">{user.display_name || user.username}</span>
          <Button variant="quiet" onClick={() => void change(locale === 'zh-CN' ? 'en' : 'zh-CN')}>
            {locale === 'zh-CN' ? 'EN' : '中文'}
          </Button>
          <Button
            variant="quiet"
            disabled={logout.isPending}
            aria-label={t('退出登录')}
            onClick={() => logout.mutate()}
          >
            <LogOut size={16} aria-hidden />
          </Button>
        </div>
      </header>
      <div className="workspace">
        <aside className="sidebar">
          <nav aria-label={t('主导航')}>
            {links.map((link) => (
              <Link key={link.to} to={link.to}>
                <link.icon size={18} strokeWidth={1.8} aria-hidden />
                <span>{t(link.label)}</span>
              </Link>
            ))}
          </nav>
        </aside>
        <main className="page" id="main-content">
          <ErrorMessage error={logout.error} />
          <Outlet />
          <footer>new-api · QuantumNous</footer>
        </main>
      </div>
    </>
  )
}
