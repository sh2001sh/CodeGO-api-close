import assert from 'node:assert/strict'
import { test } from 'node:test'
import { api } from '@/lib/api'
import { updateMarketplaceChannel } from '../api'
import {
  channelEditFormSchema,
  channelFormDefaults,
  channelPresentationSchema,
} from './channel-form'

test('presentation corrections can save independently of invalid legacy configuration', () => {
  const presentation = {
    custom_name: '模型接入',
    description: '支持批量处理\n高峰限流',
    tags: ['文本', '图像'],
  }
  assert.equal(channelPresentationSchema.safeParse(presentation).success, true)
  assert.equal(
    channelEditFormSchema.safeParse({
      ...channelFormDefaults,
      ...presentation,
      model_prices: { legacy: { input_price_per_million: 0 } },
    }).success,
    false
  )
  assert.deepEqual(
    channelPresentationSchema.parse({
      ...presentation,
      visibility: 'public',
      lifecycle_status: 'active',
    }),
    presentation
  )
})

test('presentation still enforces title, note and tag limits and permits clearing', () => {
  const valid = {
    custom_name: '名'.repeat(20),
    description: '注'.repeat(300),
    tags: Array.from({ length: 6 }, (_, index) => `${index}${'标'.repeat(15)}`),
  }
  assert.equal(channelPresentationSchema.safeParse(valid).success, true)
  for (const invalid of [
    { ...valid, custom_name: `${valid.custom_name}名` },
    { ...valid, description: `${valid.description}注` },
    { ...valid, tags: [...valid.tags, '额外'] },
    { ...valid, tags: ['标'.repeat(17)] },
  ]) {
    assert.equal(channelPresentationSchema.safeParse(invalid).success, false)
  }
  assert.equal(
    channelPresentationSchema.safeParse({
      custom_name: '',
      description: '',
      tags: [],
    }).success,
    true
  )
})

test('owner presentation PATCH sends only public fields and exposes rejected corrections', async () => {
  const previousAdapter = api.defaults.adapter
  const values = {
    custom_name: '恢复标题',
    description: '支持文本',
    tags: ['文本'],
  }
  let reject = false
  const calls: {
    method: string | undefined
    url: string | undefined
    body: unknown
  }[] = []
  api.defaults.adapter = async (config) => {
    calls.push({
      method: config.method,
      url: config.url,
      body: JSON.parse(config.data),
    })
    return {
      data: reject
        ? { success: false, message: '公开内容仍需修改' }
        : { success: true, data: { id: 'owner-channel', ...values } },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  try {
    await updateMarketplaceChannel('owner-channel', values, false)
    assert.deepEqual(calls[0], {
      method: 'patch',
      url: '/api/marketplace/channels/owner-channel',
      body: values,
    })
    reject = true
    await assert.rejects(
      updateMarketplaceChannel('owner-channel', values, false),
      /公开内容仍需修改/
    )
  } finally {
    api.defaults.adapter = previousAdapter
  }
})
