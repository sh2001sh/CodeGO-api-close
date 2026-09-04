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
    merged.set(key, catalogModel ? { ...catalogModel, ...model } : model)
  }

  return [...merged.values()]
}
