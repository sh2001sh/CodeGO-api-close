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

export interface StreamThroughputInput {
  isStream: boolean
  completionTokens: number
  generationTimeMs?: number | null
  timingVersion?: number | null
}

/** Output-token throughput, measured before billing and background work. */
export function getStreamTokenThroughput(
  input: StreamThroughputInput
): number | null {
  if (
    !input.isStream ||
    input.timingVersion !== 2 ||
    !Number.isFinite(input.completionTokens) ||
    input.completionTokens <= 0 ||
    input.generationTimeMs == null ||
    !Number.isFinite(input.generationTimeMs) ||
    input.generationTimeMs <= 0
  )
    return null
  return (input.completionTokens * 1000) / input.generationTimeMs
}

export interface FirstOutputTimingInput {
  frt?: number
  e2e_ttft_ms?: number
  response_start_ms?: number
  first_byte_trace?: {
    e2e_first_text_ms?: number
    total_text_ms?: number
  }
}

/** Lifecycle events must not be presented as model text or output. */
export function getFirstOutputTiming(input: FirstOutputTimingInput | null): {
  milliseconds: number
  label: 'First text' | 'First output' | 'Response started'
} | null {
  if (!input) return null
  const candidates = [
    [input.first_byte_trace?.e2e_first_text_ms, 'First text'],
    [input.first_byte_trace?.total_text_ms, 'First text'],
    [input.e2e_ttft_ms, 'First output'],
    [input.frt, 'First output'],
    [input.response_start_ms, 'Response started'],
  ] as const
  for (const [milliseconds, label] of candidates) {
    if (
      milliseconds != null &&
      Number.isFinite(milliseconds) &&
      milliseconds >= 0
    ) {
      return { milliseconds, label }
    }
  }
  return null
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
