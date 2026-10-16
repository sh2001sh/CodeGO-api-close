import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  aggregateHourlyUsage,
  aggregateUsageByBucket,
  getRolling24HourRange,
  buildOverviewUsageTimeline,
  getOverviewGroupLatency,
} from './overview-usage.ts'

describe('dashboard rolling usage range', () => {
  test('covers exactly the previous 24 hours', () => {
    assert.deepEqual(getRolling24HourRange(200_000), {
      start_timestamp: 113_600,
      end_timestamp: 200_000,
    })
  })

  test('merges model rows into ordered hourly totals', () => {
    assert.deepEqual(
      aggregateHourlyUsage([
        { created_at: 7_200, quota: 4 },
        { created_at: 3_600, quota: 2 },
        { created_at: 7_200, quota: 3 },
      ]),
      [
        { created_at: 3_600, quota: 2 },
        { created_at: 7_200, quota: 7 },
      ]
    )
  })

  test('rolls hourly rows into daily buckets for longer ranges', () => {
    assert.deepEqual(
      aggregateUsageByBucket(
        [
          { created_at: 3_600, quota: 2 },
          { created_at: 7_200, quota: 3 },
          { created_at: 90_000, quota: 4 },
        ],
        86_400,
        7
      ),
      [
        { created_at: 0, quota: 5 },
        { created_at: 86_400, quota: 4 },
      ]
    )
  })
})

describe('overview timeline and available measurements', () => {
  test('retains gaps rather than placing sparse hours next to one another', () => {
    assert.deepEqual(
      buildOverviewUsageTimeline(
        [
          { created_at: 3600, quota: 2 },
          { created_at: 3600, quota: 3 },
          { created_at: 10800, quota: 7 },
        ],
        3600,
        14400,
        3600
      ),
      [
        { created_at: 3600, quota: 5 },
        { created_at: 7200, quota: 0 },
        { created_at: 10800, quota: 7 },
        { created_at: 14400, quota: 0 },
      ]
    )
  })

  test('shows all 30 days and excludes out-of-window or invalid rows', () => {
    const day = 86400
    const timeline = buildOverviewUsageTimeline(
      [
        { created_at: day, quota: 1 },
        { created_at: 30 * day, quota: 2 },
        { created_at: 0, quota: 100 },
        { created_at: 31 * day, quota: 100 },
        { created_at: NaN, quota: 100 },
      ],
      day,
      31 * day - 1,
      day
    )
    assert.equal(timeline.length, 30)
    assert.equal(timeline[0].quota, 1)
    assert.equal(timeline.at(-1)?.quota, 2)
    assert.equal(
      timeline.reduce((sum, row) => sum + row.quota, 0),
      3
    )
    assert.deepEqual(buildOverviewUsageTimeline([], 2, 1, 3600), [])
    assert.deepEqual(buildOverviewUsageTimeline([], 1, 2, 0), [])
  })

  test('does not turn missing latency into a zero-duration measurement', () => {
    assert.equal(
      getOverviewGroupLatency([
        { avg_latency_ms: 1000, request_count: 2 },
        { avg_latency_ms: 2000, request_count: 1 },
        { avg_latency_ms: null, request_count: 100 },
      ]),
      4000 / 3
    )
    assert.equal(
      getOverviewGroupLatency([{ avg_latency_ms: null, request_count: 1 }]),
      null
    )
    assert.equal(getOverviewGroupLatency([]), null)
  })
})
