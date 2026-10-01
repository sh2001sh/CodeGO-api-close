import type { Schema } from '../lib/types'
import { credits } from '../lib/format'

export function BoxGuarantees(props: {
  pool: Schema['MarketplacePool']
  state?: Schema['MarketplacePityState']
}) {
  const rules = props.pool.guarantees
  const standard = props.pool.standard_policy
  if (standard?.enabled)
    return (
      <details>
        <summary>保底规则与进度</summary>
        {BigInt(standard.first_purchase_minimum_micro) > 0n && (
          <p>
            首购保底：首次付费购买的盲盒开启时，至少获得{' '}
            {credits(standard.first_purchase_minimum_micro)}。
          </p>
        )}
        {standard.pity_after > 0 && (
          <p>
            保底进度 {props.state?.small_progress ?? 0} / {standard.pity_after}，保底至少{' '}
            {credits(standard.pity_minimum_micro)}；高价值奖励或订阅奖励重置进度。
          </p>
        )}
        {BigInt(standard.subscription_probability_ppb) > 0n && (
          <p>
            订阅奖励概率 {(Number(standard.subscription_probability_ppb) / 10_000_000).toFixed(5)}
            %，命中后重置保底进度。
          </p>
        )}
      </details>
    )
  if (!rules || (!rules.first?.length && !rules.small_after && !rules.big_after)) return null
  return (
    <details>
      <summary>保底规则与进度</summary>
      {rules.first?.length ? (
        <p>首抽保底：{rules.first.map((reward) => reward.title).join('、')}</p>
      ) : null}
      {rules.small_after ? (
        <p>
          小保底 {props.state?.small_progress ?? 0} / {rules.small_after}，获得至少{' '}
          {credits(rules.small_reset_micro)} 时重置。
        </p>
      ) : null}
      {rules.big_after ? (
        <p>
          大保底 {props.state?.big_progress ?? 0} / {rules.big_after}，获得至少{' '}
          {credits(rules.big_reset_micro)} 时重置。
        </p>
      ) : null}
    </details>
  )
}
