import assert from 'node:assert/strict'
import { test } from 'node:test'
import type {
  PerformanceGroup,
  PerfModelSummary,
} from '@/features/performance-metrics/types'
import { rankModels, toUsageSeries } from './data'

const model = (
  name: string,
  requests: number,
  tokens: number
): PerfModelSummary => ({
  model_name: name,
  request_count: requests,
  input_tokens: tokens,
  avg_ttft_ms: 0,
  avg_latency_ms: 0,
  success_rate: 0,
  avg_tps: 0,
  cache_hit_rate: 0,
  cache_read_tokens: 0,
  cache_write_tokens: 0,
})

test('model ranking switches metric, keeps zero-token traffic and excludes invalid samples', () => {
  const models = [
    model('beta', 10, 50),
    model('alpha', 10, 200),
    model('no-traffic', 0, 999),
    model('zero-token', 1, 0),
    model('bad', Infinity, 10),
  ]
  assert.deepEqual(
    rankModels(models, 'request_count').map((item) => item.model_name),
    ['alpha', 'beta', 'zero-token']
  )
  assert.deepEqual(
    rankModels(models, 'input_tokens').map((item) => item.model_name),
    ['alpha', 'beta', 'zero-token']
  )
  assert.equal(
    rankModels(models, 'request_count', ' BETA ')[0]?.request_count,
    10
  )
  assert.equal(rankModels(models, 'input_tokens', 'missing').length, 0)
  assert.equal(models[0].model_name, 'beta')
  assert.deepEqual(
    rankModels(
      [model('more-tokens', 2, 400), model('more-requests', 20, 10)],
      'input_tokens'
    ).map((item) => item.model_name),
    ['more-tokens', 'more-requests']
  )
})

test('usage aggregation preserves sparse timestamps, zero observations and group boundaries', () => {
  function group(
    name: string,
    series: Array<[number, number, number]>
  ): PerformanceGroup {
    return {
      group: name,
      avg_ttft_ms: 0,
      avg_latency_ms: 0,
      success_rate: 0,
      avg_tps: 0,
      cache_hit_rate: 0,
      input_tokens: 0,
      cache_read_tokens: 0,
      cache_write_tokens: 0,
      series: series.map(([ts, requests, tokens]) => ({
        ts,
        request_count: requests,
        input_tokens: tokens,
        avg_ttft_ms: 0,
        avg_latency_ms: 0,
        success_rate: 0,
        avg_tps: 0,
        cache_hit_rate: 0,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
      })),
    }
  }
  const groups = [
    group('a', [
      [100, 2, 20],
      [300, 0, 0],
      [400, -1, -1],
    ]),
    group('b', [[100, 5, 50]]),
    group('a', [
      [100, 3, 30],
      [NaN, 8, 80],
    ]),
  ]
  assert.deepEqual(toUsageSeries(groups, 'request_count'), [
    { timestamp: 100, group: 'a', value: 5 },
    { timestamp: 100, group: 'b', value: 5 },
    { timestamp: 300, group: 'a', value: 0 },
  ])
  assert.equal(toUsageSeries(groups, 'input_tokens')[0].value, 50)
  assert.equal(
    toUsageSeries(groups, 'request_count').some(
      (point) => point.timestamp === 200
    ),
    false
  )
  assert.deepEqual(toUsageSeries([], 'request_count'), [])
})
