/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useTranslation } from 'react-i18next'
import {
  formatTierAmount,
  groupTiersByRewardType,
} from '../lib/blind-box-rarity'
import type { BlindBoxSelfData, BlindBoxTier } from '../types'

export function BlindBoxPoolShowcase(props: {
  data: BlindBoxSelfData | null
  tiers?: BlindBoxTier[]
  title?: string
  description?: string
  hideSubscription?: boolean
}) {
  const { t } = useTranslation()
  const tiers = props.tiers || props.data?.tiers || []
  const grouped = groupTiersByRewardType(tiers)
  const hiddenProbability = props.data?.subscription_prize_probability || 0
  const maxProbability = Math.max(
    hiddenProbability,
    ...tiers.map((tier) => tier.probability),
    0.0001
  )
  const groups = [
    { title: t('通用额度'), tiers: grouped.credit },
    { title: t('道具'), tiers: grouped.props },
  ]

  return (
    <section className='blind-box-pool'>
      <h2>{props.title || t('奖池一览')}</h2>
      {!props.hideSubscription ? (
        <PoolGroup title={t('隐藏款')}>
          <PoolRow
            label={props.data?.subscription_plan_title || t('Lite 月卡')}
            probability={hiddenProbability}
            maxProbability={maxProbability}
          />
        </PoolGroup>
      ) : null}
      {groups.map((group) =>
        group.tiers.length > 0 ? (
          <PoolGroup key={group.title} title={group.title}>
            {group.tiers.map((tier) => (
              <PoolRow
                key={tier.name}
                label={formatTierAmount(tier)}
                probability={tier.probability}
                maxProbability={maxProbability}
              />
            ))}
          </PoolGroup>
        ) : null
      )}
      {tiers.length === 0 && props.hideSubscription ? (
        <p className='blind-box-empty'>{t('暂无数据')}</p>
      ) : null}
    </section>
  )
}

function PoolGroup(props: { title: string; children: React.ReactNode }) {
  return (
    <section className='blind-box-pool-group'>
      <h3>{props.title}</h3>
      <div>{props.children}</div>
    </section>
  )
}

function PoolRow(props: {
  label: string
  probability: number
  maxProbability: number
}) {
  const barWidth = Math.max(
    0,
    Math.min(100, (props.probability / props.maxProbability) * 100)
  )
  return (
    <div className='blind-box-pool-row'>
      <span>{props.label}</span>
      <span className='blind-box-probability-bar' aria-hidden>
        <span style={{ width: `${barWidth}%` }} />
      </span>
      <span className='blind-box-probability tabular-nums'>
        {formatProbability(props.probability)}
      </span>
    </div>
  )
}

function formatProbability(probability: number) {
  if (probability <= 0) return '0%'
  const percentage = probability * 100
  if (percentage < 0.001) return '<0.001%'
  return `${percentage.toFixed(3).replace(/0+$/, '').replace(/\.$/, '')}%`
}
