import type { BlindBoxTierSetting } from '@/features/system-settings/types'

export const DEFAULT_BALANCE_BLIND_BOX_TIERS: BlindBoxTierSetting[] = [
  quotaTier('0.20-0.80 统一额度', 0.2, 0.8, 0.22),
  quotaTier('0.80-1.50 统一额度', 0.8, 1.5, 0.13),
  quotaTier('1.50-2.50 统一额度', 1.5, 2.5, 0.08),
  quotaTier('2.50-3.69 统一额度', 2.5, 3.69, 0.502427727273),
  quotaTier('4.50-12.00 统一额度', 4.5, 12, 0.045),
  quotaTier('12.00-30.00 统一额度', 12, 30, 0.008),
  quotaTier('30.00-100.00 统一额度', 30, 100, 0.0007),
  quotaTier('100.00-300.00 统一额度', 100, 300, 0.00015),
  quotaTier('300.00-500.00 统一额度', 300, 500, 0.00002),
  quotaTier('500.00 统一额度', 500, 500, 0.000002272727),
  propTier('再来一抽', 0.0127),
  propTier('15 分钟 0.1 倍率卡', 0.001),
]

export const BALANCE_BLIND_BOX_DEFAULTS = {
  'blind_box_setting.balance_blind_box_enabled': true,
  'blind_box_setting.balance_blind_box_price_usd': 2.5,
  'blind_box_setting.balance_blind_box_daily_purchase_limit': 10,
  'blind_box_setting.balance_blind_box_first_draw_guarantee_usd': 10,
  'blind_box_setting.balance_blind_box_small_pity_threshold': 10,
  'blind_box_setting.balance_blind_box_small_pity_guarantee_usd': 10,
  'blind_box_setting.balance_blind_box_pity_threshold': 50,
  'blind_box_setting.balance_blind_box_pity_guarantee_usd': 35,
  'blind_box_setting.balance_blind_box_tiers': DEFAULT_BALANCE_BLIND_BOX_TIERS,
}

function quotaTier(
  name: string,
  min_usd: number,
  max_usd: number,
  probability: number
): BlindBoxTierSetting {
  return {
    name,
    min_usd,
    max_usd,
    probability,
    reward_type: 'claude_quota',
    wallet_type: 'claude',
  }
}

function propTier(name: string, probability: number): BlindBoxTierSetting {
  return {
    name,
    min_usd: 0,
    max_usd: 0,
    probability,
    reward_type: 'prop',
  }
}
