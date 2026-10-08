import { describe, expect, it } from 'vitest'
import type { Schema } from '../../lib/types'
import { boardRows } from '../home/board-data'
import { groupProbe, groupTone, latestProbeAt, overallTone } from './status-helpers'

type Group = Schema['ChannelMarketChannelView']
const fixtureNow = Date.now()
const oldTime = new Date(fixtureNow - 20 * 60_000).toISOString()
const newTime = new Date(fixtureNow - 5 * 60_000).toISOString()
const result = (model: string, status = 'passed', testedAt = oldTime, latency = 100) => ({
  model,
  status,
  tested_at: testedAt,
  latency_ms: latency,
  listed: true,
})
function group(overrides: Partial<Group> = {}): Group {
  return {
    rating: { average_score: 0, rating_count: 0 },
    id: 'channel-1',
    internal_channel_id: 1,
    owner_user_id: 1,
    group_id: 'stable-group-1',
    routing_group: 'market_stable-group-1',
    effective_model_prices: {},
    recent_request_bucket_seconds: 3600,
    recent_request_series: [],
    public_slug: 'stable-public-1',
    system_display_name: 'Technical pool',
    provider_type: 'openai',
    approved_source_label: '',
    declared_models: ['gpt-test'],
    model_prices: {},
    multiplier_ppm: 1_000_000,
    multiplier: 1,
    visibility: 'public',
    lifecycle_status: 'active',
    verification_status: 'passed',
    last_review_reason: '',
    created_at: oldTime,
    updated_at: oldTime,
    model_consistency_status: '',
    submitted_source_label: '',
    source_label_status: '',
    source_label_review_reason: '',
    max_concurrency: 0,
    user_max_concurrency: 0,
    qps: 0,
    maintenance_window: '',
    sensitive_word_interception_enabled: null,
    multiplier_card_supported: false,
    multiplier_card_user_enabled: false,
    auto_probe_enabled: false,
    auto_probe_interval_minutes: 30,
    auto_probe_model: '',
    verification_stage: 'completed',
    verification_detector_version: '',
    model_verification_results: [result('gpt-test')],
    ...overrides,
  }
}

