import { Check, Coins, ShieldCheck, Trophy } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { formatLuckyUsd } from '../lib'
import type { LuckyNumberRules } from '../types'
import { RuleFact, type RuleTier } from './rules-dialog-sections'

type RewardKey =
  | 'none'
  | 'base_reward_1_usd'
  | 'base_reward_2_usd'
  | 'base_reward_3_usd'
  | 'base_reward_4_usd'

export function RewardRules(props: {
  rules: LuckyNumberRules
  tiers: RuleTier[]
}) {
  const { t } = useTranslation()
  const rewardRows: Array<{
    label: string
    match: string
    key: RewardKey
    description: string
  }> = [
    {
      label: t('No match'),
      match: t('0 digits'),
      key: 'none',
      description: t('The first digit from the right is already different.'),
    },
    {
      label: t('Base reward'),
      match: t('1 digit'),
      key: 'base_reward_1_usd',
      description: t('Only the rightmost 1 digit matches consecutively.'),
    },
    {
      label: t('Advanced reward'),
      match: t('2 digits'),
      key: 'base_reward_2_usd',
      description: t('The rightmost 2 digits match consecutively.'),
    },
    {
      label: t('High-tier reward'),
      match: t('3 digits'),
      key: 'base_reward_3_usd',
      description: t('The rightmost 3 digits match consecutively.'),
    },
    {
      label: t('Full-match reward'),
      match: t('4 digits'),
      key: 'base_reward_4_usd',
      description: t('The base reward plus an equal share of the jackpot.'),
    },
  ]

  return (
    <div className='space-y-4'>
      <div className='overflow-x-auto rounded-lg border'>
        <table className='w-full min-w-[560px] text-sm'>
          <thead className='bg-muted/45 text-muted-foreground text-left text-xs'>
            <tr>
              <th className='px-3 py-2.5 font-medium'>
                {t('Membership tier')}
              </th>
              <th className='px-3 py-2.5 font-medium'>{t('Multiplier')}</th>
              <th className='px-3 py-2.5 font-medium'>
                {t('Example for a 2-digit match')}
              </th>
              <th className='px-3 py-2.5 text-right font-medium'>
                {t('Calculation')}
              </th>
            </tr>
          </thead>
          <tbody className='divide-border divide-y'>
            {props.tiers.map((tier) => (
              <tr key={tier.name}>
                <td className={cn('px-3 py-3 font-semibold', tier.tone)}>
                  {tier.name}{' '}
                  <span className='text-muted-foreground font-normal'>
                    · {tier.label}
                  </span>
                </td>
                <td className='text-foreground px-3 py-3 font-mono font-semibold tabular-nums'>
                  {Number(tier.multiplier).toFixed(1)}x
                </td>
                <td className='text-foreground px-3 py-3 font-mono font-semibold tabular-nums'>
                  {formatLuckyUsd(
                    props.rules.base_reward_2_usd * tier.multiplier
                  )}
                </td>
                <td className='text-muted-foreground px-3 py-3 text-right text-xs'>
                  {t('{{base}} × {{multiplier}}', {
                    base: formatLuckyUsd(props.rules.base_reward_2_usd),
                    multiplier: Number(tier.multiplier).toFixed(1),
                  })}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className='grid grid-cols-2 gap-2 sm:grid-cols-5'>
        {rewardRows.map((row) => {
          const amount = row.key === 'none' ? 0 : props.rules[row.key]
          return (
            <div
              key={row.key}
              className='border-border bg-background min-w-0 rounded-lg border px-3 py-3'
            >
              <div className='text-muted-foreground text-xs'>{row.label}</div>
              <div className='text-foreground mt-1 font-mono text-lg font-semibold tabular-nums'>
                {row.key === 'none'
                  ? '$0'
                  : `${formatLuckyUsd(amount)}${row.key === 'base_reward_4_usd' ? ' +' : ''}`}
              </div>
              <div className='text-primary mt-1 text-xs font-medium'>
                {row.match}
              </div>
              <p className='text-muted-foreground mt-1.5 text-[11px] leading-4'>
                {row.description}
              </p>
            </div>
          )
        })}
      </div>
      <div className='border-primary/20 bg-primary/[0.045] flex items-start gap-2.5 rounded-lg border px-3.5 py-3 text-xs leading-5'>
        <Coins
          className='text-primary mt-0.5 size-4 shrink-0'
          aria-hidden='true'
        />
        <p className='text-muted-foreground'>
          {t(
            'The order is: determine the number of consecutive matching digits, then apply the membership tier multiplier. Only the highest match is paid once; the jackpot share for a full four-digit match is not multiplied.'
          )}
        </p>
      </div>
    </div>
  )
}

export function SettlementRules(props: { rules: LuckyNumberRules }) {
  const { t } = useTranslation()

  return (
    <div className='grid gap-3 md:grid-cols-2'>
      <RuleFact
        icon={<Check aria-hidden='true' />}
        title={t('Reward destination')}
      >
        {t(
          'After settlement, the reward is added automatically to the matched membership card balance, not the ordinary wallet. If the page shows "crediting", the original draw will continue to settle and will not generate a new number.'
        )}
      </RuleFact>
      <RuleFact icon={<Trophy aria-hidden='true' />} title={t('Jackpot')}>
        {t(
          'The jackpot starts at {{initial}}. If no card matches all four digits, it increases by {{increment}}, up to {{cap}}; when there is a full match, all full-match cards split it equally and the next draw resets it.',
          {
            initial: formatLuckyUsd(props.rules.jackpot_initial_usd),
            increment: formatLuckyUsd(props.rules.jackpot_increment_usd),
            cap: formatLuckyUsd(props.rules.jackpot_cap_usd),
          }
        )}
      </RuleFact>
      <RuleFact
        icon={<ShieldCheck aria-hidden='true' />}
        title={t('Activity limits')}
      >
        {t(
          'Rewards cannot be withdrawn, traded, or transferred, and cannot purchase extra draw entries. The card number and winning records remain available, but an expired card does not join new draws.'
        )}
      </RuleFact>
      <RuleFact
        icon={<ShieldCheck aria-hidden='true' />}
        title={t('Number and fairness')}
      >
        {t(
          'The whole site receives one four-digit lucky number per day. Different cards may hold the same suffix; leading zeros are preserved, and the published draw record is the source of truth.'
        )}
      </RuleFact>
    </div>
  )
}
