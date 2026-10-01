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
})
export const keysOptions = () =>
  resourceOptions('keys', (signal) =>
    api.GET('/api/token/', { signal }).then((result) => unwrap(result)),
  )
export const walletOptions = () =>
  resourceOptions('wallet', (signal) =>
    api.GET('/api/wallet', { signal }).then((result) => unwrap(result)),
  )
