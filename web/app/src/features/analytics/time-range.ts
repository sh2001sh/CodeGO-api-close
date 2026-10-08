// Time-range preset handling shared by every analytics page. Ranges are kept
// as ISO strings so they serialize directly into `from`/`to` query params.

export type RangePreset = '24h' | '7d' | '30d' | 'custom'

export type Range = { from: string; to: string }

const presetHours: Record<Exclude<RangePreset, 'custom'>, number> = {
  '24h': 24,
  '7d': 24 * 7,
  '30d': 24 * 30,
}

export function rangeFromPreset(preset: Exclude<RangePreset, 'custom'>, now = new Date()): Range {
  const to = now.toISOString()
  const from = new Date(now.getTime() - presetHours[preset] * 3_600_000).toISOString()
  return { from, to }
}

export const rangePresetOptions: readonly { value: RangePreset; label: string }[] = [
  { value: '24h', label: '最近 24 小时' },
  { value: '7d', label: '最近 7 天' },
  { value: '30d', label: '最近 30 天' },
  { value: 'custom', label: '自定义' },
]

/** `<input type="datetime-local">` has no timezone; interpret it as local time. */
export function localInputToISO(value: string): string {
  if (!value) return ''
  const parsed = new Date(value)
  return Number.isNaN(parsed.getTime()) ? '' : parsed.toISOString()
}

export function isoToLocalInput(value: string): string {
  if (!value) return ''
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return ''
  const offset = parsed.getTimezoneOffset()
  return new Date(parsed.getTime() - offset * 60_000).toISOString().slice(0, 16)
}
