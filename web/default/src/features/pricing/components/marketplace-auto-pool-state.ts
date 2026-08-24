import { useMemo, useState } from 'react'
import type { MarketplaceAutoRoutePoolItem } from '@/features/marketplace/types'
import type { AutoPoolSort } from './marketplace-auto-pool-filters'
import type { AutoPoolSourceOption } from './marketplace-auto-pool-source-tabs'

const OFFICIAL_SOURCE = 'official'
const MARKETPLACE_SOURCE_PREFIX = 'marketplace:'

export function autoPoolSourceKey(item: MarketplaceAutoRoutePoolItem) {
  if (item.source_type === 'official') return OFFICIAL_SOURCE
  return `${MARKETPLACE_SOURCE_PREFIX}${item.source_label || 'pending'}`
}

function sourcePriority(option: AutoPoolSourceOption) {
  if (option.value === OFFICIAL_SOURCE) return 0
  if (option.label === 'Codex Plus') return 1
  if (option.label === 'Codex Pro') return 2
  if (option.label === 'CC-Max') return 3
  if (option.label === 'CC-Kiro') return 4
  return 10
}

function buildSourceOptions(
  items: MarketplaceAutoRoutePoolItem[],
  officialLabel: string,
  otherLabel: string
) {
  const counts = new Map<string, AutoPoolSourceOption>()
  for (const item of items) {
    const value = autoPoolSourceKey(item)
    const label =
      item.source_type === 'official'
        ? officialLabel
        : item.source_label || otherLabel
    counts.set(value, {
      value,
      label,
      count: (counts.get(value)?.count ?? 0) + 1,
    })
  }
  return Array.from(counts.values()).sort((left, right) => {
    const priority = sourcePriority(left) - sourcePriority(right)
    return priority || left.label.localeCompare(right.label, 'zh-CN')
  })
}

function filterAndSortCandidates(
  items: MarketplaceAutoRoutePoolItem[],
  filters: {
    search: string
    source: string
    model: string
    sort: AutoPoolSort
  }
) {
  const keyword = filters.search.trim().toLowerCase()
  const filtered = items.filter((item) => {
    const matchesKeyword =
      !keyword ||
      [item.system_display_name, item.source_label, ...item.models]
        .join(' ')
        .toLowerCase()
        .includes(keyword)
    const matchesSource =
      filters.source === 'all' || autoPoolSourceKey(item) === filters.source
    const matchesModel =
      filters.model === 'all' || item.models.includes(filters.model)
    return matchesKeyword && matchesSource && matchesModel
  })
  return filtered.sort((left, right) => {
    if (filters.sort === 'multiplier') return left.multiplier - right.multiplier
    if (filters.sort === 'success')
      return right.success_rate - left.success_rate
    if (filters.sort === 'cache')
      return right.cache_hit_rate - left.cache_hit_rate
    if (filters.sort === 'latency')
      return left.avg_latency_ms - right.avg_latency_ms
    return left.route_score - right.route_score
  })
}

/** Selection and priority state for the editable Auto route pool. */
export function useAutoPoolSelection(items: MarketplaceAutoRoutePoolItem[]) {
  const [draft, setDraft] = useState<string[] | null>(null)
  const serverOrder = useMemo(
    () =>
      items
        .filter((item) => item.selected)
        .sort((left, right) => left.priority - right.priority)
        .map((item) => item.group_id),
    [items]
  )
  const order = draft ?? serverOrder
  const selected = useMemo(() => new Set(order), [order])
  const unselected = useMemo(
    () => items.filter((item) => !selected.has(item.group_id)),
    [items, selected]
  )
  const byID = useMemo(
    () => new Map(items.map((item) => [item.group_id, item])),
    [items]
  )
  const routes = order
    .map((groupID) => byID.get(groupID))
    .filter((item): item is MarketplaceAutoRoutePoolItem => Boolean(item))
  const changed =
    order.length !== serverOrder.length ||
    order.some((groupID, index) => groupID !== serverOrder[index])

  return { order, selected, unselected, routes, changed, setDraft }
}

/** Search, source-tab, model, and sort state for unselected candidates. */
export function useAutoPoolCandidates(
  items: MarketplaceAutoRoutePoolItem[],
  allItems: MarketplaceAutoRoutePoolItem[],
  labels: { official: string; other: string }
) {
  const [search, setSearch] = useState('')
  const [source, setSource] = useState('all')
  const [model, setModel] = useState('all')
  const [sort, setSort] = useState<AutoPoolSort>('route')
  const sources = useMemo(
    () => buildSourceOptions(items, labels.official, labels.other),
    [items, labels.official, labels.other]
  )
  const models = useMemo(
    () => Array.from(new Set(allItems.flatMap((item) => item.models))).sort(),
    [allItems]
  )
  const visible = useMemo(
    () => filterAndSortCandidates(items, { search, source, model, sort }),
    [items, model, search, sort, source]
  )
  const resetFilters = () => {
    setSearch('')
    setModel('all')
    setSort('route')
  }
  const leaveEmptySource = (groupID: string) => {
    if (source === 'all') return
    const remaining = items.some(
      (item) => item.group_id !== groupID && autoPoolSourceKey(item) === source
    )
    if (!remaining) setSource('all')
  }

  return {
    search,
    setSearch,
    source,
    setSource,
    model,
    setModel,
    sort,
    setSort,
    sources,
    models,
    visible,
    resetFilters,
    leaveEmptySource,
  }
}
