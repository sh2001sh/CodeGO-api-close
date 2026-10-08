import type { ReactNode } from 'react'
import { Moon, ShieldCheck, Sun, Timer, Waypoints } from 'lucide-react'
import { useTranslation } from '../../lib/i18n'
import { useTheme } from '../../lib/preferences'
import { IconButton } from '../../components/ui'
import { Brand, CompanyIdentity } from '../../components/app/global-topbar'
import { LanguageMenu } from '../../components/app/language-menu'

const bullets = [
  { icon: Waypoints, label: '一个接口，接入所有主流模型' },
  { icon: Timer, label: '实时路由' },
  { icon: ShieldCheck, label: '可验证的渠道市场' },
] as const

/**
 * Split layout shared by sign-in, sign-up, password recovery and the OAuth
 * callback page. Left side carries the value proposition; right side holds
 * the theme/language toggles above the actual form panel.
 */
export function AuthShell(props: { children: ReactNode }) {
  const { t } = useTranslation()
  const { theme, setTheme } = useTheme()
  const dark = theme === 'dark'
  return (
    <main className="auth-layout">
      <aside className="auth-aside">
        <Brand />
        <div>
          <h2>{t('选择模型，管理调用。')}</h2>
          <ul>
            {bullets.map((bullet) => (
              <li key={bullet.label}>
                <bullet.icon size={18} aria-hidden />
                <span>{t(bullet.label)}</span>
              </li>
            ))}
          </ul>
        </div>
        <CompanyIdentity />
      </aside>
      <div className="auth-main">
        <div className="auth-main-top">
          <div className="auth-mobile-brand">
            <Brand />
          </div>
          <IconButton
            label={dark ? t('切换到浅色模式') : t('切换到深色模式')}
            onClick={() => setTheme(dark ? 'light' : 'dark')}
          >
            {dark ? <Sun size={17} aria-hidden /> : <Moon size={17} aria-hidden />}
          </IconButton>
          <LanguageMenu />
        </div>
        {props.children}
        <footer className="auth-footer">
          <CompanyIdentity />
        </footer>
      </div>
    </main>
  )
}
