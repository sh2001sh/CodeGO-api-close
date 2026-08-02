import { useQuery } from '@tanstack/react-query'
import { getBounties, getBountyBalances, getBountyNotifications } from '../api'
import type { BountySearch } from '../types'

export function useBountyList(search: BountySearch) {
  return useQuery({
    queryKey: ['bounties', search],
    queryFn: () => getBounties(search),
    staleTime: 20_000,
  })
}

export function useBountyBalances(enabled = true) {
  return useQuery({
    queryKey: ['bounty-balances'],
    queryFn: getBountyBalances,
    enabled,
    staleTime: 30_000,
  })
}

export function useBountyNotifications(enabled = true) {
  return useQuery({
    queryKey: ['bounty-notifications'],
    queryFn: getBountyNotifications,
    enabled,
    staleTime: 15_000,
  })
}
