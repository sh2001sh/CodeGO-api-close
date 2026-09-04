/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { api } from '@/lib/api'
import type { UsageLog } from './data/schema'
import { buildQueryParams } from './lib/utils'
import type {
  GetLogsParams,
  GetLogsResponse,
  GetLogStatsParams,
  GetLogStatsResponse,
  GetTaskLogsParams,
  UsageLogGroupOption,
  UserInfo,
} from './types'

// ============================================================================
// Generic API Helpers
// ============================================================================

function buildApiPath(endpoint: string, isAdmin: boolean): string {
  return isAdmin ? endpoint : `${endpoint}/self`
}

async function fetchLogs<T>(
  endpoint: string,
  params: T,
  isAdmin: boolean
): Promise<GetLogsResponse> {
  const paramRecord = params as unknown as Record<string, unknown>
  const queryParams = buildQueryParams({
    p: paramRecord.p || 1,
    page_size: paramRecord.page_size || 20,
    ...params,
  })
  const path = buildApiPath(endpoint, isAdmin)
  const res = await api.get(`${path}?${queryParams}`)
  return res.data
}

async function fetchLogStats<T>(
  endpoint: string,
  params: T,
  isAdmin: boolean
): Promise<GetLogStatsResponse> {
  const queryParams = buildQueryParams(
    params as unknown as Record<string, unknown>
  )
  const path = buildApiPath(endpoint, isAdmin)
  const res = await api.get(`${path}/stat?${queryParams}`)
  return res.data
}

// ============================================================================
// Common Log APIs
// ============================================================================

export const getAllLogs = (params: GetLogsParams = {}) =>
  fetchLogs('/api/log', params, true)

export const getUserLogs = (
  params: Omit<GetLogsParams, 'username' | 'channel'> = {}
) => fetchLogs('/api/log', params, false)

export async function getAllUserLogs(
  params: Omit<GetLogsParams, 'username' | 'channel' | 'p' | 'page_size'> = {}
): Promise<UsageLog[]> {
  const pageSize = 100
  const first = await getUserLogs({ ...params, p: 1, page_size: pageSize })
  if (!first.success || !first.data) {
    throw new Error(first.message || '用量记录加载失败')
  }

  const items = [...(first.data.items as UsageLog[])]
  const pageCount = Math.ceil(first.data.total / pageSize)
  for (let page = 2; page <= pageCount; page += 1) {
    const response = await getUserLogs({
      ...params,
      p: page,
      page_size: pageSize,
    })
    if (!response.success || !response.data) {
      throw new Error(response.message || `用量记录第 ${page} 页加载失败`)
    }
    items.push(...(response.data.items as UsageLog[]))
  }
  return items
}

export const getLogStats = (params: GetLogStatsParams = {}) =>
  fetchLogStats('/api/log', params, true)

export const getUserLogStats = (
  params: Omit<GetLogStatsParams, 'username' | 'channel'> = {}
) => fetchLogStats('/api/log', params, false)

export function normalizeUsageLogGroupOptions(
  value: unknown
): UsageLogGroupOption[] {
  if (!Array.isArray(value)) return []
  return value.flatMap((item): UsageLogGroupOption[] => {
    if (typeof item === 'string' && item.trim()) {
      return [{ value: item, label: item }]
    }
    if (!item || typeof item !== 'object') return []
    const option = item as Record<string, unknown>
    if (typeof option.value !== 'string' || !option.value.trim()) return []
    return [
      {
        value: option.value,
        label:
          typeof option.label === 'string' && option.label.trim()
            ? option.label
            : option.value,
        ...(typeof option.public_id === 'string' && option.public_id.trim()
          ? { public_id: option.public_id }
          : {}),
        ...(typeof option.marketplace_group_id === 'string' &&
        option.marketplace_group_id.trim()
          ? { marketplace_group_id: option.marketplace_group_id }
          : {}),
      },
    ]
  })
}

export async function getUsageLogGroups(
  isAdmin: boolean
): Promise<UsageLogGroupOption[]> {
  const path = isAdmin
    ? '/api/log/groups/options'
    : '/api/log/self/groups/options'
  const res = await api.get(path)
  if (!res.data?.success || !Array.isArray(res.data.data)) {
    throw new Error(res.data?.message || 'Unable to load usage log groups.')
  }
  return normalizeUsageLogGroupOptions(res.data.data)
}

export async function getUserInfo(
  userId: number
): Promise<{ success: boolean; message?: string; data?: UserInfo }> {
  const res = await api.get(`/api/user/${userId}`)
  return res.data
}

// ============================================================================
// Task Logs API
// ============================================================================

export const getAllTaskLogs = (params: GetTaskLogsParams) =>
  fetchLogs('/api/task', params, true)

export const getUserTaskLogs = (params: GetTaskLogsParams) =>
  fetchLogs('/api/task', params, false)
