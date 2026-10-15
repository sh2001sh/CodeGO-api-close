import type { PricingModel } from '../types'

function modelKey(name: string): string {
  return name.trim().toLowerCase()
}

/** Merge catalog metadata with the complete backend billing model set. */
export function mergePricingModels(
  catalogModels: PricingModel[],
  pricedModels: PricingModel[]
): PricingModel[] {
  const merged = new Map<string, PricingModel>()

  for (const model of catalogModels) {
    const key = modelKey(model.model_name)
    if (key) merged.set(key, model)
  }

  for (const model of pricedModels) {
    const key = modelKey(model.model_name)
    if (!key) continue
    const catalogModel = merged.get(key)
    merged.set(
      key,
      catalogModel
        ? {
            ...catalogModel,
            ...model,
            enable_groups: catalogModel.enable_groups,
          }
        : model
    )
  }

  return [...merged.values()]
}

/** Billing configuration alone must never make a model appear available. */
export function availablePricingModels(
  catalogModels: PricingModel[],
  pricedModels: PricingModel[],
  availableNames?: string[]
): PricingModel[] {
  const available = new Set(
    (
      availableNames ??
      catalogModels
        .filter((model) => model.enable_groups?.length > 0)
        .map((model) => model.model_name)
    )
      .map(modelKey)
      .filter(Boolean)
  )
  return mergePricingModels(catalogModels, pricedModels).filter((model) =>
    available.has(modelKey(model.model_name))
  )
}
