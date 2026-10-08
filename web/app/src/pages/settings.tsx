import { useState } from 'react'
import { useSuspenseQuery } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import { resourceOptions } from '../lib/queries'
import { useTranslation } from '../lib/i18n'
import { PageHeader } from '../components/ui'
import { settingGroups, settingFields } from '../features/settings/registry'
import { matchesSearch } from '../features/settings/search'
import { SettingsNav, type SettingsNavItem } from '../features/settings/settings-nav'
import { SettingGroupForm } from '../features/settings/group-form'
import { RawSettingsEditor } from '../features/settings/raw-settings'

const navItems: readonly SettingsNavItem[] = [
  ...settingGroups,
  { id: 'raw', label: '高级 / 原始配置', description: '未纳入分组表单的配置键' },
]

export default function SettingsPage() {
  const { t } = useTranslation()
  const { data } = useSuspenseQuery(
    resourceOptions('settings', (signal) =>
      api.GET('/api/settings', { signal }).then((result) => unwrap(result)),
    ),
  )
  const [active, setActive] = useState<string>('site')
  const [query, setQuery] = useState('')

  const filtered = query.trim()
    ? settingFields.filter((field) => matchesSearch(field, query, t))
    : null

  return (
    <>
      <PageHeader title="系统设置" description="按分区管理站点配置；搜索可跨分区查找设置项。" />
      <label className="field settings-search" htmlFor="settings-search">
        <span>{t('搜索')}</span>
        <input
          id="settings-search"
          type="search"
          placeholder={t('按名称或配置键搜索')}
          value={query}
          onChange={(event) => setQuery(event.target.value)}
        />
      </label>
      {filtered ? (
        <div className="settings-content">
          <SettingGroupForm
            group={{
              id: 'site',
              label: '搜索结果',
              description: `${t('匹配 ')}${filtered.length}${t(' 项配置')}`,
            }}
            fields={filtered}
            settings={data ?? []}
          />
        </div>
      ) : (
        <div className="settings-layout">
          <SettingsNav items={navItems} active={active} onChange={setActive} />
          <div className="settings-content">
            {active === 'raw' ? (
              <RawSettingsEditor settings={data ?? []} />
            ) : (
              <SettingGroupForm
                group={settingGroups.find((group) => group.id === active) ?? settingGroups[0]}
                fields={settingFields.filter((field) => field.group === active)}
                settings={data ?? []}
              />
            )}
          </div>
        </div>
      )}
    </>
  )
}
