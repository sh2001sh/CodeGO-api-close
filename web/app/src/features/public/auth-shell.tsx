import type { ReactNode } from 'react'
import { Moon, Sun } from 'lucide-react'
import { useTranslation } from '../../lib/i18n'
import { useTheme } from '../../lib/preferences'
import { IconButton } from '../../components/ui'
import { Brand, CompanyIdentity } from '../../components/app/global-topbar'
import { LanguageMenu } from '../../components/app/language-menu'

/**
 * Split layout shared by sign-in, sign-up, password recovery and the OAuth
 * callback page. Left side carries the brand artwork; right side holds
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
        <img
          src="/brand/codego-routing-small.webp"
          alt=""
          width={768}
          height={512}
          decoding="async"
          style={{ width: '100%', height: 'auto' }}
        />
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
