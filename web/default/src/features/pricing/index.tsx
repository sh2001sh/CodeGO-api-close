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
import { lazy, Suspense, useCallback, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { getPublicPageSeoEntry } from '@/lib/public-page-seo'
import { Tabs, TabsContent } from '@/components/ui/tabs'
import { PublicLayout } from '@/components/layout'
import { PageTransition } from '@/components/page-transition'
import { SiteSeo } from '@/components/seo'
import { useMarketplaceGroups } from '@/features/marketplace/hooks'
import {
  LoadingSkeleton,
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

const ModelDetailsDrawer = lazy(async () => ({
  default: (await import('./components/model-details')).ModelDetailsDrawer,
}))

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
      source: '',
      provider: '',
      status: '',
      verification: '',
      sort: 'score',
      direction: 'desc',
      window_hours: 24,
      page: 1,
      page_size: 50,
    }),
    []
  )
  const marketplaceQuery = useMarketplaceGroups(marketplaceFilters, {
    enabled: sourceView === 'third_party',
  })
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
    () =>
      buildThirdPartyPricingModels(
        thirdPartyGroups,
        pricing.models || [],
        pricing.pricedModelDetails || []
      ),
    [pricing.models, pricing.pricedModelDetails, thirdPartyGroups]
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
    <PublicLayout showMainContainer={false} headerProps={{ className: 'demo-reference-header' }}>
      <SiteSeo
        title={pricingSeo.title}
        description={pricingSeo.description}
        keywords={pricingSeo.keywords}
        canonicalPath={pricingSeo.path}
        ogType='website'
      />
      <PageTransition className='demo-models-page'>
        <div className='wrap'>
          <header className='mhead'>
            <div>
              <div className='kick'><span>D·01</span> MODEL PLAZA</div>
              <h1>模型广场，<em>皆有价签</em>。</h1>
              <SearchBar
                value={filters.searchInput}
                onChange={filters.setSearchInput}
                onClear={filters.clearSearch}
                placeholder={
                  sourceView === 'official'
                    ? t('搜索模型名称、供应商、端点或标签')
                    : t('搜索第三方模型、来源或分组')
                }
                className='model-search'
              />
            </div>
            <div className='sum'>
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
              {marketplaceQuery.isLoading ? (
                <LoadingSkeleton viewMode={thirdPartyFilters.viewMode} />
              ) : (
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
              )}
            </TabsContent>
          </Tabs>
        </div>

        {selectedModel && (
          <Suspense fallback={null}>
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
          </Suspense>
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
