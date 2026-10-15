import assert from 'node:assert/strict'
import { test } from 'node:test'
import { api } from '@/lib/api'
import { getMarketplaceGroupStatus } from './api'

test('status fetch returns all groups in one request without market pagination', async () => {
  const previousAdapter = api.defaults.adapter
  const requests: string[] = []
  const items = Array.from({ length: 125 }, (_, index) => ({
    id: `group-${index}`,
  }))
  api.defaults.adapter = async (config) => {
    requests.push(config.url ?? '')
    return {
      data: { success: true, data: items },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  try {
    const groups = await getMarketplaceGroupStatus()
    assert.equal(groups.length, 125)
    assert.equal(groups[124].id, 'group-124')
    assert.deepEqual(requests, ['/api/marketplace/group-status'])
  } finally {
    api.defaults.adapter = previousAdapter
  }
})

test('a failed status request is not treated as an empty group list', async () => {
  const previousAdapter = api.defaults.adapter
  api.defaults.adapter = async (config) => ({
    data: { success: false, message: 'status unavailable' },
    status: 200,
    statusText: 'OK',
    headers: {},
    config,
  })
  try {
    await assert.rejects(getMarketplaceGroupStatus(), /status unavailable/)
  } finally {
    api.defaults.adapter = previousAdapter
  }
})
