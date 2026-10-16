import assert from 'node:assert/strict'
import { test } from 'node:test'
import { api } from '@/lib/api'
import {
  createMarketplaceRoutePool,
  getMarketplaceGroups,
  getMarketplaceAutoRoutePool,
  updateMarketplaceAutoRoutePool,
  updateMarketplaceRoutePool,
} from './api'
import { getMarketplaceShops } from './shop-api'
import type { GroupFilters } from './types'

test('market and shop filters use package support, and pool settings preserve both checkbox states', async () => {
  const previousAdapter = api.defaults.adapter
  const requests: { url: URL; body: Record<string, unknown> }[] = []
  api.defaults.adapter = async (config) => {
    requests.push({
      url: new URL(api.getUri(config), 'http://localhost'),
      body: config.data ? JSON.parse(config.data) : {},
    })
    return {
      data: { success: true, data: { items: [], total: 0 } },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  const filters: GroupFilters = {
    search: '',
    model: '',
    source: '',
    provider: '',
    status: '',
    verification: '',
    sort: 'score',
    direction: 'desc',
    window_hours: 24,
    page: 1,
    page_size: 20,
  }
  try {
    for (const credit_pack of ['supported', 'unsupported', ''] as const) {
      await getMarketplaceGroups({ ...filters, credit_pack })
      await getMarketplaceShops({ ...filters, credit_pack })
      for (const request of requests.slice(-2)) {
        assert.equal(
          request.url.searchParams.get('credit_pack'),
          credit_pack || null
        )
        assert.equal(request.url.searchParams.has('multiplier_card'), false)
      }
    }
    for (const credit_pack_only of [true, false]) {
      await createMarketplaceRoutePool({
        name: 'Package pool',
        groupIds: ['supported'],
        autoBuild: { enabled: false, credit_pack_only },
      })
      await updateMarketplaceRoutePool({
        id: 'pool',
        groupIds: ['supported'],
        autoBuild: { enabled: true, credit_pack_only },
      })
      for (const request of requests.slice(-2)) {
        assert.equal(
          (request.body.auto_build as { credit_pack_only: boolean })
            .credit_pack_only,
          credit_pack_only
        )
      }
    }
  } finally {
    api.defaults.adapter = previousAdapter
  }
})

test('market data and route-pool writes expose transport and business failures', async () => {
  const previousAdapter = api.defaults.adapter
  const read = () =>
    getMarketplaceGroups({
      search: '',
      model: '',
      source: '',
      provider: '',
      status: '',
      verification: '',
      sort: 'score',
      direction: 'desc',
      window_hours: 24,
      page: 1,
      page_size: 20,
    })
  const calls = [
    read,
    getMarketplaceAutoRoutePool,
    () => updateMarketplaceAutoRoutePool({ groupIds: ['real-group'] }),
  ]
  try {
    api.defaults.adapter = async () => {
      throw new Error('Network unavailable')
    }
    for (const call of calls) await assert.rejects(call, /Network unavailable/)
    api.defaults.adapter = async (config) => ({
      data: { success: false, message: 'Pool update rejected' },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    })
    for (const call of calls) await assert.rejects(call, /Pool update rejected/)
  } finally {
    api.defaults.adapter = previousAdapter
  }
})
