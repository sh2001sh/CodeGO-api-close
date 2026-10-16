import assert from 'node:assert/strict'
import { test } from 'node:test'
import { apiKeySchema } from '../types'
import {
  transformApiKeyToFormDefaults,
  transformFormDataToPayload,
} from './api-key-form'

test('renaming a key preserves limits and ordinary group retry settings', () => {
  for (const crossGroupRetry of [true, false]) {
    const key = apiKeySchema.parse({
      id: 1,
      name: 'original',
      key: 'local-fixture',
      status: 1,
      remain_quota: 2500000,
      used_quota: 100,
      unlimited_quota: false,
      expired_time: 1900000000,
      created_time: 1700000000,
      accessed_time: 1700000000,
      group: 'premium',
      cross_group_retry: crossGroupRetry,
      model_limits_enabled: true,
      model_limits: 'gpt-4.1,claude-sonnet-4',
      allow_ips: '10.0.0.0/8',
      marketplace_multiplier_limit: 1.3,
    })
    const draft = transformApiKeyToFormDefaults(key)
    draft.name = 'renamed'
    assert.deepEqual(transformFormDataToPayload(draft), {
      name: 'renamed',
      remain_quota: key.remain_quota,
      expired_time: key.expired_time,
      unlimited_quota: false,
      group: key.group,
      cross_group_retry: crossGroupRetry,
      model_limits_enabled: true,
      model_limits: key.model_limits,
      allow_ips: key.allow_ips,
      marketplace_multiplier_limit: 1.3,
    })
  }
})

test('switching route families follows the server retry contract', () => {
  for (const [group, expected] of [
    ['auto', true],
    ['market:auto', false],
    ['market:pool:example', false],
    ['market:example', false],
  ] as const) {
    const key = transformFormDataToPayload({
      name: 'fixture',
      group,
      cross_group_retry: !expected,
      unlimited_quota: true,
      model_limits: [],
    })
    assert.equal(key.cross_group_retry, expected)
  }
})
