import assert from 'node:assert/strict'
import { test } from 'node:test'
import { api } from '@/lib/api'
import { getAllVendors } from './api'

test('vendor selectors fetch all 205 options despite the server 100-row cap', async () => {
  const previousAdapter = api.defaults.adapter
  const pages: number[] = []
  api.defaults.adapter = async (config) => {
    const request = new URL(api.getUri(config), 'http://localhost')
    const page = Number(request.searchParams.get('p'))
    assert.equal(request.searchParams.get('page_size'), '100')
    pages.push(page)
    const count = Math.min(100, 205 - (page - 1) * 100)
    return {
      data: {
        success: true,
        data: {
          items: Array.from({ length: count }, (_, index) => ({
            id: (page - 1) * 100 + index + 1,
          })),
          total: 205,
          page,
          page_size: 100,
        },
      },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  try {
    const result = await getAllVendors()
    assert.equal(result.data?.items.length, 205)
    assert.equal(result.data?.items.at(-1)?.id, 205)
    assert.deepEqual(pages, [1, 2, 3])
  } finally {
    api.defaults.adapter = previousAdapter
  }
})

test('vendor selectors do not present a partial result as the complete list', async () => {
  const previousAdapter = api.defaults.adapter
  api.defaults.adapter = async (config) => {
    const page = Number(
      new URL(api.getUri(config), 'http://localhost').searchParams.get('p')
    )
    return {
      data:
        page === 1
          ? {
              success: true,
              data: {
                items: Array.from({ length: 100 }, (_, i) => ({ id: i + 1 })),
                total: 101,
                page: 1,
                page_size: 100,
              },
            }
          : { success: false, message: 'Vendor query unavailable' },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  try {
    await assert.rejects(getAllVendors(), /Vendor query unavailable/)
  } finally {
    api.defaults.adapter = previousAdapter
  }
})
