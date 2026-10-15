import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { normalizeUsageLogGroupOptions } from './api.ts'

describe('usage-log group options', () => {
  test('preserves internal filter values while exposing public identity', () => {
    assert.deepEqual(
      normalizeUsageLogGroupOptions([
        {
          value: 'Codex-Plus-1731f7',
          label: '41-Codex Plus-0.13x',
          public_id: '41',
          marketplace_group_id: 'group-41',
        },
      ]),
      [
        {
          value: 'Codex-Plus-1731f7',
          label: '41-Codex Plus-0.13x',
          public_id: '41',
          marketplace_group_id: 'group-41',
        },
      ]
    )
  })

  test('accepts legacy string options during rolling deployment', () => {
    assert.deepEqual(normalizeUsageLogGroupOptions(['default', '', null]), [
      { value: 'default', label: 'default' },
    ])
  })
})
