import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { getUserGroups } from '@/lib/api'
import { getMarketplaceRoutePools } from '@/features/marketplace/api'
import { getSelectableMarketplaceGroups } from '../api'
import { getSidebarGroupStatus } from '@/features/sidebar-group-status/api'
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
        successRate: groupStatus?.data?.find((item) => item.group === key)?.success_rate,
        requestCount: groupStatus?.data?.find((item) => item.group === key)?.request_count,
      }))
    const poolOptions: ApiKeyGroupOption[] = routePools.map((pool) => ({
      value: pool.token_group,
      label: pool.name,
      desc: `${pool.member_count} 个分组 · ${pool.models.length} 个模型`,
      ratio: '动态', category: 'marketplace_pool', models: pool.models,
    }))
    return [...poolOptions, ...officialGroups, ...marketplaceGroups]
  }, [groupStatus?.data, groupsData?.data, marketplaceGroups, routePools])
}
