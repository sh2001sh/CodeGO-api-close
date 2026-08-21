import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { buildHealthSegments, getStatusMeta } from './presentation'

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
})
