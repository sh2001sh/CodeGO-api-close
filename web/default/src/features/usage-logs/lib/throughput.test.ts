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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { getEffectiveTokenThroughput } from './throughput.ts'

describe('usage log effective token throughput', () => {
  test('uses exact completion tokens over the complete request duration', () => {
    assert.equal(
      getEffectiveTokenThroughput({
        completionTokens: 600,
        totalDurationMs: 20_000,
        useTimeSeconds: 20,
      }),
      30
    )
  })

  test('does not inflate a buffered stream released in a short burst', () => {
    const bufferedStream = {
      completionTokens: 3_000,
      totalDurationMs: 60_000,
      useTimeSeconds: 60,
      streamOutputTokens: 3_000,
      streamOutputTimeMs: 2_000,
    }

    assert.equal(getEffectiveTokenThroughput(bufferedStream), 50)
    assert.equal(
      bufferedStream.streamOutputTokens /
        (bufferedStream.streamOutputTimeMs / 1000),
      1_500
    )
  })

  test('falls back to historical whole-second duration', () => {
    assert.equal(
      getEffectiveTokenThroughput({
        completionTokens: 132,
        useTimeSeconds: 4,
      }),
      33
    )
  })

  test('omits throughput when tokens or duration are unavailable', () => {
    assert.equal(
      getEffectiveTokenThroughput({
        completionTokens: 0,
        totalDurationMs: 10_000,
        useTimeSeconds: 10,
      }),
      null
    )
    assert.equal(
      getEffectiveTokenThroughput({
        completionTokens: 100,
        totalDurationMs: 0,
        useTimeSeconds: 0,
      }),
      null
    )
  })
})
