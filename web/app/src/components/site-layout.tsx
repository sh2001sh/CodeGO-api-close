import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, Outlet, useNavigate, useRouterState } from '@tanstack/react-router'
import { Dialog as BaseDialog } from '@base-ui/react/dialog'
import { LayoutDashboard, LogIn, UserPlus, X } from 'lucide-react'
import { useTranslation } from '../lib/i18n'
import { sessionOptions } from '../lib/queries'
import { isActive, publicNav, resourceNav } from '../lib/navigation'
import { IconButton } from './ui'
import {
  BalanceChip,
  Brand,
  CompanyIdentity,
  GlobalTopbar,
  communityURL,
  usePaletteHotkey,
} from './app/global-topbar'
import { CommandPalette, type PaletteAction } from './app/command-palette'

/**
 * Public frame. Same top bar as the console; signed-in visitors see their balance and a console
 * link, anonymous ones see sign-in and sign-up. The session probe never throws here.
 */
export function SiteLayout() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const session = useQuery({ ...sessionOptions(), throwOnError: false })
  const [paletteOpen, setPaletteOpen] = useState(false)
  const [navOpen, setNavOpen] = useState(false)
  const pathname = useRouterState({ select: (state) => state.location.pathname })
  useEffect(() => setNavOpen(false), [pathname])
  usePaletteHotkey(() => setPaletteOpen(true))
  const signedIn = Boolean(session.data)
  const resolvingSession = session.isPending
  const actions = useMemo<PaletteAction[]>(
    () =>
      resolvingSession
        ? []
        : signedIn
          ? [
              {
                id: 'console',
                label: '进入控制台',
                icon: LayoutDashboard,
                run: () => void navigate({ to: '/dashboard' }),
              },
            ]
          : [
              {
                id: 'sign-in',
                label: '登录',
                icon: LogIn,
                keywords: 'login sign in',
                run: () => void navigate({ to: '/sign-in' }),
              },
              {
                id: 'sign-up',
                label: '注册',
                icon: UserPlus,
                keywords: 'register sign up',
                run: () => void navigate({ to: '/sign-up' }),
              },
            ],
    [navigate, signedIn, resolvingSession],
  )

  return (
    <div className="site">
      <a className="skip-link" href="#main-content">
        {t('跳转到内容')}
      </a>
      <GlobalTopbar
        onSearch={() => setPaletteOpen(true)}
        onOpenNav={() => setNavOpen(true)}
        navLabel="打开站点导航"
        account={
          resolvingSession ? (
            <span className="account-placeholder" aria-busy="true" aria-label={t('加载中')}>
              <span aria-hidden>···</span>
            </span>
          ) : signedIn ? (
            <>
              <BalanceChip />
              <Link to="/dashboard" className="button button-primary" data-size="sm">
                {t('进入控制台')}
              </Link>
            </>
          ) : (
            <>
              <Link to="/sign-in" className="button button-ghost" data-size="sm">
                {t('登录')}
              </Link>
              <Link to="/sign-up" className="button button-primary" data-size="sm">
                {t('注册')}
              </Link>
            </>
          )
        }
      />
      <main className="site-main" id="main-content" tabIndex={-1}>
        <Outlet />
      </main>
      <footer className="site-footer">
        <div className="site-container site-footer-inner">
          <div>
            <Brand />
            <CompanyIdentity />
          </div>
          <nav className="site-footer-links" aria-label={t('页脚导航')}>
            <div className="site-footer-column">
              <h2>{t('产品')}</h2>
              <Link to="/models">{t('模型与价格')}</Link>
              <Link to="/channel-market">{t('渠道市场')}</Link>
              <Link to="/playground">{t('对话')}</Link>
              <Link to="/dashboard">{t('控制台')}</Link>
            </div>
            <div className="site-footer-column">
              <h2>{t('开发者')}</h2>
              <Link to="/docs" search={{ article: 'quickstart' }}>
                {t('快速开始')}
              </Link>
              <Link to="/docs" search={{ article: 'client-integration' }}>
                {t('调用示例')}
              </Link>
              <Link to="/docs" search={{ article: 'billing' }}>
                {t('计费说明')}
              </Link>
              <Link to="/status">{t('服务状态')}</Link>
            </div>
            <div className="site-footer-column">
              <h2>{t('支持')}</h2>
              <Link to="/help">{t('常见问题')}</Link>
              <Link to="/support">{t('联系支持')}</Link>
              <a href={communityURL} target="_blank" rel="noopener noreferrer">
                {t('社区')}
              </a>
              <Link to="/support" hash="suppliers">
                {t('渠道合作')}
              </Link>
            </div>
            <div className="site-footer-column">
              <h2>{t('公司与政策')}</h2>
              <Link to="/about">{t('关于 CodeGo')}</Link>
              <Link to="/privacy">{t('隐私政策')}</Link>
              <Link to="/terms">{t('服务条款')}</Link>
              <Link to="/refund-policy">{t('退款规则')}</Link>
              <Link to="/supplier-agreement">{t('渠道供给与结算协议')}</Link>
              <Link to="/market-rules">{t('市场治理、评价与排名规则')}</Link>
            </div>
          </nav>
        </div>
        <div className="site-container site-footer-bottom">
          <small>© {new Date().getFullYear()} CodeGo AI Limited</small>
        </div>
      </footer>
      {/* Below the .topnav breakpoint the site sections move into this drawer. */}
      <BaseDialog.Root open={navOpen} onOpenChange={setNavOpen}>
        <BaseDialog.Portal>
          <BaseDialog.Backdrop className="overlay-backdrop" />
          <BaseDialog.Popup className="drawer-popup" data-side="left">
            <div className="drawer-header">
              <BaseDialog.Title className="sr-only">{t('站点导航')}</BaseDialog.Title>
              <Brand />
              <BaseDialog.Close render={<IconButton label={t('关闭站点导航')} />}>
                <X size={18} aria-hidden />
              </BaseDialog.Close>
            </div>
            <nav className="site-drawer-nav" aria-label={t('站点导航')}>
              {[...publicNav, ...resourceNav].map((item) => (
                <Link
                  key={item.to}
                  to={item.to}
                  data-status={isActive(item, pathname) ? 'active' : undefined}
                >
                  <item.icon size={17} aria-hidden />
                  {t(item.label)}
                </Link>
              ))}
            </nav>
            <div className="site-drawer-account">
              {signedIn ? (
                <Link to="/dashboard" className="button button-primary">
                  {t('进入控制台')}
                </Link>
              ) : (
                <>
                  <Link to="/sign-in" className="button button-secondary">
                    {t('登录')}
                  </Link>
                  <Link to="/sign-up" className="button button-primary">
                    {t('注册')}
                  </Link>
                </>
              )}
            </div>
          </BaseDialog.Popup>
        </BaseDialog.Portal>
      </BaseDialog.Root>
      <CommandPalette
        open={paletteOpen}
        onOpenChange={setPaletteOpen}
        role={session.data?.role}
        actions={actions}
        publicOnly={!signedIn}
      />
    </div>
  )
}
