import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  bindMarketplaceToken,
  createMarketplaceChannel,
  deleteMarketplaceChannel,
  fetchMarketplaceModels,
  getAdminMarketplaceChannels,
  getAdminOwnerIncome,
  getMarketplaceGroups,
  getMarketplaceMultiplierTrends,
  getMarketplaceAutoRoutePool,
  getMyMarketplaceChannels,
  getMyMarketplaceUsageLogs,
  getTokenOptions,
  pauseMarketplaceVerification,
  queueMarketplaceDetection,
  queueMarketplaceConnectivityTest,
  removeMarketplaceFailedModel,
  reviewMarketplaceChannel,
  retryMarketplaceFailedConnectivity,
  setMarketplaceChannelPaused,
  submitMarketplaceChannelFeedback,
  updateMarketplaceChannel,
  updateMarketplaceAutoRoutePool,
} from './api'
import type {
  AdminMarketplaceChannelFilters,
  GroupFilters,
  MarketplaceOwnerUsageLogFilters,
} from './types'

function verificationRefetchInterval(
  channels: {
    lifecycle_status: string
    verification_status: string
    gpt56_mapping_status?: string
    connectivity_test_status?: string
  }[]
) {
  return channels.some(
    (channel) =>
      channel.lifecycle_status === 'verifying' ||
      ['queued', 'running'].includes(channel.verification_status) ||
      ['queued', 'running'].includes(channel.gpt56_mapping_status ?? '') ||
      ['queued', 'running'].includes(channel.connectivity_test_status ?? '')
  )
    ? 1000
    : false
}

export function useMarketplaceChannelFeedback() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: submitMarketplaceChannelFeedback,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['marketplace-groups'] })
    },
  })
}

export function useMarketplaceGroups(filters: GroupFilters) {
  return useQuery({
    queryKey: ['marketplace-groups', filters],
    queryFn: () => getMarketplaceGroups(filters),
    placeholderData: (previousData) => previousData,
    staleTime: 30_000,
    refetchInterval: 60_000,
    refetchIntervalInBackground: false,
    refetchOnWindowFocus: false,
  })
}

export function useMarketplaceMultiplierTrends(
  rangeHours: number,
  model: string
) {
  return useQuery({
    queryKey: ['marketplace-multiplier-trends', rangeHours, model],
    queryFn: () => getMarketplaceMultiplierTrends({ rangeHours, model }),
    refetchInterval: 60_000,
    refetchIntervalInBackground: false,
  })
}

export function useMarketplaceAutoRoutePool(enabled = true) {
  return useQuery({
    queryKey: ['marketplace-auto-route-pool'],
    queryFn: getMarketplaceAutoRoutePool,
    enabled,
  })
}

export function useMarketplaceAutoRoutePoolUpdate() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: updateMarketplaceAutoRoutePool,
    onSuccess: async (data) => {
      queryClient.setQueryData(['marketplace-auto-route-pool'], data)
      await queryClient.invalidateQueries({
        queryKey: ['api-key-marketplace-auto-pool'],
      })
    },
  })
}

export function useMyMarketplaceChannels() {
  return useQuery({
    queryKey: ['marketplace-channels', 'mine'],
    queryFn: getMyMarketplaceChannels,
    refetchInterval: (query) =>
      verificationRefetchInterval(query.state.data ?? []),
  })
}

export function useMyMarketplaceUsageLogs(
  params: MarketplaceOwnerUsageLogFilters
) {
  return useQuery({
    queryKey: ['marketplace-channels', 'mine', 'usage-logs', params],
    queryFn: () => getMyMarketplaceUsageLogs(params),
    placeholderData: (previousData) => previousData,
  })
}

export function useMarketplaceTokens() {
  return useQuery({
    queryKey: ['marketplace-token-options'],
    queryFn: getTokenOptions,
  })
}

