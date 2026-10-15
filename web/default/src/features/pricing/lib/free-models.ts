import type { PricingModel } from '../types'

function getEnabledGroupRatios(
  model: PricingModel,
  groupRatios?: Record<string, number>
): number[] {
  const sourceRatios = groupRatios ?? model.group_ratio ?? {}
  const groups = Array.isArray(model.enable_groups) ? model.enable_groups : []

  return groups
    .map((group) => sourceRatios[group])
    .filter((ratio): ratio is number => Number.isFinite(ratio))
}

export function isFreeModel(
  model: PricingModel,
  groupRatios?: Record<string, number>
): boolean {
  const ratios = getEnabledGroupRatios(model, groupRatios)
  return ratios.some((ratio) => ratio === 0)
}

export function getFreeEligibleGroups(
  model: PricingModel,
  groupRatios?: Record<string, number>
): string[] {
  const sourceRatios = groupRatios ?? model.group_ratio ?? {}
  const groups = Array.isArray(model.enable_groups) ? model.enable_groups : []

  return groups.filter((group) => sourceRatios[group] === 0)
}

export function countFreeModels(
  models: PricingModel[],
  groupRatios?: Record<string, number>
): number {
  return models.reduce(
    (count, model) => count + (isFreeModel(model, groupRatios) ? 1 : 0),
    0
  )
}
