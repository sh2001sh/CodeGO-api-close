import type { ModelVerificationResult } from '../types'

const GPT56_MODELS = new Set(['gpt-5.6-sol', 'gpt-5.6-terra', 'gpt-5.6-luna'])

/** Returns whether the declared model set requires GPT-5.6 mapping detection. */
export function hasGPT56Model(models: string[]) {
  return models.some((model) => GPT56_MODELS.has(model.trim().toLowerCase()))
}

/** Returns the latest models that need a targeted connectivity retry. */
export function failedConnectivityModels(results: ModelVerificationResult[]) {
  return results.filter(
    (result) => result.status !== 'passed' || result.listed !== true
  )
}
