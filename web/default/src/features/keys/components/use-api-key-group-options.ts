import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { getUserGroups } from '@/lib/api'
import { getMarketplaceAutoRoutePool } from '@/features/marketplace/api'
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
  const { data: autoPool } = useQuery({
    queryKey: ['api-key-marketplace-auto-pool'],
    queryFn: getMarketplaceAutoRoutePool,
    staleTime: 60 * 1000,
  })

  return useMemo<ApiKeyGroupOption[]>(() => {
    const officialGroups = Object.entries(groupsData?.data ?? {}).map(
      ([key, info]) => ({
        value: key,
        label: key,
        desc: info.desc || key,
        ratio: info.ratio,
        category: 'official' as const,
      })
    )
    const autoOption: ApiKeyGroupOption = {
      value: 'market:auto',
      label: '第三方 Auto',
      desc:
        autoPool && autoPool.selected_count > 0
          ? `从路由池中的 ${autoPool.selected_count} 个第三方分组自动选择 · 仅使用通用额度`
          : '请在创建 API Key 时配置第三方路由池 · 仅使用通用额度',
      ratio: '动态',
      category: 'marketplace_auto',
      disabled: !autoPool || autoPool.selected_count === 0,
      models: Array.from(
        new Set(
          (autoPool?.items ?? [])
            .filter((item) => item.selected)
            .flatMap((item) => item.models)
        )
      ).sort((left, right) => left.localeCompare(right)),
    }
    return [...officialGroups, autoOption, ...marketplaceGroups]
  }, [autoPool, groupsData?.data, marketplaceGroups])
}
