import { ROLE } from '@/lib/roles'
import type { NavGroup } from '../types'

export function filterNavGroupsByRole(
  navGroups: NavGroup[],
  userRole?: number | null
) {
  const isAdmin = (userRole ?? 0) >= ROLE.ADMIN
  return navGroups.filter((group) => group.id !== 'admin' || isAdmin)
}
