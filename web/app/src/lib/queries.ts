import { queryOptions } from '@tanstack/react-query'
import { api, unwrap } from './api'

export const resourceOptions = <T>(
  key: string,
  fetcher: (signal: AbortSignal) => Promise<T>,
  params: readonly (string | number | boolean)[] = [],
) =>
  queryOptions<NoInfer<T>>({
    queryKey: [key, ...params],
    queryFn: ({ signal }) => fetcher(signal),
  })

export const sessionOptions = () => ({
  ...resourceOptions('session', (signal) =>
    api.GET('/api/user/self', { signal }).then((result) => unwrap(result)),
  ),
  staleTime: 60_000,
  retry: false as const,
  // Anonymous public frames share a 401 result. Remounting a child must not
  // restart the session check and toggle its parent back into a loading frame.
  retryOnMount: false,
})
export const keysOptions = () =>
  resourceOptions('keys', (signal) =>
    api.GET('/api/token/', { signal }).then((result) => unwrap(result)),
  )
export const walletOptions = () =>
  // Route loaders and the top-bar balance share this request. A transient
  // observer unmount must not cancel the promise still awaited by a loader.
  queryOptions({
    queryKey: ['wallet'],
    queryFn: () => api.GET('/api/wallet').then((result) => unwrap(result)),
  })
