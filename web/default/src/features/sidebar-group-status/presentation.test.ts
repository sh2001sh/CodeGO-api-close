import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  buildHealthSegments,
  collectModelOptions,
  filterGroupStatusItems,
  getStatusMeta,
} from './presentation'
import type { SidebarGroupStatusItem } from './types'

const GROUPS: SidebarGroupStatusItem[] = [
  {
    group: 'official-default',
    display_name: '官方默认',
    source_type: 'official',
    status: 'healthy',
    models: [
      {
        model: 'gpt-5.2',
        status: 'healthy',
        success_rate: 99,
        sample_window: 6,
      },
    ],
  },
  {
    group: 'marketplace-fast',
    display_name: '快速线路',
    source_type: 'marketplace_user',
    status: 'unstable',
    models: [
      {
        model: 'claude-opus-4.6',
        status: 'unstable',
        success_rate: 88,
        sample_window: 6,
      },
      {
        model: 'gpt-5.2',
        status: 'healthy',
        success_rate: 97,
        sample_window: 6,
      },
    ],
  },
]

describe('group status presentation', () => {
  test('uses the same labels and thresholds as marketplace request health', () => {
    assert.equal(getStatusMeta('healthy').label, '稳定')
    assert.equal(getStatusMeta('unstable').label, '波动')
    assert.equal(getStatusMeta('failed').label, '异常')

    const segments = buildHealthSegments({
      model: 'gpt-test',
      status: 'healthy',
      success_rate: 100,
      sample_window: 0.5,
      series: [
        { ts: 1, success_rate: 90, request_count: 1 },
        { ts: 2, success_rate: 85, request_count: 1 },
        { ts: 3, success_rate: 84.99, request_count: 1 },
        { ts: 4, success_rate: null, request_count: 0 },
      ],
    })

    assert.deepEqual(
      segments.map((segment) => segment.tone),
      ['healthy', 'unstable', 'failed', 'unknown']
    )
  })

  test('filters by source, model, status, display name, and internal id', () => {
    assert.deepEqual(
      filterGroupStatusItems(GROUPS, {
        source: 'marketplace_user',
        model: 'gpt-5.2',
        status: 'unstable',
        search: 'marketplace-fast',
      }).map((item) => item.group),
      ['marketplace-fast']
    )

    assert.deepEqual(
      filterGroupStatusItems(GROUPS, {
        source: 'all',
        model: '',
        status: '',
        search: '官方默认',
      }).map((item) => item.group),
      ['official-default']
    )
  })

  test('returns unique model options in stable alphabetical order', () => {
    assert.deepEqual(collectModelOptions(GROUPS), [
      'claude-opus-4.6',
      'gpt-5.2',
    ])
  })
})
