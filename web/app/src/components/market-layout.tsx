import { useQuery } from '@tanstack/react-query'
import { APIError } from '../lib/api'
import { sessionOptions } from '../lib/queries'
import { Shell } from './shell'
import { SiteLayout } from './site-layout'
import { ErrorMessage, Loading } from './ui'

/** Browsing is public; an authenticated visitor keeps the console frame. */
export function MarketLayout() {
  const session = useQuery({ ...sessionOptions(), throwOnError: false })
  if (session.isPending) return <Loading />
  if (session.error && !(session.error instanceof APIError && session.error.status === 401))
    return <ErrorMessage error={session.error} />
  return session.data ? <Shell /> : <SiteLayout />
}
