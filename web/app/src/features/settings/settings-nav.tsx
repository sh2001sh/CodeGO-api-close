// Section navigation: a vertical rail on desktop, a native select on mobile.
// Both render from the same list so there is a single source of truth.
import { useTranslation } from '../../lib/i18n'
import type { SettingGroupDef } from './registry-types'

export type SettingsNavItem = SettingGroupDef | { id: 'raw'; label: string; description: string }

export function SettingsNav(props: {
  items: readonly SettingsNavItem[]
  active: string
  onChange: (id: string) => void
}) {
  const { t } = useTranslation()
  return (
    <>
      <nav className="settings-rail" aria-label={t('系统设置分区')}>
        {props.items.map((item) => (
          <button
            key={item.id}
            type="button"
            className="settings-rail-item"
            aria-current={item.id === props.active ? 'true' : undefined}
            onClick={() => props.onChange(item.id)}
          >
            {t(item.label)}
          </button>
        ))}
      </nav>
      <label className="field settings-mobile-nav" htmlFor="settings-mobile-nav">
        <span>{t('系统设置分区')}</span>
        <select
          id="settings-mobile-nav"
          value={props.active}
          onChange={(event) => props.onChange(event.target.value)}
        >
          {props.items.map((item) => (
            <option key={item.id} value={item.id}>
              {t(item.label)}
            </option>
          ))}
        </select>
      </label>
    </>
  )
}
