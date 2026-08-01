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
import { useEffect, useState } from 'react'
import { Info } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { SectionPageLayout } from '@/components/layout'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { BlindBoxOperationsPanel } from './components/blind-box-operations-panel'
import { DailyLuckyAdminPanel } from '@/features/daily-lucky-number/components/daily-lucky-admin-panel'
import { SubscriptionsDialogs } from './components/subscriptions-dialogs'
import { SubscriptionsPrimaryButtons } from './components/subscriptions-primary-buttons'
import {
  SubscriptionsProvider,
  useSubscriptions,
} from './components/subscriptions-provider'
import { SubscriptionsTable } from './components/subscriptions-table'

type SubscriptionsTab = 'plans' | 'blind-box' | 'daily-lucky'

function getInitialTab(): SubscriptionsTab {
  if (typeof window === 'undefined') return 'plans'
  if (window.location.hash === '#daily-lucky-admin') return 'daily-lucky'
  return window.location.hash === '#blind-box-admin' ? 'blind-box' : 'plans'
}

function setSubscriptionsHash(tab: SubscriptionsTab) {
  if (typeof window === 'undefined') return
  let hash = '#subscription-plans'
  if (tab === 'blind-box') hash = '#blind-box-admin'
  if (tab === 'daily-lucky') hash = '#daily-lucky-admin'
  if (window.location.hash === hash) return
  window.history.replaceState({}, '', `${window.location.pathname}${hash}`)
}

function SubscriptionsContent() {
  const { t } = useTranslation()
  const { complianceConfirmed } = useSubscriptions()
  const [activeTab, setActiveTab] = useState<SubscriptionsTab>(getInitialTab)

  useEffect(() => {
    if (typeof window === 'undefined') return

    const syncTabFromHash = () => {
      if (window.location.hash === '#daily-lucky-admin') {
        setActiveTab('daily-lucky')
      } else if (window.location.hash === '#blind-box-admin') {
        setActiveTab('blind-box')
      } else {
        setActiveTab('plans')
      }
    }

    syncTabFromHash()
    window.addEventListener('hashchange', syncTabFromHash)
    return () => window.removeEventListener('hashchange', syncTabFromHash)
  }, [])

  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>
          {t('Package Management')}
        </SectionPageLayout.Title>
        <SectionPageLayout.Description>
          {t('Manage subscription plan creation, pricing and status')}
        </SectionPageLayout.Description>
        <SectionPageLayout.Actions>
          <div className='flex items-center gap-2'>
            <Alert variant='default' className='hidden px-3 py-2 sm:flex'>
              <Info className='h-4 w-4' />
              <AlertDescription className='text-xs'>
                {t(
                  'Stripe/Creem requires creating products on the third-party platform and entering the ID'
                )}
              </AlertDescription>
            </Alert>
            {activeTab === 'plans' ? <SubscriptionsPrimaryButtons /> : null}
          </div>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          {!complianceConfirmed ? (
            <Alert variant='destructive' className='mb-4'>
              <AlertDescription>
                {t(
                  'Subscription plan creation and changes are locked until the administrator confirms compliance terms in Payment Gateway settings.'
                )}
              </AlertDescription>
            </Alert>
          ) : null}
          <Tabs
            value={activeTab}
            onValueChange={(value) => {
              const nextTab = value as SubscriptionsTab
              setActiveTab(nextTab)
              setSubscriptionsHash(nextTab)
            }}
            className='space-y-4'
          >
            <TabsList className='grid h-11 w-full grid-cols-3 rounded-2xl bg-slate-100 p-1 sm:w-[540px]'>
              <TabsTrigger value='plans' className='rounded-xl text-sm font-medium'>
                {t('Package Management')}
              </TabsTrigger>
              <TabsTrigger value='blind-box' className='rounded-xl text-sm font-medium'>
                {t('Blind Box Operations')}
              </TabsTrigger>
              <TabsTrigger value='daily-lucky' className='rounded-xl text-sm font-medium'>
                {t('Daily Lucky Number')}
              </TabsTrigger>
            </TabsList>

            <TabsContent value='plans' className='mt-0'>
              <div id='subscription-plans' className='scroll-mt-4'>
                <SubscriptionsTable />
              </div>
            </TabsContent>

            <TabsContent value='blind-box' className='mt-0'>
              <div id='blind-box-admin' className='scroll-mt-4'>
                <BlindBoxOperationsPanel />
              </div>
            </TabsContent>

            <TabsContent value='daily-lucky' className='mt-0'>
              <div id='daily-lucky-admin' className='scroll-mt-4'>
                <DailyLuckyAdminPanel />
              </div>
            </TabsContent>
          </Tabs>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <SubscriptionsDialogs />
    </>
  )
}

export function Subscriptions() {
  return (
    <SubscriptionsProvider>
      <SubscriptionsContent />
    </SubscriptionsProvider>
  )
}
