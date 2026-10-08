import { vendorOf, type PublicGroup } from '../../lib/public-catalog'
import { groupProbe } from '../public/status-helpers'

export type BoardHealth = 'up' | 'down' | 'idle'
export type BoardSort = 'multiplier' | 'latency' | 'models'

export type BoardRow = {
  id: string
  groupId: string
  slug: string
  name: string
  provider: string
  /** Multiplier in parts per million, exact. */
  multiplierPpm: bigint
  models: string[]
  vendors: string[]
  verified: boolean
  health: BoardHealth
  /** Median verification probe latency, ms; null when the latest observation has no latency. */
  latencyMs: number | null
  probedAt: string | null
}

const boardHealth: Record<ReturnType<typeof groupProbe>['tone'], BoardHealth> = {
  healthy: 'up',
  degraded: 'down',
  unknown: 'idle',
}

/** One board row per public group. */
export function boardRows(groups: readonly PublicGroup[]): BoardRow[] {
  return groups.map((group) => {
    const models = [...(group.declared_models ?? [])].sort()
    const probe = groupProbe(group)
    return {
      id: group.id,
      groupId: group.group_id,
      slug: group.public_slug,
      name: group.system_display_name || group.public_slug,
      provider: group.approved_source_label || group.provider_type,
      multiplierPpm: BigInt(group.multiplier_ppm),
      models,
      vendors: [...new Set(models.map(vendorOf))],
      verified: group.verification_status === 'passed',
      health: boardHealth[probe.tone],
      latencyMs: probe.latencyMs,
      probedAt: probe.testedAt ?? null,
    }
  })
}

const healthRank: Record<BoardHealth, number> = { up: 0, idle: 1, down: 2 }

export function sortRows(rows: readonly BoardRow[], sort: BoardSort): BoardRow[] {
  return [...rows].sort((a, b) => {
    if (healthRank[a.health] !== healthRank[b.health])
      return healthRank[a.health] - healthRank[b.health]
    if (sort === 'models') return b.models.length - a.models.length
    if (sort === 'latency') return (a.latencyMs ?? Infinity) - (b.latencyMs ?? Infinity)
    return a.multiplierPpm < b.multiplierPpm ? -1 : a.multiplierPpm > b.multiplierPpm ? 1 : 0
  })
}

export function filterRows(rows: readonly BoardRow[], query: string): BoardRow[] {
  const needle = query.trim().toLowerCase()
  if (!needle) return [...rows]
  return rows.filter(
    (row) =>
      row.name.toLowerCase().includes(needle) ||
      row.id.toLowerCase().includes(needle) ||
      row.groupId.toLowerCase().includes(needle) ||
      row.provider.toLowerCase().includes(needle) ||
      row.models.some((model) => model.toLowerCase().includes(needle)) ||
      row.vendors.some((vendor) => vendor.toLowerCase().includes(needle)),
  )
}

/** "0.8x" style multiplier from exact ppm, up to three decimals. */
export function multiplierLabel(ppm: bigint): string {
  const whole = ppm / 1_000_000n
  const fraction = String(ppm % 1_000_000n)
    .padStart(6, '0')
    .slice(0, 3)
    .replace(/0+$/, '')
  return `${whole}${fraction ? `.${fraction}` : ''}×`
}

export function boardSummary(rows: readonly BoardRow[]) {
  const models = new Set(rows.flatMap((row) => row.models))
  const vendors = new Set(rows.flatMap((row) => row.vendors))
  return {
    groups: rows.length,
    up: rows.filter((row) => row.health === 'up').length,
    models: models.size,
    vendors: vendors.size,
  }
}
