import type { ModelVerificationResult } from '../types'

/** Returns the latest models that need a targeted connectivity retry. */
export function failedConnectivityModels(results: ModelVerificationResult[]) {
  return results.filter(
    (result) => result.status !== 'passed' || result.listed !== true
  )
}
