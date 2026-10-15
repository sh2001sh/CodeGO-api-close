import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import type { MarketplaceAutoRoutePoolItem } from '../types'
import {
  appendAutoRoutePoolGroup,
  selectedAutoRoutePoolGroupIDs,
} from './auto-route-pool'

const item = (
  groupID: string,
  selected: boolean,
  priority: number
): MarketplaceAutoRoutePoolItem => ({
  group_id: groupID,
  source_type: 'marketplace_user',
  public_slug: groupID,
  system_display_name: groupID,
  source_label: 'test',
  lifecycle_status: 'active',
  multiplier: 1,
  availability: 100,
  success_rate: 100,
  cache_hit_rate: 0,
  avg_latency_ms: 100,
  latest_request_status: 'healthy',
  metrics_available: true,
  route_score: 1,
  observing: false,
  request_count: 1,
  models: [],
  selected,
  priority,
})

describe('auto route pool helpers', () => {
  test('preserves saved priority when appending a group', () => {
    const items = [
      item('second', true, 2),
      item('candidate', false, 0),
      item('first', true, 1),
    ]

    assert.deepEqual(appendAutoRoutePoolGroup(items, 'candidate'), [
      'first',
      'second',
      'candidate',
    ])
  })

  test('does not duplicate a group already in the route pool', () => {
    const items = [item('first', true, 1), item('second', true, 2)]

    assert.deepEqual(selectedAutoRoutePoolGroupIDs(items), ['first', 'second'])
    assert.deepEqual(appendAutoRoutePoolGroup(items, 'first'), [
      'first',
      'second',
    ])
  })
})
