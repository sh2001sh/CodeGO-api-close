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
import { useCallback, useMemo, useState } from 'react'
import { getRouteApi, useNavigate } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { useSidebarConfig } from '@/hooks/use-sidebar-config'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { SectionPageLayout } from '@/components/layout'
import type { NavGroup } from '@/components/layout/types'
import { useMyMarketplaceChannels } from '@/features/marketplace/hooks'
import { CacheStatsDialog } from '@/features/system-settings/general/channel-affinity/cache-stats-dialog'
import { UserInfoDialog } from './components/dialogs/user-info-dialog'
import { OwnerChannelUsageLogs } from './components/owner-channel-usage-logs'
import {
  UsageLogsProvider,
  useUsageLogsContext,
} from './components/usage-logs-provider'
import { UsageLogsTable } from './components/usage-logs-table'
import {
  getUsageLogsSectionMeta,
  resolveUsageLogsSectionId,
} from './section-meta'
import {
  isUsageLogsSectionId,
  type UsageLogsSectionId,
} from './section-registry'

const route = getRouteApi('/_authenticated/usage-logs/$section')
const TASK_LOG_SECTIONS = ['task'] as const

function UsageLogsContent() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const params = route.useParams()
  const activeCategory = resolveUsageLogsSectionId(params.section)
  const pageMeta = getUsageLogsSectionMeta(activeCategory)
  const ownerChannelsQuery = useMyMarketplaceChannels()
  const ownerChannels = ownerChannelsQuery.data ?? []
  const [commonView, setCommonView] = useState<'personal' | 'channel'>(
    'personal'
  )
  const {
    selectedUserId,
    userInfoDialogOpen,
    setUserInfoDialogOpen,
    affinityTarget,
    affinityDialogOpen,
    setAffinityDialogOpen,
  } = useUsageLogsContext()
  const tabNavGroups = useMemo<NavGroup[]>(
    () => [
      {
        title: t('Task Logs'),
        items: TASK_LOG_SECTIONS.map((section) => ({
          title: t(getUsageLogsSectionMeta(section).titleKey),
          url: `/usage-logs/${section}`,
        })),
      },
    ],
    [t]
  )
  const filteredTabGroups = useSidebarConfig(tabNavGroups)
  const visibleSections = useMemo(
    () =>
      (filteredTabGroups[0]?.items ?? [])
        .map((item) => {
          if (!('url' in item) || typeof item.url !== 'string') return null
          return item.url.split('/').pop() ?? null
        })
        .filter((section): section is UsageLogsSectionId =>
          Boolean(section && isUsageLogsSectionId(section))
        ),
    [filteredTabGroups]
  )

  const handleSectionChange = useCallback(
    (section: string) => {
      void navigate({
        to: '/usage-logs/$section',
        params: { section: section as UsageLogsSectionId },
      })
    },
    [navigate]
  )

  const showTaskSwitcher =
    activeCategory !== 'common' && visibleSections.length > 1

  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>
          {t(pageMeta.titleKey)}
        </SectionPageLayout.Title>
        <SectionPageLayout.Description>
          {t(pageMeta.descriptionKey)}
        </SectionPageLayout.Description>
        <SectionPageLayout.Content>
          <div className='space-y-4'>
            {showTaskSwitcher && (
              <Tabs value={activeCategory} onValueChange={handleSectionChange}>
                <TabsList className='h-auto max-w-full flex-wrap justify-start'>
                  {visibleSections.map((section) => (
                    <TabsTrigger key={section} value={section}>
                      {t(getUsageLogsSectionMeta(section).titleKey)}
                    </TabsTrigger>
                  ))}
                </TabsList>
              </Tabs>
            )}
            {activeCategory === 'common' && ownerChannels.length > 0 && (
              <Tabs
                value={commonView}
                onValueChange={(value) =>
                  setCommonView(value as 'personal' | 'channel')
                }
              >
                <TabsList className='h-auto max-w-full flex-wrap justify-start'>
                  <TabsTrigger value='personal'>
                    {t('个人使用日志')}
                  </TabsTrigger>
                  <TabsTrigger value='channel'>{t('渠道使用日志')}</TabsTrigger>
                </TabsList>
              </Tabs>
            )}
            {activeCategory === 'common' &&
            commonView === 'channel' &&
            ownerChannels.length > 0 ? (
              <OwnerChannelUsageLogs channels={ownerChannels} />
            ) : (
              <UsageLogsTable logCategory={activeCategory} />
            )}
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <UserInfoDialog
        userId={selectedUserId}
        open={userInfoDialogOpen}
        onOpenChange={setUserInfoDialogOpen}
      />

      <CacheStatsDialog
        open={affinityDialogOpen}
        onOpenChange={setAffinityDialogOpen}
        target={
          affinityTarget
            ? {
                rule_name: affinityTarget.rule_name || '',
                using_group:
                  affinityTarget.using_group ||
                  affinityTarget.selected_group ||
                  '',
                key_hint: affinityTarget.key_hint || '',
                key_fp: affinityTarget.key_fp || '',
              }
            : null
        }
      />
    </>
  )
}

export function UsageLogs() {
  return (
    <UsageLogsProvider>
      <UsageLogsContent />
    </UsageLogsProvider>
  )
}
