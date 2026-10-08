import type { Page } from '@playwright/test'

export type MarketFixtureGroup = {
  id: string
  group_id: string
  public_slug?: string
  system_display_name: string
  declared_models: string[]
  visibility?: string
  tags?: string[]
  provider_type?: string
  approved_source_label?: string
  multiplier?: number
  effective_model_prices?: Record<string, Record<string, unknown>>
  [key: string]: unknown
}

export function marketBrowseResponse(groups: MarketFixtureGroup[], url: URL) {
  const q = url.searchParams
  const search = (q.get('search') ?? '').trim().toLowerCase()
  const model = q.get('model') ?? ''
  const tag = q.get('tag') ?? ''
  const scope = q.get('scope') ?? 'all'
  const filtered = groups.filter(
    (group) =>
      (!model || group.declared_models.includes(model)) &&
      (!tag || group.tags?.includes(tag)) &&
      (scope === 'all' || group.visibility === scope) &&
      (!search ||
        [
          group.id,
          group.system_display_name,
          group.remark,
          group.provider_type,
          group.approved_source_label,
          ...group.declared_models,
          ...(group.tags ?? []),
        ]
          .join(' ')
          .toLowerCase()
          .includes(search)),
  )
  const sort = q.get('sort') ?? 'recommended'
  const field =
    {
      input: 'input_per_million',
      output: 'output_per_million',
      'cache-read': 'cache_read_per_million',
      'cache-write': 'cache_write_per_million',
      request: 'per_unit',
    }[q.get('price_basis') ?? 'input'] ?? 'input_per_million'
  filtered.sort((a, b) => {
    let comparison = 0
    if (sort === 'price' || (sort === 'recommended' && model)) {
      const amount = (group: MarketFixtureGroup) =>
        Number(group.effective_model_prices?.[model]?.[field] ?? Infinity)
      comparison = amount(a) - amount(b)
    } else if (sort === 'multiplier') comparison = (a.multiplier ?? 1) - (b.multiplier ?? 1)
    else if (sort === 'models') comparison = b.declared_models.length - a.declared_models.length
    return (
      comparison ||
      a.system_display_name.localeCompare(b.system_display_name) ||
      a.id.localeCompare(b.id)
    )
  })
  if (!q.has('page')) return { success: true, data: filtered }
  const page = Number(q.get('page'))
  const page_size = Number(q.get('page_size') ?? 24)
  return {
    success: true,
    data: filtered.slice((page - 1) * page_size, page * page_size).map((group) => ({
      ...group,
      model_prices: {},
      effective_model_prices:
        model && group.effective_model_prices?.[model]
          ? { [model]: group.effective_model_prices[model] }
          : {},
    })),
    pagination: { page, page_size, total: filtered.length },
    models: [...new Set(groups.flatMap((group) => group.declared_models))].sort(),
  }
}

/** Browse returns compact pages; selection and comparison fetch their complete own details. */
export async function marketGroups(
  page: Page,
  groups: MarketFixtureGroup[],
  endpoint = 'key-group-options',
) {
  await page.route('**/api/marketplace/**', async (route) => {
    const url = new URL(route.request().url())
    if (url.pathname === `/api/marketplace/${endpoint}`)
      return route.fulfill({ json: marketBrowseResponse(groups, url) })
    if (/^\/api\/marketplace\/groups\/[^/]+$/.test(url.pathname)) {
      const id = decodeURIComponent(url.pathname.split('/').at(-1)!)
      const group = groups.find(
        (group) => group.id === id || group.group_id === id || group.public_slug === id,
      )
      return group
        ? route.fulfill({ json: { success: true, data: group } })
        : route.fulfill({ status: 404, json: { success: false, message: '资源不存在或无权访问' } })
    }
    return route.fallback()
  })
}

export type MarketFixtureShop = {
  id: string
  name: string
  description: string
  declared_models: string[]
  tags: string[]
  [key: string]: unknown
}

export function marketShopResponse(shops: MarketFixtureShop[], url: URL) {
  const q = url.searchParams
  const search = (q.get('search') ?? '').trim().toLowerCase()
  const filtered = shops.filter(
    (shop) =>
      (!q.get('model') || shop.declared_models.includes(q.get('model')!)) &&
      (!q.get('tag') || shop.tags.includes(q.get('tag')!)) &&
      (!search ||
        [shop.id, shop.name, shop.description, ...shop.declared_models, ...shop.tags]
          .join(' ')
          .toLowerCase()
          .includes(search)),
  )
  const page = Number(q.get('page') ?? 1)
  const page_size = Number(q.get('page_size') ?? 24)
  return {
    success: true,
    data: filtered.slice((page - 1) * page_size, page * page_size),
    pagination: { page, page_size, total: filtered.length },
    models: [...new Set(shops.flatMap((shop) => shop.declared_models))].sort(),
  }
}

export async function marketShops(
  page: Page,
  shops: MarketFixtureShop[],
  inventory: Record<string, MarketFixtureGroup[]>,
) {
  await page.route('**/api/marketplace/shops**', async (route) => {
    const url = new URL(route.request().url())
    if (url.pathname === '/api/marketplace/shops')
      return route.fulfill({ json: marketShopResponse(shops, url) })
    const shop = shops.find((shop) => url.pathname === `/api/marketplace/shops/${shop.id}`)
    if (!shop) return route.fallback()
    const groups = marketBrowseResponse(inventory[shop.id] ?? [], url)
    return route.fulfill({
      json: { success: true, data: { shop, groups: groups.data, pagination: groups.pagination } },
    })
  })
}
