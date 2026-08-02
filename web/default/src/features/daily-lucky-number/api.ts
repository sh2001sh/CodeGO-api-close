import { api } from '@/lib/api'
import type {
  DailyLuckyAdminResponse,
  DailyLuckyBackfillResponse,
  DailyLuckyConfig,
  DailyLuckyConfigResponse,
  DailyLuckyHistoryResponse,
  DailyLuckyPublicWinsResponse,
  DailyLuckySelfResponse,
} from './types'

export async function getDailyLuckyNumberSelf(): Promise<DailyLuckySelfResponse> {
  const response = await api.get('/api/daily-lucky-number/self')
  return response.data
}

export async function getDailyLuckyNumberHistory(
  page = 1,
  pageSize = 20
): Promise<DailyLuckyHistoryResponse> {
  const response = await api.get('/api/daily-lucky-number/history', {
    params: { page, page_size: pageSize },
  })
  return response.data
}

export async function getDailyLuckyNumberPublicWins(
  page = 1,
  pageSize = 20
): Promise<DailyLuckyPublicWinsResponse> {
  const response = await api.get('/api/daily-lucky-number/public-wins', {
    params: { page, page_size: pageSize },
  })
  return response.data
}

export async function getAdminDailyLuckyNumberConfig(): Promise<DailyLuckyConfigResponse> {
  const response = await api.get('/api/daily-lucky-number/admin/config')
  return response.data
}

export async function updateAdminDailyLuckyNumberConfig(
  payload: Partial<DailyLuckyConfig>
): Promise<DailyLuckyConfigResponse> {
  const response = await api.put('/api/daily-lucky-number/admin/config', payload)
  return response.data
}

export async function getAdminDailyLuckyNumberDraws(
  page = 1,
  pageSize = 20
): Promise<DailyLuckyAdminResponse> {
  const response = await api.get('/api/daily-lucky-number/admin/draws', {
    params: { page, page_size: pageSize },
  })
  return response.data
}

export async function retryAdminDailyLuckyNumberDraw(
  drawId: number
): Promise<{ success: boolean; message?: string }> {
  const response = await api.post(
    `/api/daily-lucky-number/admin/draws/${drawId}/retry`
  )
  return response.data
}

export async function backfillAdminDailyLuckyNumbers(): Promise<DailyLuckyBackfillResponse> {
  const response = await api.post('/api/daily-lucky-number/admin/backfill')
  return response.data
}
