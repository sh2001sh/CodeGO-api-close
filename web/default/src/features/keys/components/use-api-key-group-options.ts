import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { getUserGroups } from '@/lib/api'
import { getMarketplaceKeyGroupOptions } from '../api'
import { toApiKeyGroupOptions } from '../lib/marketplace-group-options'
import type { ApiKeyGroupOption } from './api-key-group-combobox'

export function useApiKeyGroupOptions() {
  const { t } = useTranslation()
  const userID = useAuthStore((state) => state.auth.user?.id)
  const official = useQuery({
    queryKey: ['user-groups', userID],
    queryFn: async () => {
      const result = await getUserGroups()
      if (!result.success || !result.data) {
        throw new Error(result.message || 'Failed to load groups')
      }
      return result
    },
    staleTime: 5 * 60 * 1000,
  })
  const marketplace = useQuery({
    queryKey: ['api-key-group-options', userID],
    queryFn: getMarketplaceKeyGroupOptions,
    staleTime: 60 * 1000,
  })
  const options = useMemo<ApiKeyGroupOption[]>(() => {
    const officialGroups: ApiKeyGroupOption[] = Object.entries(
      official.data?.data ?? {}
    )
      .filter(([key]) => key.trim().toLowerCase() !== 'auto')
      .filter(([key]) => key.trim().toLowerCase() !== 'default')
      .map(([key, info]) => ({
        value: key,
        label: key,
        desc: info.desc || key,
        ratio: info.ratio,
        subscriptionEnabled: info.subscription_enabled,
        subscriptionRatio: info.subscription_ratio,
        category: 'official',
      }))
    return [
      ...toApiKeyGroupOptions(marketplace.data ?? [], t),
      ...officialGroups,
    ]
  }, [official.data?.data, marketplace.data, t])

  return {
    options,
    isPending: official.isPending || marketplace.isPending,
    error: official.error || marketplace.error,
    refetch: () => Promise.all([official.refetch(), marketplace.refetch()]),
  }
}
