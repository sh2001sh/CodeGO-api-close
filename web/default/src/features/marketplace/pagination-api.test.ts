import assert from 'node:assert/strict'
import { test } from 'node:test'
import { api } from '@/lib/api'
import {
  getAdminMarketplaceChannels,
  getMyMarketplaceBargainRequests,
  getOwnerMultipliers,
  getTokenOptions,
} from './api'

test('owner and admin lists request the selected page instead of the first page', async () => {
  const previousAdapter = api.defaults.adapter
  const requests: URL[] = []
  api.defaults.adapter = async (config) => {
    requests.push(new URL(api.getUri(config), 'http://localhost'))
    return {
      data: {
        success: true,
        data: {
          items: [{ id: 'second-page' }],
          total: 51,
          page: 2,
          page_size: 20,
        },
      },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  try {
    await getMyMarketplaceBargainRequests('pending', 2, 20)
    await getOwnerMultipliers(2)
    await getAdminMarketplaceChannels({
      page: 2,
      pageSize: 20,
      status: 'active',
    })
    for (const request of requests)
      assert.equal(request.searchParams.get('page'), '2')
    assert.equal(requests[0].searchParams.get('page_size'), '20')
    assert.equal(requests[0].searchParams.get('status'), 'pending')
    assert.equal(requests[2].searchParams.get('page_size'), '20')
    assert.equal(requests[2].searchParams.get('status'), 'active')
  } finally {
    api.defaults.adapter = previousAdapter
  }
})

test('token options preserve page metadata and surface a later-page failure', async () => {
  const previousAdapter = api.defaults.adapter
  const requests: URL[] = []
  api.defaults.adapter = async (config) => {
    const request = new URL(api.getUri(config), 'http://localhost')
    requests.push(request)
    const page = Number(request.searchParams.get('p'))
    return {
      data:
        page === 3
          ? { success: false, message: 'Token page unavailable' }
          : {
              success: true,
              data: {
                items: [{ id: page * 50 }],
                total: 121,
                page,
                page_size: 50,
              },
            },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  try {
    const result = await getTokenOptions(2)
    assert.equal(result.items[0].id, 100)
    assert.equal(result.total, 121)
    assert.equal(result.page, 2)
    assert.equal(requests[0].searchParams.get('page_size'), '50')
    await assert.rejects(getTokenOptions(3), /Token page unavailable/)
  } finally {
    api.defaults.adapter = previousAdapter
  }
})