describe('public connectivity observations', () => {
  it('recognizes the backend passed verification enum', () => {
    expect(boardRows([group()])[0].verified).toBe(true)
    expect(boardRows([group()])[0]).toMatchObject({ id: 'channel-1', groupId: 'stable-group-1' })
    expect(boardRows([group({ verification_status: 'pending' })])[0].verified).toBe(false)
  })

  it('keeps status, time and latency aligned when a newer automatic probe fails', () => {
    const failed = group({ auto_probe_last_status: 'failed', auto_probe_last_at: newTime })
    expect(groupProbe(failed)).toEqual({
      tone: 'degraded',
      testedAt: newTime,
      latencyMs: null,
    })
    expect(boardRows([failed])[0]).toMatchObject({
      health: 'down',
      probedAt: newTime,
      latencyMs: null,
      verified: true,
    })
    expect(groupTone(failed)).toBe('degraded')
    expect(latestProbeAt(failed)).toBe(newTime)
  })

  it('uses a newer complete verification instead of an older automatic failure', () => {
    const recovered = group({
      model_verification_results: [result('gpt-test', 'passed', newTime)],
      auto_probe_last_status: 'failed',
      auto_probe_last_at: oldTime,
    })
    expect(groupProbe(recovered)).toEqual({
      tone: 'healthy',
      testedAt: newTime,
      latencyMs: 100,
    })
  })

  it('does not call a partially failed group healthy or reuse its successful latency', () => {
    const partial = group({
      declared_models: ['gpt-test', 'claude-test'],
      model_verification_results: [result('gpt-test'), result('claude-test', 'failed')],
    })
    expect(groupTone(partial)).toBe('degraded')
    expect(boardRows([partial])[0]).toMatchObject({ health: 'down', latencyMs: null })
    expect(overallTone([group(), partial])).toBe('degraded')
  })

  it('does not erase a failure in another model with one passing automatic sample', () => {
    const partial = group({
      declared_models: ['gpt-test', 'claude-test'],
      auto_probe_model: 'gpt-test',
      auto_probe_last_status: 'passed',
      auto_probe_last_at: newTime,
      model_verification_results: [result('gpt-test'), result('claude-test', 'failed')],
    })
    expect(groupProbe(partial)).toEqual({
      tone: 'degraded',
      testedAt: newTime,
      latencyMs: null,
    })
  })

  it('preserves a failed verification even when the individual requests passed', () => {
    const failed = group({ verification_status: 'failed', verification_completed_at: newTime })
    expect(groupProbe(failed)).toEqual({
      tone: 'degraded',
      testedAt: newTime,
      latencyMs: null,
    })
  })

  it('does not reuse an older automatic pass after a newer failed verification with no model results', () => {
    expect(
      groupProbe(
        group({
          verification_status: 'failed',
          verification_completed_at: newTime,
          model_verification_results: [],
          auto_probe_last_status: 'passed',
          auto_probe_last_at: oldTime,
        }),
      ),
    ).toEqual({ tone: 'degraded', testedAt: newTime, latencyMs: null })
  })

  it('allows a passing automatic sample to recover the same failed model', () => {
    const recovered = group({
      auto_probe_model: 'gpt-test',
      auto_probe_last_status: 'passed',
      auto_probe_last_at: newTime,
      model_verification_results: [result('gpt-test', 'failed')],
    })
    expect(groupProbe(recovered)).toEqual({
      tone: 'healthy',
      testedAt: newTime,
      latencyMs: null,
    })
  })

  it('shows no data for absent, incomplete or malformed observations', () => {
    for (const unknown of [
      group({ model_verification_results: [] }),
      group({ model_verification_results: [result('gpt-test', 'queued')] }),
      group({ model_verification_results: [result('gpt-test', 'passed', 'invalid')] }),
      group({ model_verification_results: [], auto_probe_last_status: 'failed' }),
    ]) {
      expect(groupTone(unknown)).toBe('unknown')
      expect(boardRows([unknown])[0]).toMatchObject({ health: 'idle', latencyMs: null })
    }
    expect(overallTone([])).toBe('unknown')
  })

  it('compares actual instants across timezone offsets rather than date strings', () => {
    const recentAutomatic = '2026-10-06T01:00:00+08:00'
    const earlierManual = '2026-10-05T16:00:00Z'
    const failed = group({
      model_verification_results: [result('gpt-test', 'passed', earlierManual)],
      auto_probe_last_status: 'failed',
      auto_probe_last_at: recentAutomatic,
    })
    expect(groupProbe(failed, Date.parse(recentAutomatic) + 60_000)).toMatchObject({
      tone: 'degraded',
      testedAt: recentAutomatic,
    })
  })

  it('does not present an overdue scheduled probe as current availability', () => {
    const scheduled = group({
      auto_probe_enabled: true,
      auto_probe_interval_minutes: 30,
      auto_probe_last_status: 'passed',
      auto_probe_last_at: newTime,
    })
    const observedAt = Date.parse(newTime)
    expect(groupProbe(scheduled, observedAt + 60 * 60_000).tone).toBe('healthy')
    expect(groupProbe(scheduled, observedAt + 60 * 60_000 + 1)).toEqual({
      tone: 'unknown',
      testedAt: newTime,
      latencyMs: null,
    })
    expect(
      groupProbe({ ...scheduled, auto_probe_enabled: false }, observedAt + 7 * 86_400_000).tone,
    ).toBe('unknown')
  })

  it('requires complete declared-model coverage instead of treating one passed model as the group', () => {
    const observedAt = Date.parse(newTime)
    const partial = group({ declared_models: ['gpt-test', 'claude-test'] })
    expect(groupProbe(partial, observedAt).tone).toBe('unknown')
    expect(
      groupProbe(
        {
          ...partial,
          auto_probe_model: 'gpt-test',
          auto_probe_last_at: newTime,
          auto_probe_last_status: 'passed',
          model_verification_results: [],
        },
        observedAt,
      ).tone,
    ).toBe('unknown')
    expect(
      groupProbe(
        {
          ...partial,
          auto_probe_model: 'gpt-test',
          auto_probe_last_at: newTime,
          auto_probe_last_status: 'passed',
          model_verification_results: [
            result('gpt-test'),
            result('claude-test', 'passed', new Date(observedAt - 86_400_001).toISOString()),
          ],
        },
        observedAt,
      ).tone,
    ).toBe('unknown')
  })

  it('retains historical times but hides stale or implausibly future manual availability and latency', () => {
    const observedAt = Date.parse(oldTime)
    const manual = group()
    expect(groupProbe(manual, observedAt + 86_400_000).tone).toBe('healthy')
    expect(groupProbe(manual, observedAt + 86_400_001)).toEqual({
      tone: 'unknown',
      testedAt: oldTime,
      latencyMs: null,
    })
    expect(groupProbe(manual, observedAt - 5 * 60_000).tone).toBe('healthy')
    expect(groupProbe(manual, observedAt - 5 * 60_000 - 1)).toEqual({
      tone: 'unknown',
      testedAt: oldTime,
      latencyMs: null,
    })
  })

  it('grants a minimum fifteen-minute freshness window to short automatic intervals', () => {
    const observedAt = Date.parse(newTime)
    const automatic = group({
      auto_probe_enabled: true,
      auto_probe_interval_minutes: 1,
      auto_probe_last_at: newTime,
      auto_probe_last_status: 'passed',
    })
    expect(groupProbe(automatic, observedAt + 15 * 60_000).tone).toBe('healthy')
    expect(groupProbe(automatic, observedAt + 15 * 60_000 + 1)).toEqual({
      tone: 'unknown',
      testedAt: newTime,
      latencyMs: null,
    })
  })

  it('uses the median valid probe latency only for healthy manual observations', () => {
    expect(
      groupProbe(
        group({
          model_verification_results: [
            result('gpt-test', 'passed', newTime, 100),
            result('claude-test', 'passed', newTime, 300),
            result('qwen-test', 'passed', newTime, 0),
          ],
        }),
      ).latencyMs,
    ).toBe(200)
  })
})
