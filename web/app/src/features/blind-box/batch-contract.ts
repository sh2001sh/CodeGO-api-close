import type { Schema } from '../../lib/types'

export type BoxInteger = Schema['BlindBoxBatch']['id']
export type BoxBatchReward = Schema['BlindBoxBatchReward']
export type BoxBatch = Schema['BlindBoxBatch']
export type BoxEntitlement = Schema['BlindBoxBatchEntitlement']
export type BoxBatchOverview = Schema['BlindBoxBatchOverview']
export type BoxBatchDraw = Schema['BlindBoxBatchDrawResult']
export type BoxBatchStats = Schema['BlindBoxBatchStatistics']
export type LegacyBoxInventory = Schema['BlindBoxInventoryGroup']

/** The display only reads these fields; API contracts retain the complete frozen plan. */
export type PlanSnapshot = {
  name?: string
  credits?: BoxInteger
  period_seconds?: BoxInteger
  policy_version?: string
  model_limits?: Record<string, BoxInteger> | null
}
