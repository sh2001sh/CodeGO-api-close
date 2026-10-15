const ACTIVE_ROUTE_POOL_KEY = 'marketplace:active-route-pool'

export function readActiveRoutePoolID() {
  try {
    return window.localStorage.getItem(ACTIVE_ROUTE_POOL_KEY) ?? ''
  } catch {
    return ''
  }
}

export function persistActiveRoutePoolID(id: string) {
  try {
    if (id) window.localStorage.setItem(ACTIVE_ROUTE_POOL_KEY, id)
    else window.localStorage.removeItem(ACTIVE_ROUTE_POOL_KEY)
  } catch {
    // Storage can be unavailable in private browsing; page state still works.
  }
}
