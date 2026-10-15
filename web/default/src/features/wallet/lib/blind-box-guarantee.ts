import type { BlindBoxRecord } from '../types'

/** Returns the user-facing guarantee type encoded in a sealed reward tier. */
export function blindBoxGuaranteeLabel(
  record: Pick<BlindBoxRecord, 'is_pity' | 'reward_tier'>
) {
  if (!record.is_pity) return null
  if (record.reward_tier.startsWith('首购')) return '首抽保底'
  if (record.reward_tier.startsWith('小保底')) return '小保底'
  if (record.reward_tier.startsWith('大保底')) return '大保底'
  return '保底'
}
