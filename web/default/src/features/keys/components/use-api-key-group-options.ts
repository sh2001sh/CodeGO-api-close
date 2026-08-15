import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { getUserGroups } from '@/lib/api'
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
    return [...officialGroups, ...marketplaceGroups]
  }, [groupsData?.data, marketplaceGroups])
}
