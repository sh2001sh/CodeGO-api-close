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
import { useMemo } from 'react'
import { parseHeaderNavModulesFromStatus } from '@/lib/nav-modules'
import { useStatus } from '@/hooks/use-status'
import { defaultTopNavLinks } from '@/components/layout/config/top-nav.config'
import type { TopNavLink } from '@/components/layout/types'

export type { TopNavLink } from '@/components/layout/types'

export function useTopNavLinks(): TopNavLink[] {
  const { status } = useStatus()

  return useMemo(() => {
    const modules = parseHeaderNavModulesFromStatus(
      status as Record<string, unknown> | null
    )

    return defaultTopNavLinks.flatMap((link): TopNavLink[] => {
      if (link.href === '/' && modules.home === false) return []
      if (link.href === '/dashboard/overview' && modules.console === false)
        return []
      if (link.href === '/docs' && modules.docs === false) return []
      if (link.href === '/about' && modules.about === false) return []
      if (link.href === '/pricing') {
        if (!modules.pricing.enabled) return []
        return [{ ...link, requiresAuth: modules.pricing.requireAuth }]
      }
      if (link.href === '/rankings') {
        if (!modules.rankings.enabled) return []
        return [{ ...link, requiresAuth: modules.rankings.requireAuth }]
      }
      return [link]
    })
  }, [status])
}
