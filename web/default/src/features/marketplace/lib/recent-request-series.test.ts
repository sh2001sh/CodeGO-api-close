import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  normalizeRecentRequestSeries,
  resolveRecentRequestStatus,
} from './recent-request-series'

describe('recent request series', () => {
  test('fills missing intervals into six hours of fifteen-minute cells', () => {
    const bucketSeconds = 900
    const now = 24 * bucketSeconds
    const series = normalizeRecentRequestSeries(
      [{ ts: 23 * bucketSeconds, success_rate: 95, request_count: 4 }],
      bucketSeconds,
      now
    )

    assert.equal(series.length, 24)
    assert.equal(series[0].ts, bucketSeconds)
    assert.deepEqual(series.at(-2), {
      ts: 23 * bucketSeconds,
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

  test('keeps server buckets visible when the browser clock is far ahead', () => {
    const bucketSeconds = 900
    const series = normalizeRecentRequestSeries(
      [{ ts: 10 * bucketSeconds, success_rate: 100, request_count: 2 }],
      bucketSeconds,
      100 * bucketSeconds
    )

    assert.equal(series.at(-1)?.request_count, 2)
  })

  test('accepts millisecond timestamps from older marketplace payloads', () => {
    const bucketSeconds = 900
    const currentBucket = 1_789_460_100
    const series = normalizeRecentRequestSeries(
      [
        {
          ts: (currentBucket - bucketSeconds) * 1000,
          success_rate: 95,
          request_count: 4,
        },
      ],
      bucketSeconds,
      currentBucket
    )

    assert.equal(series.at(-2)?.request_count, 4)
  })

  test('keeps a six-bucket hourly API series at six visible hours', () => {
    const bucketSeconds = 3600
    const now = 20 * bucketSeconds
    const input = Array.from({ length: 6 }, (_, index) => ({
      ts: (15 + index) * bucketSeconds,
      success_rate: 95,
      request_count: 10,
    }))

    const series = normalizeRecentRequestSeries(
      input,
      bucketSeconds,
      now,
      input.length
    )

    assert.equal(series.length, 6)
    assert.equal(series[0].ts, 15 * bucketSeconds)
    assert.equal(series.at(-1)?.ts, 20 * bucketSeconds)
  })
})
