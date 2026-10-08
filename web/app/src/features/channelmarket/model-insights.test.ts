import { describe, expect, it } from 'vitest'
import { createElement as element } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { Schema } from '../../lib/types'
import { ChannelDisclosure, ModelInsights, modelInsightsOptions } from './model-insights'
import { MarketComparison } from './market-comparison'

const empty: Schema['ChannelMarketInsights'] = {
  group_id: 'group-7',
  display_id: '7',
  model: 'model-a',
  window_hours: 24,
  request_count: 0n,
  success_count: 0n,
  independent_consumers: 0n,
  performance_samples: 0n,
  failure_counts: [],
}

describe('public model evidence', () => {
  it('separates caches by group, model and statistics window', () => {
    expect(modelInsightsOptions('group-7', 'model-a', 24).queryKey).not.toEqual(
      modelInsightsOptions('group-7', 'model-b', 24).queryKey,
    )
    expect(modelInsightsOptions('group-7', 'model-a', 24).queryKey).not.toEqual(
      modelInsightsOptions('group-7', 'model-a', 168).queryKey,
    )
  })
  it('shows unknown performance for zero samples without implying a perfect success rate', () => {
    const client = new QueryClient()
    client.setQueryData(modelInsightsOptions('group-7', 'model-a', 24).queryKey, empty)
    const html = renderToStaticMarkup(
      element(
        QueryClientProvider,
        { client },
        element(ModelInsights, { group: 'group-7', model: 'model-a' }),
      ),
    )
    expect(html).toContain('所选模型暂无有效调用样本')
    expect(html).toContain('暂无数据')
    expect(html).not.toContain('100%')
    expect(html).not.toContain('0 ms')
    client.clear()
  })
  it('shows exact large counts and measured latency while retaining self-declaration limits', () => {
    const client = new QueryClient()
    client.setQueryData(modelInsightsOptions('group-7', 'model-a', 24).queryKey, {
      ...empty,
      request_count: '9007199254740993',
      success_rate: 0.9,
      ttft_p50_ms: 123,
      failure_counts: [{ category: 'timeout', count: 1 }],
    })
    const html = renderToStaticMarkup(
      element(
        QueryClientProvider,
        { client },
        element(ModelInsights, { group: 'group-7', model: 'model-a' }),
      ),
    )
    expect(html).toContain('9007199254740993')
    expect(html).toContain('123 ms')
    expect(html).toContain('请求超时')
    expect(html).toContain('未经平台独立核验')
    client.clear()
  })
  it('labels owner assertions and does not infer undisclosed model abilities', () => {
    const html = renderToStaticMarkup(
      element(ChannelDisclosure, {
        model: 'model-a',
        disclosure: {
          source_kind: 'direct',
          regions: ['HK'],
          retention: 'none',
          training: 'no',
          models: [],
          provenance: 'owner_declared',
          updated_at: '2026-10-07T12:00:00Z',
        },
      }),
    )
    expect(html).toContain('声明不留存')
    expect(html).toContain('渠道主自行声明')
    expect(html).toContain('未披露')
    expect(html).not.toContain('平台已核验')
  })
  it('compares the effective model quote without applying a listing multiplier twice', () => {
    const client = new QueryClient()
    client.setQueryData(modelInsightsOptions('group-7', 'model-a', 24).queryKey, empty)
    const group: Schema['ChannelMarketChannelView'] = {
      id: '7',
      group_id: 'group-7',
      system_display_name: 'Public seven',
      public_slug: 'seven',
      multiplier: 10,
      verification_status: 'passed',
      max_concurrency: 2,
      user_max_concurrency: 1,
      qps: 2,
      maintenance_window: '',
      model_consistency_status: 'unknown',
      submitted_source_label: '',
      source_label_status: '',
      source_label_review_reason: '',
      sensitive_word_interception_enabled: null,
      multiplier_card_supported: false,
      multiplier_card_user_enabled: false,
      auto_probe_enabled: false,
      auto_probe_interval_minutes: 60,
      auto_probe_model: '',
      verification_stage: '',
      verification_detector_version: '',
      model_verification_results: [],
      internal_channel_id: 7n,
      owner_user_id: 1n,
      provider_type: 'openai',
      approved_source_label: '',
      declared_models: ['model-a'],
      model_prices: {},
      multiplier_ppm: 10000000n,
      visibility: 'public',
      lifecycle_status: 'active',
      last_review_reason: '',
      created_at: '2026-10-07T12:00:00Z',
      updated_at: '2026-10-07T12:00:00Z',
      routing_group: 'group-7',
      recent_request_bucket_seconds: 3600,
      recent_request_series: [],
      rating: { average_score: 0, rating_count: 0n },
      effective_model_prices: {
        'model-a': {
          mode: 'token',
          unit: 'tokens',
          input_per_million: '2',
          output_per_million: '8',
          cache_read_per_million: '0.2',
          cache_write_per_million: '1',
          per_unit: '0',
        },
      },
    }
    const html = renderToStaticMarkup(
      element(
        QueryClientProvider,
        { client },
        element(MarketComparison, {
          groups: [group],
          model: 'model-a',
          onRemove: () => {},
          onClear: () => {},
          onInspect: () => {},
        }),
      ),
    )
    expect(html).toContain('6 credits')
    expect(html).not.toContain('60 credits')
    expect(html).toContain('分组 ID')
    expect(html).toContain('再选择一个支持相同模型')
    client.clear()
  })
})
