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

export const MARKETPLACE_GROUP_PAGE_SIZE = 50

export interface MarketplaceGroup {
  id: string
  system_display_name: string
  source_label: string
  lifecycle_status: string
  verification_status: string
  multiplier: number
  subscription_enabled: boolean
  subscription_multiplier: number
  success_rate?: number
  request_count?: number
  gpt56_mapping_status?: 'matched' | 'mismatch' | 'insufficient_evidence' | ''
  models: string[]
}

export interface MarketplaceGroupPage {
  items: MarketplaceGroup[]
  total: number
  page: number
  page_size: number
}

type FetchMarketplaceGroupPage = (
  page: number,
  pageSize: number
) => Promise<MarketplaceGroupPage>

function isSelectableMarketplaceGroup(group: MarketplaceGroup): boolean {
  return (
    ['active', 'degraded'].includes(group.lifecycle_status) &&
    group.verification_status === 'passed' &&
    group.gpt56_mapping_status !== 'mismatch'
  )
}

/** Loads every marketplace page and returns the unique, selectable groups. */
export async function loadSelectableMarketplaceGroups(
  fetchPage: FetchMarketplaceGroupPage
): Promise<MarketplaceGroup[]> {
  const groupsById = new Map<string, MarketplaceGroup>()
  let page = 1

  while (true) {
    const result = await fetchPage(page, MARKETPLACE_GROUP_PAGE_SIZE)
    for (const group of result.items) groupsById.set(group.id, group)

    const responsePageSize =
      result.page_size > 0 ? result.page_size : MARKETPLACE_GROUP_PAGE_SIZE
    const totalPages = Math.ceil(Math.max(result.total, 0) / responsePageSize)
    const reachedReportedEnd = totalPages > 0 && page >= totalPages
    const reachedPhysicalEnd = result.items.length < responsePageSize

    if (reachedReportedEnd || reachedPhysicalEnd) break
    page += 1
  }

  return Array.from(groupsById.values()).filter(isSelectableMarketplaceGroup)
}
