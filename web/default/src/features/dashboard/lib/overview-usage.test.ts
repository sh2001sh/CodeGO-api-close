import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  aggregateHourlyUsage,
  aggregateUsageByBucket,
  getRolling24HourRange,
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
