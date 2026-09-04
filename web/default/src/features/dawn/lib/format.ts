/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com.
*/

/** 0–1 → 百分数文本；空值输出横杠。 */
export function pct(value: number | null | undefined, digits = 1): string {
  if (value == null || !Number.isFinite(value)) return '—'
  const scaled = value <= 1 ? value * 100 : value
  return `${scaled.toFixed(digits)}`
}

/** Normalize API success rates, which may be returned as either 0–1 or 0–100. */
export function successRatePercent(
  value: number | null | undefined
): number | null {
  if (value == null || !Number.isFinite(value)) return null
  return value <= 1 ? value * 100 : value
}

/** 毫秒 → 秒文本。 */
export function sec(ms: number | null | undefined, digits = 2): string {
  if (ms == null || !Number.isFinite(ms)) return '—'
  return (ms / 1000).toFixed(digits)
}

/** 请求数缩写：182000 → 182K。 */
export function compactCount(value: number | null | undefined): string {
  if (value == null || !Number.isFinite(value)) return '—'
  if (value === 0) return '0'
  if (value >= 1_000_000)
    return `${(value / 1_000_000).toFixed(value >= 10_000_000 ? 0 : 1)}M`
  if (value >= 1_000)
    return `${(value / 1_000).toFixed(value >= 10_000 ? 0 : 1)}K`
  return String(Math.round(value))
}

export function fmtInt(value: number | null | undefined): string {
  if (value == null || !Number.isFinite(value)) return '—'
  return value.toLocaleString('zh-CN', { maximumFractionDigits: 0 })
}

export function fmtMoney(
  value: number | null | undefined,
  prefix = '$'
): string {
  if (value == null || !Number.isFinite(value)) return '—'
  return `${prefix}${value.toLocaleString('zh-CN', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })}`
}

export function dash<T>(
  value: T | null | undefined,
  render: (value: T) => string
): string {
  return value == null ? '—' : render(value)
}

/** 时段倍率窗口文本。 */
export function windowLabel(
  startTimestamp: number,
  endTimestamp: number
): string {
  const fmt = (ts: number) => {
    const date = new Date(ts < 1e12 ? ts * 1000 : ts)
    return `${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`
  }
  return `${fmt(startTimestamp)}–${fmt(endTimestamp)}`
}

export type HealthState = 'ok' | 'warn' | 'bad' | 'idle'

/** 由分组窗口指标推导状态：无请求 → idle；成功率 ≥99 稳定，98–99 波动，<98 异常。 */
export function healthState(input: {
  requestCount: number
  successRate: number | null
}): HealthState {
  if (!input.requestCount || input.successRate == null) return 'idle'
  const rate =
    input.successRate <= 1 ? input.successRate * 100 : input.successRate
  if (rate >= 99) return 'ok'
  if (rate >= 98) return 'warn'
  return 'bad'
}

export const STATE_LABEL: Record<HealthState, string> = {
  ok: '稳定',
  warn: '波动',
  bad: '异常',
  idle: '无请求',
}
