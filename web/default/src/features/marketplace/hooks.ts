import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  bindMarketplaceToken,
  createMarketplaceChannel,
  fetchMarketplaceModels,
  getAdminMarketplaceChannels,
  getMarketplaceGroups,
  getMyMarketplaceChannels,
  getMyMarketplaceUsageLogs,
  getTokenOptions,
  queueMarketplaceVerification,
  reviewMarketplaceChannel,
  setMarketplaceChannelPaused,
  updateMarketplaceChannel,
} from './api'
import type { GroupFilters } from './types'

function verificationRefetchInterval(
  channels: { lifecycle_status: string; verification_status: string }[]
) {
  return channels.some(
    (channel) =>
      channel.lifecycle_status === 'verifying' ||
      ['queued', 'running'].includes(channel.verification_status)
  )
    ? 1000
    : false
}

export function useMarketplaceGroups(filters: GroupFilters) {
  return useQuery({
    queryKey: ['marketplace-groups', filters],
    queryFn: () => getMarketplaceGroups(filters),
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

export function useMyMarketplaceUsageLogs(params: {
  channelId?: string
  page: number
  pageSize: number
}) {
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
      queryClient.invalidateQueries({ queryKey: ['marketplace-channels'] }),
    ])
  }
  return {
    create: useMutation({
      mutationFn: createMarketplaceChannel,
      onSuccess: invalidate,
    }),
    fetchModels: useMutation({ mutationFn: fetchMarketplaceModels }),
    verify: useMutation({
      mutationFn: queueMarketplaceVerification,
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

export function useAdminMarketplaceChannels(enabled: boolean) {
  return useQuery({
    queryKey: ['marketplace-channels', 'admin'],
    queryFn: () => getAdminMarketplaceChannels(),
    enabled,
    refetchInterval: (query) =>
      verificationRefetchInterval(query.state.data ?? []),
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
        queryKey: ['selectable-marketplace-groups'],
      })
    },
  })
}
