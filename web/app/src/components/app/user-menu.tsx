import { Menu } from '@base-ui/react/menu'
import { useNavigate } from '@tanstack/react-router'
import { Languages, LogOut, Monitor, Moon, Sun, UserCircle, Wallet } from 'lucide-react'
import { useTranslation } from '../../lib/i18n'
import { useTheme, type ThemePreference } from '../../lib/preferences'
import { LanguageItems } from './language-menu'

const themeOptions: readonly { value: ThemePreference; label: string; icon: typeof Sun }[] = [
  { value: 'light', label: '浅色', icon: Sun },
  { value: 'dark', label: '深色', icon: Moon },
  { value: 'system', label: '跟随系统', icon: Monitor },
]

export function UserMenu(props: {
  name: string
  email?: string
  role?: string
  onLogout: () => void
  loggingOut?: boolean
}) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { theme, setTheme } = useTheme()
  const initial = (props.name.trim()[0] ?? '?').toUpperCase()
  return (
    <Menu.Root>
      <Menu.Trigger className="avatar-button" aria-label={t('账户菜单')}>
        <span className="avatar" aria-hidden>
          {initial}
        </span>
        <span className="avatar-name">{props.name}</span>
      </Menu.Trigger>
      <Menu.Portal>
        <Menu.Positioner sideOffset={6} align="end">
          <Menu.Popup className="menu-popup">
            <div className="menu-user">
              <strong>{props.name}</strong>
              {props.email && <span>{props.email}</span>}
              {props.role && props.role !== 'user' && <span>{t(props.role)}</span>}
            </div>
            <div className="menu-separator" />
            <Menu.Item className="menu-item" onClick={() => void navigate({ to: '/profile' })}>
              <UserCircle size={16} aria-hidden />
              {t('个人资料')}
            </Menu.Item>
            <Menu.Item className="menu-item" onClick={() => void navigate({ to: '/wallet' })}>
              <Wallet size={16} aria-hidden />
              {t('钱包')}
            </Menu.Item>
            <div className="menu-separator" />
            <div className="menu-label">{t('外观')}</div>
            <Menu.RadioGroup
              value={theme}
              onValueChange={(value) => setTheme(value as ThemePreference)}
            >
              {themeOptions.map((option) => (
                <Menu.RadioItem key={option.value} value={option.value} className="menu-item">
                  <option.icon size={16} aria-hidden />
                  {t(option.label)}
                  <Menu.RadioItemIndicator style={{ marginInlineStart: 'auto' }}>
                    ✓
                  </Menu.RadioItemIndicator>
                </Menu.RadioItem>
              ))}
            </Menu.RadioGroup>
            <Menu.SubmenuRoot>
              <Menu.SubmenuTrigger className="menu-item">
                <Languages size={16} aria-hidden />
                {t('语言')}
              </Menu.SubmenuTrigger>
              <Menu.Portal>
                <Menu.Positioner sideOffset={4}>
                  <Menu.Popup className="menu-popup language-popup">
                    <LanguageItems />
                  </Menu.Popup>
                </Menu.Positioner>
              </Menu.Portal>
            </Menu.SubmenuRoot>
            <div className="menu-separator" />
            <Menu.Item
              className="menu-item"
              data-danger
              disabled={props.loggingOut}
              onClick={props.onLogout}
            >
              <LogOut size={16} aria-hidden />
              {t('退出登录')}
            </Menu.Item>
          </Menu.Popup>
        </Menu.Positioner>
      </Menu.Portal>
    </Menu.Root>
  )
}
