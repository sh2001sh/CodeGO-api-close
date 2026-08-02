import { api } from '@/lib/api'
import type {
  CommunityResource,
  ResourceConfig,
  ResourceFilters,
  ResourceList,
  SubmitResourceInput,
} from './types'

interface ApiEnvelope<T> {
  success?: boolean
  message?: string
  data?: T
}

function unwrap<T>(body: ApiEnvelope<T>): T {
  if (body.success === false) throw new Error(body.message || 'Request failed')
  return body.data as T
}

function listParams(filters: ResourceFilters) {
  return {
    keyword: filters.keyword || undefined,
    category: filters.category === 'all' ? undefined : filters.category,
    status: filters.status === 'all' ? undefined : filters.status,
    page: filters.page ?? 1,
    page_size: 20,
  }
}

export async function getCommunityResourceConfig() {
  const response = await api.get<ApiEnvelope<ResourceConfig>>(
    '/api/community-resources/config'
  )
  return unwrap(response.data)
}

export async function listCommunityResources(filters: ResourceFilters) {
  const response = await api.get<ApiEnvelope<ResourceList>>(
    '/api/community-resources',
    { params: listParams(filters) }
  )
  return unwrap(response.data)
}

export async function listMyCommunityResources(filters: ResourceFilters) {
  const response = await api.get<ApiEnvelope<ResourceList>>(
    '/api/community-resources/mine',
    { params: listParams(filters) }
  )
  return unwrap(response.data)
}

export async function listAdminCommunityResources(filters: ResourceFilters) {
  const response = await api.get<ApiEnvelope<ResourceList>>(
    '/api/admin/community-resources',
    { params: listParams(filters) }
  )
  return unwrap(response.data)
}

export async function submitCommunityResource(input: SubmitResourceInput) {
  const response = await api.post<ApiEnvelope<CommunityResource>>(
    '/api/community-resources',
    input
  )
  return unwrap(response.data)
}

export async function reviewCommunityResource(
  id: number,
  input: {
    status: 'approved' | 'rejected'
    note?: string
    grant_reward?: boolean
  }
) {
  const response = await api.patch<ApiEnvelope<CommunityResource>>(
    `/api/admin/community-resources/${id}`,
    input
  )
  return unwrap(response.data)
}

export async function updateCommunityResourceConfig(rewardUsd: number) {
  const response = await api.put<ApiEnvelope<ResourceConfig>>(
    '/api/admin/community-resources/config',
    { reward_usd: rewardUsd }
  )
  return unwrap(response.data)
}
