/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

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
import type { SidebarGroupStatusItem } from '@/features/sidebar-group-status/types'
import { buildOverviewModelStatus } from './overview-health'

const groups: SidebarGroupStatusItem[] = [
  {
    group: 'pro',
    display_name: 'Codex Plus-1x-000001',
    status: 'healthy',
    models: [
      {
        model: 'gpt-active',
        status: 'healthy',
        success_rate: 96,
        sample_window: 0.5,
        request_count: 120,
      },
      {
        model: 'gpt-idle',
        status: 'unknown',
        success_rate: null,
        sample_window: 0.5,
        request_count: 0,
      },
    ],
  },
  {
    group: 'standard',
    status: 'slow',
    models: [
      {
        model: 'claude-active',
        status: 'slow',
        success_rate: 72,
        sample_window: 0.5,
        request_count: 40,
      },
    ],
  },
]

describe('buildOverviewModelStatus', () => {
  test('shows active models by request volume and keeps group-status semantics', () => {
    const result = buildOverviewModelStatus(groups)

    assert.deepEqual(
      result.rows.map((row) => row.model),
      ['gpt-active', 'claude-active']
    )
    assert.equal(result.rows[0].group, 'Codex Plus-1x-000001')
    assert.equal(result.rows[0].status, 'healthy')
    assert.equal(result.activeModelCount, 2)
    assert.equal(result.healthyModelCount, 1)
    assert.equal(result.sampleWindow, 0.5)
  })

  test('falls back to observable models when there are no request samples', () => {
    const result = buildOverviewModelStatus([
      {
        group: 'default',
        status: 'unknown',
        models: [groups[0].models[1]],
      },
    ])

    assert.equal(result.rows.length, 1)
    assert.equal(result.rows[0].status, 'unknown')
    assert.equal(result.activeModelCount, 0)
  })
})
