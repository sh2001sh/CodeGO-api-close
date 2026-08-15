import type { MarketplaceGroup } from '@/features/marketplace/types'
import type { PricingModel, PricingVendor } from '../types'

function stableModelID(name: string): number {
  let hash = 0
  for (let index = 0; index < name.length; index += 1) {
    hash = (hash * 31 + name.charCodeAt(index)) | 0
  }
  return -(Math.abs(hash) + 1)
}

function availability(group: MarketplaceGroup): number {
  return group.observing
    ? group.success_rate
    : group.wilson_success_rate || group.success_rate
}

export function buildThirdPartyPricingModels(
  groups: MarketplaceGroup[],
  officialModels: PricingModel[]
): PricingModel[] {
  const officialByName = new Map(
    officialModels.map((model) => [model.model_name, model])
  )
  const groupsByModel = new Map<string, MarketplaceGroup[]>()

  for (const group of groups) {
    for (const modelName of group.models) {
      const current = groupsByModel.get(modelName) ?? []
      current.push(group)
      groupsByModel.set(modelName, current)
    }
  }

  return Array.from(groupsByModel.entries()).map(([modelName, modelGroups]) => {
    const official = officialByName.get(modelName)
    const displayGroups = modelGroups.map(
      (group) => group.system_display_name
    )
    const groupRatio = Object.fromEntries(
      modelGroups.map((group) => [group.system_display_name, group.multiplier])
    )
    const sourceLabels = Array.from(
      new Set(modelGroups.map((group) => group.source_label).filter(Boolean))
    )
    const minimumMultiplier = Math.min(
      ...modelGroups.map((group) => group.multiplier)
    )
    const bestAvailability = Math.max(...modelGroups.map(availability))
    const summary = `${modelGroups.length} 个第三方分组可用 · 最低 ${minimumMultiplier}x · 最佳可用率 ${bestAvailability.toFixed(1)}%`

    return {
      ...(official ?? {
        id: stableModelID(modelName),
        model_name: modelName,
        quota_type: 0,
        model_ratio: 0,
        completion_ratio: 1,
        vendor_name: '第三方渠道',
        supported_endpoint_types: [],
      }),
      id: official?.id ?? stableModelID(modelName),
      description: summary,
      enable_groups: displayGroups,
      group_ratio: groupRatio,
      tags: ['第三方', ...sourceLabels].join(','),
      pricing_available: Boolean(official),
    }
  })
}

export function buildThirdPartyVendors(
  models: PricingModel[],
  officialVendors: PricingVendor[]
): PricingVendor[] {
  const names = new Set(models.map((model) => model.vendor_name).filter(Boolean))
  const vendors = officialVendors.filter((vendor) => names.has(vendor.name))
  if (names.has('第三方渠道')) {
    vendors.push({ id: -1, name: '第三方渠道' })
  }
  return vendors
}
