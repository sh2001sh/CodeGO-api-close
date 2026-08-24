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

For commercial licensing, please contact support@quantumnous.com
*/
export interface UsageLogThroughputInput {
  completionTokens: number
  totalDurationMs?: number | null
  useTimeSeconds: number
}

/**
 * Calculates effective output throughput over the complete request lifetime.
 * The second-resolution use time keeps historical logs compatible.
 */
export function getEffectiveTokenThroughput({
  completionTokens,
  totalDurationMs,
  useTimeSeconds,
}: UsageLogThroughputInput): number | null {
  if (!Number.isFinite(completionTokens) || completionTokens <= 0) return null

  const durationMs =
    totalDurationMs != null &&
    Number.isFinite(totalDurationMs) &&
    totalDurationMs > 0
      ? totalDurationMs
      : useTimeSeconds > 0 && Number.isFinite(useTimeSeconds)
        ? useTimeSeconds * 1000
        : 0

  if (durationMs <= 0) return null
  return (completionTokens * 1000) / durationMs
}
