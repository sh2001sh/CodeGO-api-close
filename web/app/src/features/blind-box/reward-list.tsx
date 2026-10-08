import type { Schema } from '../../lib/types'
import { credits } from '../../lib/format'
import { useTranslation } from '../../lib/i18n'
import { weightPercentage } from './presentation'

export function BoxRewardList({
  rewards,
  total,
}: {
  rewards: readonly Schema['MarketplaceReward'][]
  total: bigint | undefined
}) {
  const { t } = useTranslation()
  return (
    <ul className="box-reward-list">
      {rewards.map((reward, index) => (
        <li key={`${reward.kind}-${index}`}>
          <span className="box-reward-name">
            <strong>{reward.title}</strong>
            {reward.kind === 'credits' && (
              <span className="muted">
                {BigInt(reward.minimum_micro ?? 0) > 0n
                  ? `${credits(reward.minimum_micro)} – ${credits(reward.maximum_micro)}`
                  : credits(reward.amount_micro)}
              </span>
            )}
          </span>
          <span className="box-probability" dir="ltr">
            {total === undefined
              ? t('概率暂不可用')
              : (weightPercentage(reward.weight, total) ?? t('概率暂不可用'))}
          </span>
        </li>
      ))}
    </ul>
  )
}
