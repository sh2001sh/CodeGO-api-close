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
import {
  loadSelectableMarketplaceGroups,
  type MarketplaceGroup,
  type MarketplaceGroupPage,
} from './marketplace-group-options'

function createGroup(
  id: string,
  overrides: Partial<MarketplaceGroup> = {}
): MarketplaceGroup {
  return {
    id,
    system_display_name: `Group ${id}`,
    source_label: 'Test source',
    lifecycle_status: 'active',
    verification_status: 'passed',
    multiplier: 1,
    subscription_enabled: true,
    subscription_multiplier: 1,
    models: ['gpt-5.6'],
    ...overrides,
  }
}

function createPage(
  page: number,
  total: number,
  items: MarketplaceGroup[]
): MarketplaceGroupPage {
  return { page, total, items, page_size: 50 }
}

describe('loadSelectableMarketplaceGroups', () => {
  test('loads all 65 groups from two pages', async () => {
    const requestedPages: number[] = []
    const groups = Array.from({ length: 65 }, (_, index) =>
      createGroup(String(index + 1))
    )

    const result = await loadSelectableMarketplaceGroups(async (page) => {
      requestedPages.push(page)
      const start = (page - 1) * 50
      return createPage(page, groups.length, groups.slice(start, start + 50))
    })

    assert.deepEqual(requestedPages, [1, 2])
    assert.equal(result.length, 65)
    assert.equal(result.at(-1)?.id, '65')
  })

  test('stops on an empty page when the reported total changes', async () => {
    const requestedPages: number[] = []
    const firstPage = Array.from({ length: 50 }, (_, index) =>
      createGroup(String(index + 1))
    )

    const result = await loadSelectableMarketplaceGroups(async (page) => {
      requestedPages.push(page)
      if (page === 1) return createPage(page, 100, firstPage)
      return createPage(page, 50, [])
    })

    assert.deepEqual(requestedPages, [1, 2])
    assert.equal(result.length, 50)
  })

  test('de-duplicates IDs repeated across pages', async () => {
    const firstPage = Array.from({ length: 50 }, (_, index) =>
      createGroup(String(index + 1))
    )
    const duplicate = createGroup('50', {
      system_display_name: 'Updated group',
    })

    const result = await loadSelectableMarketplaceGroups(async (page) => {
      if (page === 1) return createPage(page, 51, firstPage)
      return createPage(page, 51, [duplicate, createGroup('51')])
    })

    assert.equal(result.length, 51)
    assert.equal(
      result.find((group) => group.id === '50')?.system_display_name,
      'Updated group'
    )
  })

  test('keeps only groups eligible for API Key selection', async () => {
    const groups = [
      createGroup('active'),
      createGroup('degraded', { lifecycle_status: 'degraded' }),
      createGroup('inactive', { lifecycle_status: 'inactive' }),
      createGroup('unverified', { verification_status: 'pending' }),
      createGroup('mismatch', { gpt56_mapping_status: 'mismatch' }),
      createGroup('insufficient', {
        gpt56_mapping_status: 'insufficient_evidence',
      }),
    ]

    const result = await loadSelectableMarketplaceGroups(async () =>
      createPage(1, groups.length, groups)
    )

    assert.deepEqual(
      result.map((group) => group.id),
      ['active', 'degraded', 'insufficient']
    )
  })
})
