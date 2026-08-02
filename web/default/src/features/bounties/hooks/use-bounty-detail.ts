import { useQuery } from '@tanstack/react-query'
import { getBountyDetail } from '../api'

export function useBountyDetail(taskId: string) {
  return useQuery({
    queryKey: ['bounty', taskId],
    queryFn: () => getBountyDetail(taskId),
    enabled: Boolean(taskId),
    staleTime: 10_000,
  })
}
