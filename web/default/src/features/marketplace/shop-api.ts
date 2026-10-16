import interfaceI18n from '@/i18n/config'
import { api } from '@/lib/api'
import type { GroupFilters } from './types'

export interface MarketplaceShop {
  id: number
  name: string
  custom_name: string
  moderation_status: string
  description: string
  channel_count: number
  min_multiplier: number
  models: string[]
  tags: string[]
  rating: number
  rating_count: number
  viewer_stars: number
  can_rate: boolean
  rating_permission: 'allowed' | 'owner' | 'login_required' | 'usage_required'
}

export interface MarketReport {
  id: number
  target_type: 'channel' | 'shop'
  target_id: string
  reason: string
  details: string
  evidence: string
  status: string
  resolution: string
  created_at: string
}

export interface PagedItems<T> {
  items: T[]
  total: number
  page: number
  page_size: number
}

interface Envelope<T> {
  success: boolean
  message?: string
  data: T
}
function requireResult<T>(result: Envelope<T>): T {
  if (!result.success)
    throw new Error(result.message || interfaceI18n.t('操作失败，请重试'))
  return result.data
}

export async function getMarketplaceShops(
  filters: GroupFilters
): Promise<PagedItems<MarketplaceShop>> {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(filters)) {
    if (value == null || value === '') continue
    if (Array.isArray(value))
      value.forEach((entry) =>
        params.append(key === 'models' ? 'model' : key, entry)
      )
    else params.set(key, String(value))
  }
  const response = await api.get<Envelope<PagedItems<MarketplaceShop>>>(
    `/api/marketplace/shops?${params}`
  )
  return requireResult(response.data)
}

export async function getMyShop(): Promise<MarketplaceShop> {
  return requireResult(
    (await api.get<Envelope<MarketplaceShop>>('/api/marketplace/shop/mine'))
      .data
  )
}

export async function updateMyShop(input: {
  name: string
  description: string
}): Promise<MarketplaceShop> {
  return requireResult(
    (
      await api.put<Envelope<MarketplaceShop>>(
        '/api/marketplace/shop/mine',
        input
      )
    ).data
  )
}

export async function rateShop(input: {
  id: number
  stars: number
}): Promise<MarketplaceShop> {
  return requireResult(
    (
      await api.put<Envelope<MarketplaceShop>>(
        `/api/marketplace/shops/${input.id}/rating`,
        { stars: input.stars }
      )
    ).data
  )
}

export async function submitMarketReport(input: {
  target_type: 'channel' | 'shop'
  target_id: string
  reason: string
  details: string
}): Promise<void> {
  requireResult(
    (await api.post<Envelope<unknown>>('/api/marketplace/reports', input)).data
  )
}

export async function getMarketReports(
  status: string,
  page: number
): Promise<PagedItems<MarketReport>> {
  return requireResult(
    (
      await api.get<Envelope<PagedItems<MarketReport>>>(
        `/api/marketplace/admin/reports?status=${encodeURIComponent(status)}&page=${page}&page_size=20`
      )
    ).data
  )
}

export async function resolveMarketReport(input: {
  id: number
  action: 'dismiss' | 'remove_content'
  note: string
}): Promise<void> {
  requireResult(
    (
      await api.post<Envelope<unknown>>(
        `/api/marketplace/admin/reports/${input.id}/resolve`,
        { action: input.action, note: input.note }
      )
    ).data
  )
}
