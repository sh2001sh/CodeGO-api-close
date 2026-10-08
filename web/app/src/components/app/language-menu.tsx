import { Menu } from '@base-ui/react/menu'
import { Languages } from 'lucide-react'
import { useTranslation } from '../../lib/i18n'
import { languages } from '../../lib/locales'

export function LanguageItems() {
  const { locale, change, pending } = useTranslation()
  return (
    <Menu.RadioGroup value={locale} onValueChange={(value) => void change(String(value))}>
      {languages.map((language) => (
        <Menu.RadioItem
          key={language.code}
          value={language.code}
          className="menu-item language-item"
          disabled={pending}
          closeOnClick
        >
          <bdi lang={language.code}>{language.name}</bdi>
          <Menu.RadioItemIndicator className="language-selected" aria-hidden>
            ✓
          </Menu.RadioItemIndicator>
        </Menu.RadioItem>
      ))}
    </Menu.RadioGroup>
  )
}

export function LanguageMenu() {
  const { t, locale, pending, error } = useTranslation()
  const language = languages.find((entry) => entry.code === locale)!
  return (
    <div className="language-control">
      <Menu.Root>
        <Menu.Trigger
          className="icon-button locale-toggle"
          aria-label={`${t('选择语言')} / Language`}
          title={language.name}
          aria-busy={pending || undefined}
        >
          <Languages size={17} aria-hidden />
          <span dir="ltr">{language.short}</span>
        </Menu.Trigger>
        <Menu.Portal>
          <Menu.Positioner sideOffset={8} align="end">
            <Menu.Popup className="menu-popup language-popup" aria-label="Language">
              <LanguageItems />
            </Menu.Popup>
          </Menu.Positioner>
        </Menu.Portal>
      </Menu.Root>
      {error && (
        <span className="language-error" role="alert">
          {t('语言加载失败，请重试。')}
        </span>
      )}
    </div>
  )
}
