import { useMemo, useState } from 'react'
import { useDebounce } from '@/hooks'
import {
  BarChart3,
  LineChart,
  Plus,
  ShieldCheck,
  Store,
  UploadCloud,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { SectionPageLayout } from '@/components/layout'
import { AdminGovernance } from './components/admin-governance'
import { ChannelWorkspace } from './components/channel-workspace'
import { MarketSurface } from './components/market-surface'
import { MarketplaceMultiplierTrend } from './components/marketplace-multiplier-trend'
import { MarketplaceOverview } from './components/marketplace-overview'
import { useMarketplaceGroups } from './hooks'
import type { GroupFilters } from './types'

const defaultFilters: GroupFilters = {
  search: '',
  model: '',
  source: '',
  provider: '',
  status: '',
  verification: '',
  sort: 'score',
  direction: 'desc',
  window_hours: 24,
  page: 1,
  page_size: 20,
}

type MarketplaceTab = 'market' | 'ranking' | 'trend' | 'mine' | 'admin'

export function MarketplacePage() {
  const { t } = useTranslation()
  const role = useAuthStore((state) => state.auth.user?.role ?? 0)
  const isAdmin = role >= 10
  const [tab, setTab] = useState<MarketplaceTab>('market')
  const [showChannelForm, setShowChannelForm] = useState(false)
  const [filters, setFilters] = useState<GroupFilters>(defaultFilters)
  const debouncedSearch = useDebounce(filters.search, 300)
  const debouncedModel = useDebounce(filters.model, 300)
  const effectiveFilters = useMemo(
    () => ({
      ...filters,
      search: debouncedSearch,
      model: debouncedModel,
    }),
    [debouncedModel, debouncedSearch, filters]
  )
  const groups = useMarketplaceGroups(effectiveFilters)
  const updateFilters = (patch: Partial<GroupFilters>) =>
    setFilters((current) => ({ ...current, ...patch }))

  const openChannelForm = () => {
    setTab('mine')
    setShowChannelForm(true)
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('分组市场')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        {isAdmin && (
          <Button variant='outline' size='sm' onClick={() => setTab('admin')}>
            <ShieldCheck />
            {t('渠道治理')}
          </Button>
        )}
        <Button size='sm' onClick={openChannelForm}>
          <Plus />
          {t('添加渠道')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='mx-auto w-full max-w-[1800px] space-y-3'>
          <MarketplaceOverview
            total={groups.data?.total ?? 0}
            ranked={groups.data?.ranked_count ?? 0}
            multiplier={groups.data?.highlights.cheapest?.multiplier}
            updating={groups.isFetching}
          />
          <Tabs
            value={tab}
            onValueChange={(value) => {
              const nextTab = value as MarketplaceTab
              setTab(nextTab)
              if (nextTab !== 'mine') setShowChannelForm(false)
            }}
          >
            <TabsList
              variant='line'
              className='border-border h-10 w-full justify-start gap-1 overflow-x-auto border-b px-1 sm:gap-2'
            >
              <TabsTrigger
                value='market'
                className='min-w-20 px-2 sm:min-w-24 sm:px-3'
              >
                <Store />
                {t('市场分组')}
              </TabsTrigger>
              <TabsTrigger
                value='ranking'
                className='min-w-20 px-2 sm:min-w-24 sm:px-3'
              >
                <BarChart3 />
                {t('质量排行')}
              </TabsTrigger>
              <TabsTrigger
                value='trend'
                className='min-w-20 px-2 sm:min-w-24 sm:px-3'
              >
                <LineChart />
                {t('价格走势')}
              </TabsTrigger>
              <TabsTrigger
                value='mine'
                className='min-w-20 px-2 sm:min-w-24 sm:px-3'
              >
                <UploadCloud />
                {t('我的渠道')}
              </TabsTrigger>
              {isAdmin && (
                <TabsTrigger
                  value='admin'
                  className='min-w-20 px-2 sm:min-w-24 sm:px-3'
                >
                  <ShieldCheck />
                  {t('渠道治理')}
                </TabsTrigger>
              )}
            </TabsList>
            <TabsContent value='market'>
              <MarketSurface
                filters={filters}
                updateFilters={updateFilters}
                query={groups}
                summary={`${t('共 {{total}} 个公开分组', { total: groups.data?.total ?? 0 })} · ${t('{{count}} 个达到正式排名门槛', { count: groups.data?.ranked_count ?? 0 })}`}
              />
            </TabsContent>
            <TabsContent value='ranking'>
              <MarketSurface
                ranking
                filters={filters}
                updateFilters={updateFilters}
                query={groups}
                summary={t('用统一口径比较可靠性、响应性能、吞吐与调用成本。')}
              />
            </TabsContent>
            <TabsContent value='trend'>
              <MarketplaceMultiplierTrend
                model={filters.model}
                onModelChange={(model) => updateFilters({ model, page: 1 })}
              />
            </TabsContent>
            <TabsContent value='mine'>
              <ChannelWorkspace
                showForm={showChannelForm}
                onShowForm={() => setShowChannelForm(true)}
                onHideForm={() => setShowChannelForm(false)}
              />
            </TabsContent>
            {isAdmin && (
              <TabsContent value='admin'>
                <AdminGovernance />
              </TabsContent>
            )}
          </Tabs>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
