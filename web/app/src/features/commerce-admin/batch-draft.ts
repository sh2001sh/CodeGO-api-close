import type { BoxBatch, BoxBatchReward } from '../blind-box/batch-contract'
import { boxInteger } from '../blind-box/batch-presentation'

export function newBoxBatch(): BoxBatch {
  return {
    id: 0,
    revision: 0,
    name: '',
    purpose: 'consumption',
    state: 'draft',
    price_micro: 0,
    base_credits_micro: 0,
    budget_micro: 0,
    required_budget_micro: 0,
    spent_budget_micro: 0,
    remaining_budget_micro: 0,
    total_count: 0,
    remaining_count: 0,
    entitled_count: 0,
    ancillary_cost_ppm: 30000,
    contribution_share_ppm: 100000,
    costs_confirmed: false,
    rewards: [newBatchReward()],
  }
}

export function newBatchReward(): BoxBatchReward {
  return {
    id: crypto.randomUUID(),
    title: '',
    kind: 'credits',
    amount_micro: 0,
    plan_id: 0,
    quantity: 1,
    remaining: 0,
    initial_probability: 0,
    remaining_probability: 0,
  }
}

export function validateBatchDraft(batch: BoxBatch): void {
  if (!batch.name.trim() || new TextEncoder().encode(batch.name.trim()).length > 200)
    throw new Error('请填写有效的批次名称')
  if (batch.rewards.length < 1 || batch.rewards.length > 100) throw new Error('请配置至少一项奖励')
  if (
    boxInteger(batch.contribution_share_ppm) < 1n ||
    boxInteger(batch.contribution_share_ppm) > 100000n
  )
    throw new Error('消费贡献预算比例不得超过 10%')
  if (boxInteger(batch.ancillary_cost_ppm) > 1000000n) throw new Error('附加成本比例不得超过 100%')
  if (
    batch.purpose === 'credits' &&
    (boxInteger(batch.price_micro) <= 0n ||
      boxInteger(batch.base_credits_micro) < boxInteger(batch.price_micro))
  )
    throw new Error('付费批次的确定消费额度不得低于扣款')
  if (
    batch.purpose === 'consumption' &&
    (boxInteger(batch.price_micro) !== 0n || boxInteger(batch.base_credits_micro) !== 0n)
  )
    throw new Error('免费消费回馈批次不得收款或配置付费基础额度')
  const ids = new Set<string>()
  let quantity = 0n
  for (const reward of batch.rewards) {
    if (!reward.title.trim() || new TextEncoder().encode(reward.title).length > 200)
      throw new Error('请填写有效的奖励名称')
    if (!reward.id || ids.has(reward.id)) throw new Error('奖励编号必须唯一')
    ids.add(reward.id)
    if (boxInteger(reward.quantity) <= 0n) throw new Error('奖励数量必须大于零')
    quantity += boxInteger(reward.quantity)
    if (quantity > 1000000n) throw new Error('批次总数量不得超过 1000000')
    if (reward.kind === 'credits' && boxInteger(reward.amount_micro) <= 0n)
      throw new Error('奖励额度必须大于零')
    if (reward.kind === 'subscription' && boxInteger(reward.plan_id) <= 0n)
      throw new Error('请选择有效的固定额度套餐')
  }
  if (boxInteger(batch.budget_micro) <= 0n) throw new Error('准备金预算必须大于零')
}
