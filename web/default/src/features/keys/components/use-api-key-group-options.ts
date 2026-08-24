import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { getUserGroups } from '@/lib/api'
import { getMarketplaceAutoRoutePool } from '@/features/marketplace/api'
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
  const { data: autoPool } = useQuery({
    queryKey: ['api-key-marketplace-auto-pool'],
    queryFn: getMarketplaceAutoRoutePool,
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
    const autoOption: ApiKeyGroupOption = {
      value: 'auto',
      label: 'Auto',
      desc:
        autoPool && autoPool.selected_count > 0
          ? `从全局路由池中的 ${autoPool.selected_count} 个分组自动选择`
          : '保留系统 Auto 策略，或配置全局路由池以指定官方与第三方分组',
      ratio: '动态',
      category: 'marketplace_auto',
      models: Array.from(
        new Set(
          (autoPool?.items ?? [])
            .filter((item) => item.selected)
            .flatMap((item) => item.models)
        )
      ).sort((left, right) => left.localeCompare(right)),
    }
    return [autoOption, ...officialGroups, ...marketplaceGroups]
  }, [autoPool, groupStatus?.data, groupsData?.data, marketplaceGroups])
}
