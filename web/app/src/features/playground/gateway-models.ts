// Fetches the model list from the gateway's /v1/models using a specific key's secret.
// Not part of the control-API openapi surface (it's served by the gateway), so this
// uses a plain fetch rather than the generated client.
import { GatewayError } from './stream'

export async function fetchGatewayModels(
  apiKey: string,
  signal?: AbortSignal,
  group?: string,
): Promise<string[]> {
  const response = await fetch('/v1/models', {
    headers: { Authorization: `Bearer ${apiKey}`, ...(group ? { 'X-CodeGo-Group': group } : {}) },
    signal,
  })
  if (!response.ok) {
    let message = `HTTP ${response.status}`
    try {
      const body = (await response.json()) as { error?: { message?: string } }
      message = body.error?.message ?? message
    } catch {
      // ignore
    }
    throw new GatewayError(message, response.status)
  }
  const body = (await response.json()) as { data?: { id: string }[] }
  return (body.data ?? []).map((model) => model.id)
}
