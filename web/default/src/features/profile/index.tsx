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
import i18n from '@/i18n/config'
import { useTranslation, Trans } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { useStatus } from '@/hooks/use-status'
import { Alert, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Main } from '@/components/layout'
import {
  CardStaggerContainer,
  CardStaggerItem,
} from '@/components/page-transition'
import { SiteSeo } from '@/components/seo'
import { CheckinCalendarCard } from './components/checkin-calendar-card'
import { LanguagePreferencesCard } from './components/language-preferences-card'
import { PasskeyCard } from './components/passkey-card'
import { ProfileHeader } from './components/profile-header'
import { ProfileSecurityCard } from './components/profile-security-card'
import { ProfileSettingsCard } from './components/profile-settings-card'
import { SidebarModulesCard } from './components/sidebar-modules-card'
import { TwoFACard } from './components/two-fa-card'
import { useProfile } from './hooks'
import './profile.css'

export function Profile() {
  const { t } = useTranslation()
  const { profile, loading, refreshProfile, fetchError, fetchProfile } =
    useProfile()
  const { status } = useStatus()
  const permissions = useAuthStore((s) => s.auth.user?.permissions)

  const checkinEnabled = status?.checkin_enabled === true
  const turnstileEnabled = !!(
    status?.turnstile_check && status?.turnstile_site_key
  )
  const turnstileSiteKey = status?.turnstile_site_key || ''
  const canConfigureSidebar = permissions?.sidebar_settings !== false

  return (
    <Main className='market-profile'>
      <SiteSeo
        title={i18n.t('个人资料')}
        description={i18n.t('个人资料')}
        canonicalPath='/profile'
        robots='noindex,follow'
      />
      <div className='codego-page-intro shrink-0 px-4 pt-6 pb-6 sm:px-7 sm:pt-8 sm:pb-7'>
        <h1 className='profile-page-title'>
          <Trans i18nKey={'个人资料'} />
        </h1>
      </div>
      <div className='codego-page-content min-h-0 flex-1 overflow-auto px-4 pt-5 pb-6 sm:px-7 sm:pt-6 sm:pb-8'>
        <CardStaggerContainer className='mx-auto flex w-full max-w-[1440px] flex-col gap-4 sm:gap-6'>
          {fetchError && (
            <Alert
              variant='destructive'
              className='flex flex-wrap items-center justify-between gap-4'
            >
              <AlertTitle>{fetchError}</AlertTitle>
              <Button
                variant='outline'
                disabled={loading}
                onClick={() => void fetchProfile()}
              >
                {t('Retry')}
              </Button>
            </Alert>
          )}
          <CardStaggerItem>
            <ProfileHeader profile={profile} loading={loading} />
          </CardStaggerItem>

          <CardStaggerItem>
            <div className='profile-layout'>
              <section
                className='profile-settings-column'
                aria-label={t('个人资料')}
              >
                <ProfileSettingsCard
                  profile={profile}
                  loading={loading}
                  onProfileUpdate={refreshProfile}
                />
                <LanguagePreferencesCard
                  profile={profile}
                  onProfileUpdate={refreshProfile}
                />
                <ProfileSecurityCard profile={profile} loading={loading} />
                <div className='profile-security-methods'>
                  <PasskeyCard loading={loading} />
                  <TwoFACard loading={loading} />
                </div>
              </section>

              <aside className='profile-preferences-column'>
                {checkinEnabled && (
                  <CheckinCalendarCard
                    checkinEnabled={checkinEnabled}
                    turnstileEnabled={turnstileEnabled}
                    turnstileSiteKey={turnstileSiteKey}
                  />
                )}
                {canConfigureSidebar && <SidebarModulesCard />}
              </aside>
            </div>
          </CardStaggerItem>
        </CardStaggerContainer>
      </div>
    </Main>
  )
}
