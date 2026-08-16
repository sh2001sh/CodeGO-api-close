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
import type {
  SidebarGroupAvailabilityStatus,
  SidebarGroupModelStatusItem,
  SidebarGroupStatusItem,
} from '@/features/sidebar-group-status/types'

const DEFAULT_MODEL_LIMIT = 5

export type OverviewModelStatusRow = SidebarGroupModelStatusItem & {
  group: string
}

export type OverviewModelStatus = {
  rows: OverviewModelStatusRow[]
  activeModelCount: number
  healthyModelCount: number
  sampleWindow: number | null
}

const STATUS_WEIGHT: Record<SidebarGroupAvailabilityStatus, number> = {
  degraded: 0,
  slow: 1,
  unknown: 2,
  healthy: 3,
}

/** Builds the compact model-level status shown on the dashboard overview. */
export function buildOverviewModelStatus(
  groups: SidebarGroupStatusItem[],
  limit = DEFAULT_MODEL_LIMIT
): OverviewModelStatus {
  const allModels = groups.flatMap((group) =>
    group.models.map((model) => ({
      ...model,
      group: group.display_name || group.group,
    }))
  )
  const activeModels = allModels.filter(
    (model) => (model.request_count ?? 0) > 0
  )
  const candidates = activeModels.length > 0 ? activeModels : allModels
  const rows = [...candidates]
    .sort((left, right) => {
      const requestDiff = (right.request_count ?? 0) - (left.request_count ?? 0)
      if (requestDiff !== 0) return requestDiff
      const statusDiff =
        STATUS_WEIGHT[left.status] - STATUS_WEIGHT[right.status]
      if (statusDiff !== 0) return statusDiff
      const modelDiff = left.model.localeCompare(right.model, 'en')
      return modelDiff !== 0
        ? modelDiff
        : left.group.localeCompare(right.group, 'zh-CN')
    })
    .slice(0, Math.max(0, limit))

  return {
    rows,
    activeModelCount: activeModels.length,
    healthyModelCount: activeModels.filter(
      (model) => model.status === 'healthy'
    ).length,
    sampleWindow: candidates[0]?.sample_window ?? null,
  }
}
