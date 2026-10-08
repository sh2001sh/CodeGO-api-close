import { api, unwrap } from './api'
import { resourceOptions } from './queries'
import type { paths } from './api.generated'

type GroupsResponse =
  paths['/api/marketplace/groups']['get']['responses']['200']['content']['application/json']
export type PublicGroup = GroupsResponse['data'][number]
export type PublicCatalogModel =
  paths['/api/public/models']['get']['responses']['200']['content']['application/json']['data'][number]

export const publicModelsOptions = () =>
  resourceOptions('public-models', (signal) =>
    api.GET('/api/public/models', { signal }).then(unwrap),
  )

export const modelFavoritesOptions = () =>
  resourceOptions('model-favorites', (signal) =>
    api.GET('/api/models/favorites/', { signal }).then(unwrap),
  )

export type PublicModel = {
  name: string
  vendor: string
  groups: { slug: string; name: string; multiplier: string; verified: boolean }[]
  /** Lowest multiplier across the groups that serve the model. */
  bestMultiplier: number
}

export const publicGroupsOptions = (search = '') =>
  resourceOptions(
    'public-groups',
    (signal) =>
      api
        .GET('/api/marketplace/groups', {
          params: { query: { page: 1, page_size: 24, search } },
          signal,
        })
        .then(unwrap),
    [search],
  )

export type BrowsePagination = { page: number; page_size: number; total: number }
export type MarketBrowseFilters = {
  page: number
  page_size: number
  search?: string
  model?: string
  tag?: string
  scope?: 'all' | 'public' | 'private'
  sort?: 'recommended' | 'multiplier' | 'price' | 'models' | 'success' | 'name'
  price_basis?: 'input' | 'output' | 'cache-read' | 'cache-write' | 'request'
}

export const groupPageOptions = (filters: MarketBrowseFilters, signedIn: boolean) =>
  resourceOptions(
    signedIn ? 'market-groups' : 'public-group-page',
    async (signal) => {
      const result = await api.GET(
        signedIn ? '/api/marketplace/key-group-options' : '/api/marketplace/groups',
        { params: { query: filters }, signal },
      )
      const groups = unwrap(result)
      return {
        groups,
        pagination: result.data?.pagination ?? {
          page: filters.page,
          page_size: filters.page_size,
          total: groups.length,
        },
        models: result.data?.models ?? [
          ...new Set(groups.flatMap((group) => group.declared_models ?? [])),
        ],
      }
    },
    [
      signedIn,
      filters.page,
      filters.page_size,
      filters.search ?? '',
      filters.model ?? '',
      filters.tag ?? '',
      filters.scope ?? 'all',
      filters.sort ?? 'recommended',
      filters.price_basis ?? 'input',
    ],
  )

export const groupDetailOptions = (id: string) =>
  resourceOptions(
    'market-group-detail',
    (signal) =>
      api
        .GET('/api/marketplace/groups/{slug}', { params: { path: { slug: id } }, signal })
        .then(unwrap),
    [id],
  )

export function vendorOf(model: string): string {
  const lower = model.toLowerCase()
  if (/^(gpt|o\d|chatgpt|dall-e|whisper|tts|text-embedding)/.test(lower)) return 'OpenAI'
  if (lower.startsWith('claude')) return 'Anthropic'
  if (lower.startsWith('gemini') || lower.startsWith('gemma')) return 'Google'
  if (lower.startsWith('deepseek')) return 'DeepSeek'
  if (lower.startsWith('qwen') || lower.startsWith('qwq')) return 'Qwen'
  if (lower.startsWith('glm') || lower.startsWith('chatglm')) return 'Zhipu'
  if (lower.startsWith('grok')) return 'xAI'
  if (lower.startsWith('llama')) return 'Meta'
  if (lower.startsWith('mistral') || lower.startsWith('mixtral')) return 'Mistral'
  if (lower.startsWith('moonshot') || lower.startsWith('kimi')) return 'Moonshot'
  if (lower.startsWith('doubao')) return 'ByteDance'
  return lower.includes('/') ? lower.split('/')[0] : '其他'
}

/** Inverts group → models into model → groups, sorted by name. */
export function modelsFromGroups(groups: readonly PublicGroup[]): PublicModel[] {
  const byName = new Map<string, PublicModel>()
  for (const group of groups) {
    for (const name of group.declared_models ?? []) {
      const model = byName.get(name) ?? {
        name,
        vendor: vendorOf(name),
        groups: [],
        bestMultiplier: Number.POSITIVE_INFINITY,
      }
      const multiplier = Number(group.multiplier)
      model.groups.push({
        slug: group.public_slug,
        name: group.system_display_name || group.public_slug,
        multiplier: String(group.multiplier),
        verified: group.verification_status === 'passed',
      })
      if (Number.isFinite(multiplier))
        model.bestMultiplier = Math.min(model.bestMultiplier, multiplier)
      byName.set(name, model)
    }
  }
  return [...byName.values()].sort((a, b) => a.name.localeCompare(b.name))
}
