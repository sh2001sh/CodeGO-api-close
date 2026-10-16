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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { SectionPageLayout } from '@/components/layout'
import { RoutePoolsContent } from '@/features/route-pools'
import './admin-workspace.css'
import './channels.css'
import { ChannelsDialogs } from './components/channels-dialogs'
import { ChannelsPrimaryButtons } from './components/channels-primary-buttons'
import { ChannelsProvider } from './components/channels-provider'
import { ChannelsTable } from './components/channels-table'

export function Channels() {
  const { t } = useTranslation()
  const [section, setSection] = useState('channels')
  const isSuperAdmin = useAuthStore(
    (state) => state.auth.user?.role === ROLE.SUPER_ADMIN
  )
  const showRouting = isSuperAdmin

  return (
    <ChannelsProvider>
      <div className='admin-workspace channels-workspace'>
        <SectionPageLayout kicker=''>
          <SectionPageLayout.Title>
            {showRouting ? t('渠道与智能路由') : t('Channels')}
          </SectionPageLayout.Title>
          <SectionPageLayout.Actions>
            <ChannelsPrimaryButtons />
          </SectionPageLayout.Actions>
          <SectionPageLayout.Content>
            {showRouting ? (
              <Tabs
                value={section}
                onValueChange={setSection}
                className='channel-resource-tabs'
              >
                <TabsList aria-label={t('渠道与智能路由')}>
                  <TabsTrigger value='channels'>{t('渠道')}</TabsTrigger>
                  <TabsTrigger value='routing'>{t('自动路由')}</TabsTrigger>
                </TabsList>
                <TabsContent value='channels'>
                  <ChannelsTable />
                </TabsContent>
                <TabsContent value='routing'>
                  <RoutePoolsContent />
                </TabsContent>
              </Tabs>
            ) : (
              <ChannelsTable />
            )}
          </SectionPageLayout.Content>
        </SectionPageLayout>
      </div>

      <ChannelsDialogs />
    </ChannelsProvider>
  )
}
