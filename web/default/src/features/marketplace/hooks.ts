import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  bindMarketplaceToken,
  createMarketplaceGroupInvite,
  createMarketplaceChannel,
  deleteMarketplaceChannel,
  fetchMarketplaceModels,
  getAdminMarketplaceChannels,
  getAdminOwnerIncome,
  releaseAdminOwnerIncome,
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
  setMarketplaceChannelUserBlock,
  setAdminMarketplaceChannelPaused,
  submitMarketplaceChannelFeedback,
  updateMarketplaceChannel,
  updateMarketplaceAutoRoutePool,
  startMarketplaceBatchTest,
  getMarketplaceBatchTest,
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
    ? 2000
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

export function useMarketplaceGroups(
  filters: GroupFilters,
  options: { enabled?: boolean } = {}
) {
  return useQuery({
    queryKey: ['marketplace-groups', filters],
    queryFn: () => getMarketplaceGroups(filters),
    enabled: options.enabled ?? true,
    placeholderData: (previousData) => previousData,
    staleTime: 2 * 60_000,
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

export function useMarketplaceBatchTest() {
  return useMutation({ mutationFn: startMarketplaceBatchTest })
}

export function useMarketplaceBatchTestQuery(id: string, enabled = true) {
  return useQuery({
    queryKey: ['marketplace-batch-test', id],
    queryFn: () => getMarketplaceBatchTest(id),
    enabled: Boolean(id) && enabled,
    refetchInterval: (query) =>
      ['queued', 'running'].includes(query.state.data?.status ?? '')
        ? 1000
        : false,
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
  const invalidateChannels = () =>
    queryClient.invalidateQueries({ queryKey: ['marketplace-channels'] })
  const invalidateAvailability = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ['marketplace-groups'] }),
      invalidateChannels(),
    ])
  }
  return {
    create: useMutation({
      mutationFn: createMarketplaceChannel,
      onSuccess: invalidateChannels,
    }),
    fetchModels: useMutation({ mutationFn: fetchMarketplaceModels }),
    detect: useMutation({
      mutationFn: (channelId: string) => queueMarketplaceDetection(channelId),
      onSuccess: invalidateChannels,
    }),
    testConnectivity: useMutation({
      mutationFn: (channelId: string) =>
        queueMarketplaceConnectivityTest(channelId),
      onSuccess: invalidateChannels,
    }),
    retryConnectivity: useMutation({
      mutationFn: (channelId: string) =>
        retryMarketplaceFailedConnectivity(channelId),
      onSuccess: invalidateChannels,
    }),
    pauseVerification: useMutation({
      mutationFn: (channelId: string) =>
        pauseMarketplaceVerification(channelId),
      onSuccess: invalidateChannels,
    }),
    pause: useMutation({
      mutationFn: (input: { id: string; paused: boolean }) =>
        setMarketplaceChannelPaused(input.id, input.paused),
      onSuccess: invalidateAvailability,
    }),
    userBlock: useMutation({
      mutationFn: setMarketplaceChannelUserBlock,
      onSuccess: invalidateAvailability,
    }),
    adminPause: useMutation({
      mutationFn: (input: { id: string; paused: boolean }) =>
        setAdminMarketplaceChannelPaused(input.id, input.paused),
      onSuccess: invalidateAvailability,
    }),
    bind: useMutation({
      mutationFn: (input: { groupId: string; tokenId: number }) =>
        bindMarketplaceToken(input.groupId, input.tokenId),
      onSuccess: () =>
        queryClient.invalidateQueries({
          queryKey: ['marketplace-token-options'],
        }),
    }),
    createInvite: useMutation({
      mutationFn: (groupId: string) => createMarketplaceGroupInvite(groupId),
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

export function useAdminOwnerIncomeRelease() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (
      filters: Pick<
        AdminMarketplaceChannelFilters,
        'ownerSearch' | 'ownerUserIds' | 'startTimestamp' | 'endTimestamp'
      >
    ) => releaseAdminOwnerIncome(filters),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: ['marketplace-owner-income', 'admin'],
        }),
        queryClient.invalidateQueries({
          queryKey: ['marketplace-channels', 'admin'],
        }),
        queryClient.invalidateQueries({ queryKey: ['marketplace-channels'] }),
      ])
    },
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
