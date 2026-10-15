import { useQuery } from '@tanstack/react-query'
import { getDailyLuckyNumberSelf } from '../api'

export const dailyLuckyNumberQueryKey = ['daily-lucky-number', 'self'] as const

export function useDailyLuckyNumberSelf(enabled = true) {
  return useQuery({
    queryKey: dailyLuckyNumberQueryKey,
    queryFn: async () => {
      const response = await getDailyLuckyNumberSelf()
      if (!response.success || !response.data) {
        throw new Error(response.message || 'Unable to load the daily lucky number activity.')
      }
      return response.data
    },
    enabled,
    staleTime: 60 * 1000,
    refetchInterval: 60 * 1000,
  })
}
