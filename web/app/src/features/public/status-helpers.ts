// Status derivation from the public group-status snapshot. The v3 API only
// exposes the latest verification run per group (no time-bucketed history),
// so "status" here means "result of the most recent connectivity probe".
import type { Schema } from '../../lib/types'

export type GroupStatusTone = 'healthy' | 'degraded' | 'unknown'

export const toneLabel: Record<GroupStatusTone, string> = {
  healthy: '正常',
  degraded: '异常',
  unknown: '暂无数据',
}

export const toneBadge: Record<GroupStatusTone, 'success' | 'danger' | 'neutral'> = {
  healthy: 'success',
  degraded: 'danger',
  unknown: 'neutral',
}

export const overallCopy: Record<GroupStatusTone, string> = {
  healthy: '最近探测未发现异常',
  degraded: '部分分组探测异常',
  unknown: '暂无足够探测数据',
}

export function modelTone(status: string | undefined): GroupStatusTone {
  if (status === 'passed') return 'healthy'
  if (status === 'failed') return 'degraded'
  return 'unknown'
}

function timestamp(value: string | null | undefined): number | undefined {
  if (!value) return undefined
  const time = Date.parse(value)
  return Number.isFinite(time) ? time : undefined
}

function median(values: number[]): number | null {
  if (values.length === 0) return null
  const sorted = [...values].sort((a, b) => a - b)
  const middle = Math.floor(sorted.length / 2)
  return sorted.length % 2 ? sorted[middle] : Math.round((sorted[middle - 1] + sorted[middle]) / 2)
}

/** The same probe observation drives the status, timestamp and latency everywhere. */
export function groupProbe(
  group: Schema['ChannelMarketChannelView'],
  now = Date.now(),
): {
  tone: GroupStatusTone
  testedAt: string | undefined
  latencyMs: number | null
} {
  const observation = (
    tone: GroupStatusTone,
    testedAt: string | undefined,
    latencyMs: number | null,
  ) => {
    const time = timestamp(testedAt)
    const interval = group.auto_probe_interval_minutes > 0 ? group.auto_probe_interval_minutes : 30
    // Expired and implausibly future observations retain their date without
    // making a current availability or latency claim.
    const ageLimit = group.auto_probe_enabled ? Math.max(15, interval * 2) * 60_000 : 86_400_000
    const overdue = time !== undefined && (now - time > ageLimit || time > now + 5 * 60_000)
    return {
      tone: overdue ? ('unknown' as const) : tone,
      testedAt,
      latencyMs: overdue ? null : latencyMs,
    }
  }
  const results = group.model_verification_results ?? []
  const datedResults = results
    .map((result) => ({ result, time: timestamp(result.tested_at) }))
    .filter(
      (item): item is { result: (typeof results)[number]; time: number } => item.time !== undefined,
    )
  const latestModel = datedResults.reduce<(typeof datedResults)[number] | undefined>(
    (latest, item) => (!latest || item.time > latest.time ? item : latest),
    undefined,
  )
  const completedTime =
    group.verification_stage === 'completed'
      ? timestamp(group.verification_completed_at)
      : undefined
  const manualTime =
    completedTime !== undefined && (!latestModel || completedTime > latestModel.time)
      ? completedTime
      : latestModel?.time
  const manualAt =
    manualTime === completedTime && completedTime !== undefined
      ? (group.verification_completed_at ?? undefined)
      : latestModel?.result.tested_at
  const automaticTime = timestamp(group.auto_probe_last_at)
  const declared = [...new Set(group.declared_models ?? [])]
  const recent = (time: number | undefined) =>
    time !== undefined && now - time <= 86_400_000 && time <= now + 5 * 60_000
  const completeCoverage = (automaticTarget?: string) =>
    declared.length > 0 &&
    declared.every((model) => {
      if (model === automaticTarget) return recent(automaticTime)
      return datedResults.some(
        ({ result, time }) => result.model === model && result.status === 'passed' && recent(time),
      )
    })
  // An automatic probe samples one model. A passing sample cannot erase a known
  // failure in another model; it also has no latency in the public DTO.
  if (automaticTime !== undefined && (manualTime === undefined || automaticTime > manualTime)) {
    let tone = modelTone(group.auto_probe_last_status)
    if (tone === 'healthy') {
      const target = group.auto_probe_model || [...(group.declared_models ?? [])].sort()[0]
      if (results.some((result) => result.model !== target && result.status === 'failed'))
        tone = 'degraded'
      else if (
        group.verification_status === 'failed' &&
        !results.some((result) => result.model === target && result.status === 'failed')
      )
        tone = 'degraded'
      else if (results.some((result) => result.model !== target && result.status !== 'passed'))
        tone = 'unknown'
      else if (!completeCoverage(target)) tone = 'unknown'
    }
    return observation(tone, group.auto_probe_last_at ?? undefined, null)
  }
  if (manualTime === undefined) return { tone: 'unknown', testedAt: undefined, latencyMs: null }
  const tone =
    (group.verification_stage === 'completed' && group.verification_status === 'failed') ||
    results.some((item) => item.status === 'failed')
      ? 'degraded'
      : results.length > 0 &&
          datedResults.length === results.length &&
          results.every((item) => item.status === 'passed') &&
          completeCoverage()
        ? 'healthy'
        : 'unknown'
  const latencyMs =
    tone === 'healthy'
      ? median(
          results
            .map((result) => Number(result.latency_ms))
            .filter((value) => Number.isFinite(value) && value > 0),
        )
      : null
  return observation(tone, manualAt, latencyMs)
}

export function groupTone(group: Schema['ChannelMarketChannelView']): GroupStatusTone {
  return groupProbe(group).tone
}

export function overallTone(
  groups: readonly Schema['ChannelMarketChannelView'][],
): GroupStatusTone {
  if (groups.length === 0) return 'unknown'
  const tones = groups.map((group) => groupTone(group))
  if (tones.some((tone) => tone === 'degraded')) return 'degraded'
  if (tones.every((tone) => tone === 'healthy')) return 'healthy'
  return 'unknown'
}

export function latestProbeAt(group: Schema['ChannelMarketChannelView']): string | undefined {
  return groupProbe(group).testedAt
}
