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
import { useTranslation } from 'react-i18next'
import { getPublicPageSeoEntry } from '@/lib/public-page-seo'
import { Tabs, TabsContent } from '@/components/ui/tabs'
import { PublicLayout } from '@/components/layout'
import { PageTransition } from '@/components/page-transition'
import { SiteSeo } from '@/components/seo'
import { useMarketplaceGroups } from '@/features/marketplace/hooks'
import {
  LoadingSkeleton,
  ModelDetailsDrawer,
  OfficialModelDirectory,
  PricingSourceNavigation,
  SearchBar,
  type PricingSourceView,
} from './components'
import { EXCLUDED_GROUPS } from './constants'
import { useFilters } from './hooks/use-filters'
import { usePricingData } from './hooks/use-pricing-data'
import { countFreeModels } from './lib/model-helpers'
import {
  buildThirdPartyPricingModels,
  buildThirdPartyVendors,
} from './lib/third-party-models'

const pricingSeo = getPublicPageSeoEntry('/pricing')

export function Pricing() {
  const { t } = useTranslation()
  const [sourceView, setSourceView] = useState<PricingSourceView>('official')
  const [selectedModelName, setSelectedModelName] = useState<string | null>(
    null
  )
  const pricing = usePricingData()

  const marketplaceFilters = useMemo(
    () => ({
      search: '',
      model: '',
      status: '',
      sort: 'score',
      direction: 'desc',
      window_hours: 24,
      page: 1,
      page_size: 50,
    }),
    []
  )
  const marketplaceQuery = useMarketplaceGroups(marketplaceFilters)
  const thirdPartyGroups = useMemo(
    () =>
      (marketplaceQuery.data?.items ?? []).filter(
        (group) =>
          group.source_type === 'marketplace_user' &&
          ['active', 'degraded'].includes(group.lifecycle_status) &&
          group.verification_status === 'passed'
      ),
    [marketplaceQuery.data?.items]
  )
  const thirdPartyModels = useMemo(
    () => buildThirdPartyPricingModels(thirdPartyGroups, pricing.models || []),
    [pricing.models, thirdPartyGroups]
  )
  const thirdPartyVendors = useMemo(
    () => buildThirdPartyVendors(thirdPartyModels, pricing.vendors || []),
    [pricing.vendors, thirdPartyModels]
  )
  const officialFilters = useFilters(pricing.models || [])
  const thirdPartyFilters = useFilters(thirdPartyModels)
  const filters =
    sourceView === 'official' ? officialFilters : thirdPartyFilters

  const selectedModel = useMemo(
    () =>
      selectedModelName
        ? (sourceView === 'official' ? pricing.models : thirdPartyModels).find(
            (model) => model.model_name === selectedModelName
          ) || null
        : null,
    [pricing.models, selectedModelName, sourceView, thirdPartyModels]
  )
  const availableGroups = useMemo(
    () =>
      Object.keys(pricing.usableGroup || {}).filter(
        (group) => !EXCLUDED_GROUPS.includes(group)
      ),
    [pricing.usableGroup]
  )
  const totalFreeModels = useMemo(
    () => countFreeModels(pricing.models || [], pricing.groupRatio || {}),
    [pricing.groupRatio, pricing.models]
  )
  const visibleFreeModels = useMemo(
    () => countFreeModels(filters.filteredModels, pricing.groupRatio || {}),
    [filters.filteredModels, pricing.groupRatio]
  )
  const thirdPartyFreeModels = useMemo(
    () => countFreeModels(thirdPartyModels, {}),
    [thirdPartyModels]
  )
  const visibleThirdPartyFreeModels = useMemo(
    () => countFreeModels(thirdPartyFilters.filteredModels, {}),
    [thirdPartyFilters.filteredModels]
  )
  const activeGroupLabel =
    !filters.groupFilter || filters.groupFilter === 'all'
      ? undefined
      : filters.groupFilter

  const handleModelClick = useCallback((modelName: string) => {
    setSelectedModelName(modelName)
  }, [])
  const sourceDescription = {
    official: t('浏览 CodeGo 官方维护的模型、价格和可用分组。'),
    third_party: t('比较第三方渠道的模型覆盖、倍率和真实可用率。'),
  }[sourceView]

  if (pricing.isLoading) {
    return (
      <PublicLayout showMainContainer={false}>
        <div className='mx-auto w-full max-w-[1800px] px-3 pt-16 pb-8 sm:px-6 sm:pt-20 sm:pb-10 xl:px-8'>
          <LoadingSkeleton viewMode={filters.viewMode} />
        </div>
      </PublicLayout>
    )
  }

  return (
    <PublicLayout showMainContainer={false}>
      <SiteSeo
        title={pricingSeo.title}
        description={pricingSeo.description}
        keywords={pricingSeo.keywords}
        canonicalPath={pricingSeo.path}
        ogType='website'
      />
      <PageTransition className='public-topbar-spacer mx-auto w-full max-w-[1800px] px-3 pb-8 sm:px-6 sm:pb-10 xl:px-8'>
        <div className='mx-auto max-w-7xl space-y-4 sm:space-y-5'>
          <header className='border-border bg-card grid gap-5 rounded-lg border p-5 sm:p-7 lg:grid-cols-[minmax(0,1fr)_340px] lg:items-end'>
            <div>
              <p className='text-muted-foreground mb-3 text-sm font-medium'>
                {t('模型目录与路由')}
              </p>
              <h1 className='text-foreground max-w-3xl text-4xl leading-tight font-semibold tracking-[0] sm:text-5xl'>
                {t('Model Square')}
              </h1>
              <p className='text-muted-foreground mt-4 max-w-2xl text-base leading-7'>
                {sourceDescription}
              </p>
              <SearchBar
                value={filters.searchInput}
                onChange={filters.setSearchInput}
                onClear={filters.clearSearch}
                placeholder={
                  sourceView === 'official'
                    ? t('搜索模型名称、供应商、端点或标签')
                    : t('搜索第三方模型、来源或分组')
                }
                className='mt-6 max-w-2xl'
              />
            </div>
            <div className='border-border/70 bg-background grid grid-cols-2 gap-2 rounded-lg border p-3'>
              <HeaderMetric
                value={pricing.models.length}
                label={t('官方模型')}
              />
              <HeaderMetric
                value={thirdPartyModels.length}
                label={t('第三方模型')}
              />
            </div>
          </header>

          <Tabs
            value={sourceView}
            onValueChange={(value) => setSourceView(value as PricingSourceView)}
          >
            <PricingSourceNavigation
              officialCount={pricing.models.length}
              thirdPartyCount={thirdPartyModels.length}
            />
            <TabsContent value='official'>
              <OfficialModelDirectory
                models={pricing.models}
                vendors={pricing.vendors}
                availableGroups={availableGroups}
                groupRatio={pricing.groupRatio}
                totalFreeModels={totalFreeModels}
                visibleFreeModels={visibleFreeModels}
                activeGroupLabel={activeGroupLabel}
                priceRate={pricing.priceRate}
                usdExchangeRate={pricing.usdExchangeRate}
                filters={filters}
                onModelClick={handleModelClick}
              />
            </TabsContent>
            <TabsContent value='third_party'>
              <OfficialModelDirectory
                models={thirdPartyModels}
                vendors={thirdPartyVendors}
                availableGroups={Array.from(
                  new Set(
                    thirdPartyModels.flatMap((model) => model.enable_groups)
                  )
                )}
                groupRatio={Object.assign(
                  {},
                  ...thirdPartyModels.map((model) => model.group_ratio ?? {})
                )}
                totalFreeModels={thirdPartyFreeModels}
                visibleFreeModels={visibleThirdPartyFreeModels}
                priceRate={pricing.priceRate}
                usdExchangeRate={pricing.usdExchangeRate}
                filters={thirdPartyFilters}
                onModelClick={handleModelClick}
              />
            </TabsContent>
          </Tabs>
        </div>

        {selectedModel && (
          <ModelDetailsDrawer
            open
            onOpenChange={(open) => !open && setSelectedModelName(null)}
            model={selectedModel}
            groupRatio={pricing.groupRatio}
            usableGroup={pricing.usableGroup}
            endpointMap={pricing.endpointMap}
            autoGroups={pricing.autoGroups}
            priceRate={pricing.priceRate}
            usdExchangeRate={pricing.usdExchangeRate}
            tokenUnit={filters.tokenUnit}
            showRechargePrice={filters.showRechargePrice}
          />
        )}
      </PageTransition>
    </PublicLayout>
  )
}

function HeaderMetric(props: { value: number; label: string }) {
  return (
    <div className='text-center'>
      <div className='text-foreground text-xl font-semibold tabular-nums'>
        {props.value}
      </div>
      <div className='text-muted-foreground mt-1 text-xs'>{props.label}</div>
    </div>
  )
}
