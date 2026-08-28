import type { MarketplaceAutoRoutePool, MarketplaceGroup } from '../types'

const now = Date.now()

function group(input: Partial<MarketplaceGroup> & Pick<MarketplaceGroup, 'id' | 'system_display_name' | 'source_label' | 'provider_type' | 'models' | 'multiplier'>): MarketplaceGroup {
  return {
    channel_id: `mock-channel-${input.id}`,
    public_slug: input.id,
    source_type: input.source_type ?? 'marketplace_user',
    credit_pool_policy: 'shared',
    lifecycle_status: 'active',
    verification_status: 'passed',
    verification_stage: 'completed',
    verification_summary: '最近一次检测通过',
    verification_detector_version: 'mock-1.0',
    verification_started_at: new Date(now - 3600_000).toISOString(),
    verification_completed_at: new Date(now - 1800_000).toISOString(),
    subscription_enabled: false,
    subscription_multiplier: input.multiplier,
    model_verification_results: input.models.map((model) => ({ model, status: 'passed', listed: true, latency_ms: input.avg_latency_ms ?? 800, tested_at: new Date(now - 1800_000).toISOString() })),
    connectivity_test_status: 'passed',
    connectivity_test_checked_at: new Date(now - 1800_000).toISOString(),
    remote_compaction_support: 'v1_v2',
    model_consistency_status: 'passed',
    gpt56_mapping_results: [],
    gpt56_mapping_status: 'matched',
    gpt56_mapping_checked_at: new Date(now - 1800_000).toISOString(),
    auto_probe_enabled: true,
    auto_probe_interval_minutes: 10,
    auto_probe_model: input.models[0] ?? '',
    channel_feedback: { passed: 12, failed: 1, questionable: 2, total: 15, viewer_status: 'passed' },
    can_submit_channel_feedback: true,
    channel_feedback_permission: 'allowed',
    rank: input.rank ?? 1,
    score: input.score ?? 90,
    success_rate: input.success_rate ?? 0.98,
    wilson_success_rate: input.wilson_success_rate ?? 0.95,
    avg_ttft_ms: input.avg_ttft_ms ?? 420,
    attempt_ttft_p50_ms: input.attempt_ttft_p50_ms ?? 390,
    attempt_ttft_p95_ms: input.attempt_ttft_p95_ms ?? 980,
    e2e_ttft_p50_ms: input.e2e_ttft_p50_ms ?? 520,
    e2e_ttft_p95_ms: input.e2e_ttft_p95_ms ?? 1200,
    latency_sample_count: 120,
    avg_latency_ms: input.avg_latency_ms ?? 820,
    avg_tps: input.avg_tps ?? 68,
    cache_hit_rate: input.cache_hit_rate ?? 0.32,
    latest_request_status: 'healthy',
    recent_request_series: Array.from({ length: 12 }, (_, index) => ({ ts: now - (11 - index) * 300_000, success_rate: 0.94 + (index % 4) * 0.01, request_count: 18 + index })),
    recent_request_bucket_seconds: 300,
    request_count: 240 + (input.rank ?? 1) * 17,
    max_concurrency: 32,
    user_max_concurrency: 8,
    current_concurrency: 2,
    observing: false,
    updated_at: new Date(now - 600_000).toISOString(),
    ...input,
  }
}

export const MOCK_MARKETPLACE_GROUPS: MarketplaceGroup[] = [
  group({ id: 'mock-official-fast', system_display_name: '官方 · GPT-5.2 Fast', source_label: '官方来源', provider_type: 'OpenAI', models: ['gpt-5.2', 'gpt-5.2-mini'], multiplier: 1, rank: 1, score: 96, avg_ttft_ms: 280, avg_latency_ms: 610, avg_tps: 92 }),
  group({ id: 'mock-community-steady', system_display_name: '社区 · 稳定编程组', source_label: '社区贡献', provider_type: 'OpenAI Compatible', models: ['gpt-5.2', 'claude-3-7-sonnet'], multiplier: 1.15, rank: 2, score: 93, avg_ttft_ms: 360, avg_latency_ms: 740, avg_tps: 78 }),
  group({ id: 'mock-cloud-balanced', system_display_name: '云端 · Balanced', source_label: '第三方市场', provider_type: 'Azure OpenAI', models: ['gpt-5.2', 'gpt-4.1'], multiplier: 1.25, rank: 3, score: 90, avg_ttft_ms: 430, avg_latency_ms: 860, avg_tps: 70 }),
  group({ id: 'mock-lab-low-cost', system_display_name: '实验室 · 低倍率', source_label: '第三方市场', provider_type: 'DeepSeek', models: ['deepseek-v3', 'deepseek-r1'], multiplier: 0.88, rank: 4, score: 87, avg_ttft_ms: 510, avg_latency_ms: 980, avg_tps: 62 }),
  group({ id: 'mock-edge-cn', system_display_name: '边缘节点 · 华东', source_label: '社区贡献', provider_type: 'OpenAI Compatible', models: ['gpt-5.2', 'qwen3-coder'], multiplier: 1.08, rank: 5, score: 84, avg_ttft_ms: 470, avg_latency_ms: 900, avg_tps: 66 }),
  group({ id: 'mock-research', system_display_name: '研究组 · Long Context', source_label: '第三方市场', provider_type: 'Anthropic Compatible', models: ['claude-3-7-sonnet', 'gemini-2.5-pro'], multiplier: 1.38, rank: 6, score: 82, avg_ttft_ms: 640, avg_latency_ms: 1180, avg_tps: 54 }),
]

export const MOCK_AUTO_ROUTE_POOL: MarketplaceAutoRoutePool = {
  token_group: 'auto',
  selected_count: 2,
  items: MOCK_MARKETPLACE_GROUPS.slice(0, 2).map((item, index) => ({ group_id: item.id, source_type: item.source_type === 'official' ? 'official' : 'marketplace_user', public_slug: item.public_slug, system_display_name: item.system_display_name, source_label: item.source_label, lifecycle_status: item.lifecycle_status, multiplier: item.multiplier, availability: 0.99 - index * 0.01, success_rate: item.success_rate, cache_hit_rate: item.cache_hit_rate, avg_latency_ms: item.avg_latency_ms, latest_request_status: item.latest_request_status, metrics_available: true, route_score: item.score, observing: false, request_count: item.request_count, models: item.models, selected: true, priority: index + 1 })),
  config: { strategy: 'priority', max_attempts: 3, failure_cooldown_seconds: 30, max_multiplier: 0 },
}
