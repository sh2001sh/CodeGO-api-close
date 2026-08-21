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
import { api } from '@/lib/api'
import type { SidebarGroupStatusResponse } from './types'

export async function getSidebarGroupStatus(): Promise<SidebarGroupStatusResponse> {
  const res = await api.get<SidebarGroupStatusResponse>(
    '/api/user/self/group-status',
    // The query owns its error state and retries transient failures. Avoid a
    // global toast for each attempt while the control service is warming up.
    { skipErrorHandler: true } as Record<string, unknown>
  )
  return res.data
}
