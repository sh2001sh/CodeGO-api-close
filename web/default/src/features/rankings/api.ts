import { api } from '@/lib/api'
import type {
  PerformanceMetricsData,
  PerfSummaryAllData,
} from '@/features/performance-metrics/types'

export async function getModelRankingSummary(hours: number) {
  const response = await api.get<PerfSummaryAllData>(
    '/api/rankings/models/summary',
    {
      params: { hours },
    }
  )
  return response.data
}

export async function getModelRankingTrend(model: string, hours: number) {
  const response = await api.get<PerformanceMetricsData>(
    '/api/rankings/models',
    {
      params: { model, hours },
    }
  )
  return response.data
}
