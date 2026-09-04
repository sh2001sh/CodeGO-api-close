import type { useFilters } from '../hooks/use-filters'
import type { PricingModel, PricingVendor, TokenUnit } from '../types'
import { EmptyState } from './empty-state'
import { ModelCardGrid } from './model-card-grid'
import { PricingMarketHighlight } from './pricing-market-highlight'
import { PricingSidebar } from './pricing-sidebar'
import { PricingTable } from './pricing-table'
import { PricingToolbar } from './pricing-toolbar'

type PricingFilters = ReturnType<typeof useFilters>

export function OfficialModelDirectory(props: {
  models: PricingModel[]
  vendors: PricingVendor[]
  availableGroups: string[]
  groupRatio: Record<string, number>
  totalFreeModels: number
  visibleFreeModels: number
  activeGroupLabel?: string
  priceRate: number
  usdExchangeRate: number
  filters: PricingFilters
  onModelClick: (modelName: string) => void
}) {
  const filters = props.filters
  const renderContent = () => {
    if (filters.filteredModels.length === 0) {
      return (
        <EmptyState
          searchQuery={filters.searchInput}
          hasActiveFilters={filters.hasActiveFilters}
          onClearFilters={() => {
            filters.clearFilters()
            filters.clearSearch()
          }}
        />
      )
    }
    if (filters.viewMode === 'card') {
      return (
        <ModelCardGrid
          models={filters.filteredModels}
          onModelClick={props.onModelClick}
          priceRate={props.priceRate}
          usdExchangeRate={props.usdExchangeRate}
          tokenUnit={filters.tokenUnit as TokenUnit}
          showRechargePrice={filters.showRechargePrice}
          groupRatios={props.groupRatio}
        />
      )
    }
    return (
      <PricingTable
        models={filters.filteredModels}
        priceRate={props.priceRate}
        usdExchangeRate={props.usdExchangeRate}
        tokenUnit={filters.tokenUnit as TokenUnit}
        showRechargePrice={filters.showRechargePrice}
        onModelClick={props.onModelClick}
      />
    )
  }

  return (
    <div className='space-y-4'>
      <PricingMarketHighlight
        totalCount={props.models.length}
        freeCount={props.totalFreeModels}
        visibleFreeCount={props.visibleFreeModels}
        activeGroupLabel={props.activeGroupLabel}
      />
      <div className='grid gap-4 xl:grid-cols-[300px_minmax(0,1fr)]'>
        <PricingSidebar
          quotaTypeFilter={filters.quotaTypeFilter}
          endpointTypeFilter={filters.endpointTypeFilter}
          vendorFilter={filters.vendorFilter}
          groupFilter={filters.groupFilter}
          tagFilter={filters.tagFilter}
          onQuotaTypeChange={filters.setQuotaTypeFilter}
          onEndpointTypeChange={filters.setEndpointTypeFilter}
          onVendorChange={filters.setVendorFilter}
          onGroupChange={filters.setGroupFilter}
          onTagChange={filters.setTagFilter}
          vendors={props.vendors}
          groups={props.availableGroups}
          groupRatios={props.groupRatio}
          tags={filters.availableTags}
          models={props.models}
          hasActiveFilters={filters.hasActiveFilters}
          onClearFilters={filters.clearFilters}
          className='hover-scrollbar sticky top-4 hidden max-h-[calc(100dvh-2rem)] self-start overflow-y-auto xl:block'
        />
        <main className='min-w-0 space-y-4'>
          <PricingToolbar
            filteredCount={filters.filteredModels.length}
            totalCount={props.models.length}
            sortBy={filters.sortBy}
            onSortChange={filters.setSortBy}
            tokenUnit={filters.tokenUnit}
            onTokenUnitChange={filters.setTokenUnit}
            showRechargePrice={filters.showRechargePrice}
            onRechargePriceChange={filters.setShowRechargePrice}
            viewMode={filters.viewMode}
            onViewModeChange={filters.setViewMode}
            quotaTypeFilter={filters.quotaTypeFilter}
            endpointTypeFilter={filters.endpointTypeFilter}
            vendorFilter={filters.vendorFilter}
            groupFilter={filters.groupFilter}
            tagFilter={filters.tagFilter}
            onQuotaTypeChange={filters.setQuotaTypeFilter}
            onEndpointTypeChange={filters.setEndpointTypeFilter}
            onVendorChange={filters.setVendorFilter}
            onGroupChange={filters.setGroupFilter}
            onTagChange={filters.setTagFilter}
            vendors={props.vendors}
            groups={props.availableGroups}
            groupRatios={props.groupRatio}
            tags={filters.availableTags}
            models={props.models}
            hasActiveFilters={filters.hasActiveFilters}
            activeFilterCount={filters.activeFilterCount}
            onClearFilters={filters.clearFilters}
          />
          {renderContent()}
        </main>
      </div>
    </div>
  )
}
