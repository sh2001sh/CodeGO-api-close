import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { getUserGroups } from '@/lib/api'
import { getMarketplaceRoutePools } from '@/features/marketplace/api'
import { getMarketplaceAutoRoutePool } from '@/features/marketplace/api'
import { getSidebarGroupStatus } from '@/features/sidebar-group-status/api'
import { getSelectableMarketplaceGroups } from '../api'
import type { ApiKeyGroupOption } from './api-key-group-combobox'

export function useApiKeyGroupOptions() {
  const { data: groupsData } = useQuery({
    queryKey: ['user-groups'],
    queryFn: getUserGroups,
    staleTime: 5 * 60 * 1000,
  })
  const { data: marketplaceGroups = [] } = useQuery({
    queryKey: ['selectable-marketplace-groups'],
    queryFn: getSelectableMarketplaceGroups,
    staleTime: 60 * 1000,
  })
  const { data: routePools = [] } = useQuery({
    queryKey: ['api-key-marketplace-route-pools'],
    queryFn: getMarketplaceRoutePools,
    staleTime: 60 * 1000,
    retry: false,
  })
  const { data: autoRoutePool } = useQuery({
    queryKey: ['api-key-marketplace-auto-pool'],
    queryFn: getMarketplaceAutoRoutePool,
    staleTime: 60 * 1000,
    retry: false,
  })
  const { data: groupStatus } = useQuery({
    queryKey: ['sidebar-group-status'],
    queryFn: getSidebarGroupStatus,
    staleTime: 30 * 1000,
  })

  return useMemo<ApiKeyGroupOption[]>(() => {
    const officialGroups = Object.entries(groupsData?.data ?? {})
      .filter(([key]) => key.trim().toLowerCase() !== 'auto')
      .map(([key, info]) => ({
        value: key,
        label: key,
        desc: info.desc || key,
        ratio: info.ratio,
        subscriptionEnabled: info.subscription_enabled,
        subscriptionRatio: info.subscription_ratio,
        category: 'official' as const,
        successRate: groupStatus?.data?.find((item) => item.group === key)
          ?.success_rate,
        requestCount: groupStatus?.data?.find((item) => item.group === key)
          ?.request_count,
      }))
    const poolOptions: ApiKeyGroupOption[] = routePools
      .filter((pool) => Boolean(pool.id))
      .map((pool) => ({
        // Older API responses omitted token_group; derive the stable value so
        // existing saved pools remain selectable during API rollouts.
        value: pool.token_group || `market:pool:${pool.id}`,
        label: pool.name,
        desc: `${pool.member_count} 个分组 · ${pool.models.length} 个模型`,
        ratio: '动态',
        category: 'marketplace_pool',
        models: pool.models,
      }))
    const autoOption: ApiKeyGroupOption[] = autoRoutePool
      ? [{ value: 'market:auto', label: 'AUTO 路由池', desc: `${autoRoutePool.selected_count} 个分组`, ratio: '动态', category: 'marketplace_auto', models: autoRoutePool.items.filter((item) => item.selected).flatMap((item) => item.models) }]
      : []
    return [...autoOption, ...poolOptions, ...officialGroups, ...marketplaceGroups]
  }, [autoRoutePool, groupStatus?.data, groupsData?.data, marketplaceGroups, routePools])
}