export function useMarketplaceMutations() {
  const queryClient = useQueryClient()
  const invalidate = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ['marketplace-groups'] }),
      queryClient.invalidateQueries({
        queryKey: ['marketplace-multiplier-trends'],
      }),
      queryClient.invalidateQueries({ queryKey: ['marketplace-channels'] }),
    ])
  }
  return {
    create: useMutation({
      mutationFn: createMarketplaceChannel,
      onSuccess: invalidate,
    }),
    fetchModels: useMutation({ mutationFn: fetchMarketplaceModels }),
    detect: useMutation({
      mutationFn: (channelId: string) => queueMarketplaceDetection(channelId),
      onSuccess: invalidate,
    }),
    testConnectivity: useMutation({
      mutationFn: (channelId: string) =>
        queueMarketplaceConnectivityTest(channelId),
      onSuccess: invalidate,
    }),
    retryConnectivity: useMutation({
      mutationFn: (channelId: string) =>
        retryMarketplaceFailedConnectivity(channelId),
      onSuccess: invalidate,
    }),
    pauseVerification: useMutation({
      mutationFn: (channelId: string) =>
        pauseMarketplaceVerification(channelId),
      onSuccess: invalidate,
    }),
    pause: useMutation({
      mutationFn: (input: { id: string; paused: boolean }) =>
        setMarketplaceChannelPaused(input.id, input.paused),
      onSuccess: invalidate,
    }),
    bind: useMutation({
      mutationFn: (input: { groupId: string; tokenId: number }) =>
        bindMarketplaceToken(input.groupId, input.tokenId),
      onSuccess: () =>
        queryClient.invalidateQueries({
          queryKey: ['marketplace-token-options'],
        }),
    }),
  }
}

export function useMarketplaceFailedModelRemoval(admin = false) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: { channelId: string; model: string }) =>
      removeMarketplaceFailedModel({ ...input, admin }),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['marketplace-channels'] }),
        queryClient.invalidateQueries({ queryKey: ['marketplace-groups'] }),
        queryClient.invalidateQueries({
          queryKey: ['marketplace-multiplier-trends'],
        }),
      ])
    },
  })
}

export function useAdminMarketplaceChannels(
  filters: AdminMarketplaceChannelFilters,
  enabled: boolean
) {
  return useQuery({
    queryKey: ['marketplace-channels', 'admin', filters],
    queryFn: () => getAdminMarketplaceChannels(filters),
    enabled,
    placeholderData: (previousData) => previousData,
    refetchInterval: (query) =>
      verificationRefetchInterval(query.state.data ?? []),
  })
}

export function useAdminOwnerIncome(filters: AdminMarketplaceChannelFilters) {
  return useQuery({
    queryKey: ['marketplace-owner-income', 'admin', filters],
    queryFn: () => getAdminOwnerIncome(filters),
    placeholderData: (previousData) => previousData,
  })
}

export function useAdminMarketplaceReview() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: { id: string; approved: boolean; reason: string }) =>
      reviewMarketplaceChannel(input.id, input.approved, input.reason),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ['marketplace-channels'],
      })
      await queryClient.invalidateQueries({ queryKey: ['marketplace-groups'] })
      await queryClient.invalidateQueries({
        queryKey: ['marketplace-multiplier-trends'],
      })
    },
  })
}

export function useAdminMarketplaceVerification(
  action: 'detect' | 'test' | 'retry-test' | 'pause'
) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (channelId: string) => {
      if (action === 'detect') {
        await queueMarketplaceDetection(channelId, true)
        return
      }
      if (action === 'test') {
        await queueMarketplaceConnectivityTest(channelId, true)
        return
      }
      if (action === 'retry-test') {
        await retryMarketplaceFailedConnectivity(channelId, true)
        return
      }
      await pauseMarketplaceVerification(channelId, true)
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ['marketplace-channels'],
      })
      await queryClient.invalidateQueries({
        queryKey: ['marketplace-multiplier-trends'],
      })
    },
  })
}

export function useMarketplaceChannelUpdate(admin: boolean) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: {
      id: string
      values: Parameters<typeof updateMarketplaceChannel>[1]
    }) => updateMarketplaceChannel(input.id, input.values, admin),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ['marketplace-channels'],
      })
      await queryClient.invalidateQueries({ queryKey: ['marketplace-groups'] })
      await queryClient.invalidateQueries({
        queryKey: ['marketplace-multiplier-trends'],
      })
      await queryClient.invalidateQueries({
        queryKey: ['selectable-marketplace-groups'],
      })
    },
  })
}

export function useMarketplaceChannelDelete(admin: boolean) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (channelId: string) =>
      deleteMarketplaceChannel(channelId, admin),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['marketplace-channels'] }),
        queryClient.invalidateQueries({ queryKey: ['marketplace-groups'] }),
        queryClient.invalidateQueries({
          queryKey: ['marketplace-multiplier-trends'],
        }),
        queryClient.invalidateQueries({
          queryKey: ['marketplace-auto-route-pool'],
        }),
        queryClient.invalidateQueries({
          queryKey: ['selectable-marketplace-groups'],
        }),
        queryClient.invalidateQueries({
          queryKey: ['api-key-marketplace-auto-pool'],
        }),
      ])
    },
  })
}
