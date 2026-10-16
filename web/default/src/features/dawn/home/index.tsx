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

For commercial licensing, please contact support@quantumnous.com.
*/
import { useTranslation } from 'react-i18next'
import { normalizeSystemName } from '@/lib/branding'
import { Markdown } from '@/components/ui/markdown'
import { SiteSeo } from '@/components/seo'
import { SiteFooter } from '@/components/site-footer'
import { useHomePageContent } from '@/features/home/hooks'
import { DawnNav } from '../components/dawn-nav'
import { ImmersiveCurtainMarketHome } from './market-home'

export function DawnHome() {
  const { t } = useTranslation()
  const home = useHomePageContent()
  const brand = normalizeSystemName()

  if (!home.isLoaded) {
    return (
      <div
        className='dawn market-home flex min-h-screen items-center justify-center'
        role='status'
        aria-live='polite'
      >
        <span>{t('加载中')}</span>
      </div>
    )
  }

  if (home.content) {
    return (
      <div className='dawn market-home min-h-screen'>
        <DawnNav />
        <main id='content' tabIndex={-1}>
          {home.isUrl ? (
            <iframe
              src={home.content}
              className='h-screen w-full border-none'
              title={brand}
            />
          ) : (
            <div className='mx-auto max-w-5xl px-6 py-12'>
              <Markdown className='custom-home-content'>
                {home.content}
              </Markdown>
            </div>
          )}
        </main>
        <SiteFooter />
      </div>
    )
  }

  return (
    <>
      <SiteSeo
        title={t('marketHome.pageTitle')}
        description={t('marketHome.metaDescription')}
        canonicalPath='/'
      />
      <ImmersiveCurtainMarketHome />
    </>
  )
}
