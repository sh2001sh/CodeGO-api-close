import type { ChannelModelPrice } from '../types'

export interface ChannelModelSyncDiff {
  upstreamModels: string[]
  addedModels: string[]
  retainedModels: string[]
  removedModels: string[]
}

export function normalizeChannelModels(models: readonly string[]): string[] {
  const seen = new Set<string>()
  return models.flatMap((model) => {
    const value = model.trim()
    const key = value.toLowerCase()
    if (!value || seen.has(key)) return []
    seen.add(key)
    return [value]
  })
}

export function buildChannelModelSyncDiff(
  currentModels: readonly string[],
  upstreamModels: readonly string[]
): ChannelModelSyncDiff {
  const current = normalizeChannelModels(currentModels)
  const upstream = normalizeChannelModels(upstreamModels)
  const currentKeys = new Set(current.map(modelKey))
  const upstreamKeys = new Set(upstream.map(modelKey))
  return {
    upstreamModels: upstream,
    addedModels: upstream.filter((model) => !currentKeys.has(modelKey(model))),
    retainedModels: upstream.filter((model) =>
      currentKeys.has(modelKey(model))
    ),
    removedModels: current.filter(
      (model) => !upstreamKeys.has(modelKey(model))
    ),
  }
}

export function mergeChannelModels(
  upstreamModels: readonly string[],
  removedModels: readonly string[]
): string[] {
  return normalizeChannelModels([...upstreamModels, ...removedModels])
}

export function reconcileChannelModelPrices(
  prices: Record<string, ChannelModelPrice>,
  models: readonly string[]
): Record<string, ChannelModelPrice> {
  const pricesByKey = new Map(
    Object.entries(prices).map(([model, price]) => [modelKey(model), price])
  )
  return Object.fromEntries(
    normalizeChannelModels(models).flatMap((model) => {
      const price = pricesByKey.get(modelKey(model))
      return price ? [[model, price]] : []
    })
  )
}

export function reconcileAutoProbeModel(
  currentModel: string,
  models: readonly string[],
  enabled: boolean
): string {
  const normalized = normalizeChannelModels(models)
  const currentKey = modelKey(currentModel)
  const retained = normalized.find((model) => modelKey(model) === currentKey)
  if (retained) return retained
  return enabled ? (normalized[0] ?? '') : ''
}

export function modelKey(model: string): string {
  return model.trim().toLowerCase()
}
