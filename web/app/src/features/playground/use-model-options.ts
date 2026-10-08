import { useQuery } from '@tanstack/react-query'
import { fetchGatewayModels } from './gateway-models'
import type { APIKey } from '../../lib/types'

/**
 * The gateway discovers routable models for the selected group and enforces
 * the key allowlist. Its query cache contains model names, never the secret.
 */
export function useModelOptions(selectedKey: APIKey | null, secret: string | null, group?: string) {
  const allowed = selectedKey?.allowed_models ?? null
  const query = useQuery({
    queryKey: ['playground-models', selectedKey?.id, Boolean(secret), group ?? ''],
    queryFn: ({ signal }) => fetchGatewayModels(secret ?? '', signal, group),
    enabled: Boolean(secret),
    staleTime: 60_000,
    retry: false,
  })
  const models = query.data ?? []
  return {
    models: allowed?.length ? models.filter((model) => allowed.includes(model)) : models,
    isPending: Boolean(secret) && query.isPending,
    error: query.error as Error | null,
  }
}
