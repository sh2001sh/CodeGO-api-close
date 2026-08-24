import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  normalizeRecentRequestSeries,
  resolveRecentRequestStatus,
} from './recent-request-series'

describe('recent request series', () => {
  test('fills missing intervals so the market strip always has twelve cells', () => {
    const bucketSeconds = 1800
    const now = 12 * bucketSeconds
    const series = normalizeRecentRequestSeries(
      [{ ts: 11 * bucketSeconds, success_rate: 95, request_count: 4 }],
      bucketSeconds,
      now
    )

    assert.equal(series.length, 12)
    assert.deepEqual(series.at(-2), {
      ts: 11 * bucketSeconds,
      success_rate: 95,
      request_count: 4,
    })
    assert.equal(series.at(-1)?.request_count, 0)
  })

  test('derives the latest status when the API only provides bucket data', () => {
    const status = resolveRecentRequestStatus([
      { ts: 1, success_rate: 100, request_count: 3 },
      { ts: 2, success_rate: 87, request_count: 5 },
      { ts: 3, success_rate: 0, request_count: 0 },
    ])

    assert.equal(status, 'unstable')
  })
})
