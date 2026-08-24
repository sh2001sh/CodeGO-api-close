import type { MarketplaceAutoRoutePoolItem } from '../types'

/** Returns selected route-pool group IDs in their persisted priority order. */
export function selectedAutoRoutePoolGroupIDs(
  items: MarketplaceAutoRoutePoolItem[]
): string[] {
  return items
    .filter((item) => item.selected)
    .sort((left, right) => left.priority - right.priority)
    .map((item) => item.group_id)
}

/** Appends one group without changing existing priority or creating duplicates. */
export function appendAutoRoutePoolGroup(
  items: MarketplaceAutoRoutePoolItem[],
  groupID: string
): string[] {
  const selectedGroupIDs = selectedAutoRoutePoolGroupIDs(items)
  if (selectedGroupIDs.includes(groupID)) return selectedGroupIDs
  return [...selectedGroupIDs, groupID]
}
