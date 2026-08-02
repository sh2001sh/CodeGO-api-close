import { ArrowRight, BookOpen, Info, TicketCheck } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatLuckyUsd } from '../lib'
import type { LuckyNumberRules } from '../types'
import { RewardRules, SettlementRules } from './rules-dialog-rewards'
import {
  MatchingRules,
  ParticipationRules,
  RulesSection,
  type RuleTier,
} from './rules-dialog-sections'

export function RulesDialogContent(props: {
  rules: LuckyNumberRules
  tiers: RuleTier[]
  drawTime: string
  timezone: string
}) {
  const { t } = useTranslation()
  const proTier = props.tiers.find((tier) => tier.name === t('Pro'))

  return (
    <div className='space-y-6 px-5 py-5 sm:px-7 sm:py-6'>
      <RulesCoreSummary />
      <RulesExample
        baseReward={props.rules.base_reward_2_usd}
        proMultiplier={proTier?.multiplier ?? props.rules.multiplier_pro}
      />

      <RulesSection
        icon={<BookOpen aria-hidden='true' />}
        title={t('1. Who can participate and when the draw happens')}
      >
        <ParticipationRules
          drawTime={props.drawTime}
          timezone={props.timezone}
        />
      </RulesSection>

      <RulesSection
        icon={<TicketCheck aria-hidden='true' />}
        title={t('2. How the membership card number matches the lucky number')}
      >
        <MatchingRules />
      </RulesSection>

      <RulesSection
        icon={<Info aria-hidden='true' />}
        title={t('3. How rewards are calculated after a match')}
      >
        <RewardRules rules={props.rules} tiers={props.tiers} />
      </RulesSection>

      <RulesSection
        icon={<TicketCheck aria-hidden='true' />}
        title={t('4. Credit, jackpot, and other limits')}
      >
        <SettlementRules rules={props.rules} />
      </RulesSection>
    </div>
  )
}

function RulesCoreSummary() {
  const { t } = useTranslation()

  return (
    <div className='border-primary/25 bg-primary/[0.045] rounded-xl border p-4 sm:p-5'>
      <div className='flex items-start gap-3'>
        <Info
          className='text-primary mt-0.5 size-5 shrink-0'
          aria-hidden='true'
        />
        <div className='min-w-0'>
          <h3 className='text-foreground text-sm font-semibold'>
            {t('The short version')}
          </h3>
          <p className='text-muted-foreground mt-1.5 text-sm leading-6'>
            {t(
              'The system compares the last four digits of your membership card number with the single four-digit lucky number announced for the whole site that day, one digit at a time from right to left. More consecutive matches earn a higher base reward, and the membership tier multiplier is then applied.'
            )}
          </p>
        </div>
      </div>
      <div className='border-border bg-background mt-4 flex flex-wrap items-center gap-2 rounded-lg border px-3 py-3 text-sm'>
        <span className='text-muted-foreground'>
          {t('Full membership card number')}
        </span>
        <code className='text-foreground font-mono font-semibold tabular-nums'>
          CG-7K2M9Q-7316
        </code>
        <ArrowRight className='text-primary size-4' aria-hidden='true' />
        <span className='text-muted-foreground'>
          {t('Take the last four digits')}
        </span>
        <code className='text-primary font-mono text-base font-bold tabular-nums'>
          7316
        </code>
      </div>
      <p className='text-muted-foreground mt-2 text-xs leading-5'>
        {t(
          'The prefix CG-7K2M9Q identifies the card and does not participate in matching; only the final four digits are used for the daily draw. Leading zeros are part of the suffix and must be kept.'
        )}
      </p>
    </div>
  )
}

function RulesExample(props: { baseReward: number; proMultiplier: number }) {
  const { t } = useTranslation()
  const totalReward = props.baseReward * props.proMultiplier

  return (
    <section className='border-border bg-card rounded-xl border p-4 sm:p-5'>
      <div className='flex items-center gap-2'>
        <TicketCheck className='text-primary size-4' aria-hidden='true' />
        <h3 className='text-foreground text-sm font-semibold'>
          {t('Full matching example')}
        </h3>
      </div>
      <div className='mt-4 grid gap-3 sm:grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] sm:items-center'>
        <ExampleNumber label={t('Membership card suffix')} value='7316' />
        <span className='text-primary inline-flex items-center justify-center gap-1 text-xs font-medium sm:flex-col'>
          <ArrowRight className='size-4 sm:rotate-90' aria-hidden='true' />
          {t('Compare from right to left')}
        </span>
        <ExampleNumber label={t("Today's lucky number")} value='5816' />
      </div>
      <div className='border-success/25 bg-success/8 mt-4 rounded-lg border px-3.5 py-3 text-sm leading-6'>
        <p className='text-foreground font-semibold'>
          {t('Result: the final 2 digits "16" match consecutively')}
        </p>
        <p className='text-muted-foreground mt-1'>
          {t(
            'The rightmost 6 equals 6 and the second digit from the right, 1, equals 1. The third digit, 3, does not equal 8, so the comparison stops there: this is not a 3-digit match, and other matching digits on the left do not count.'
          )}
        </p>
        <p className='text-muted-foreground mt-1'>
          {t(
            'For a Pro membership card, the reward is {{base}} × {{multiplier}} = {{total}}.',
            {
              base: formatLuckyUsd(props.baseReward),
              multiplier: Number(props.proMultiplier).toFixed(1),
              total: formatLuckyUsd(totalReward),
            }
          )}
        </p>
      </div>
    </section>
  )
}

function ExampleNumber(props: { label: string; value: string }) {
  return (
    <div className='border-border bg-background rounded-lg border px-3.5 py-3'>
      <div className='text-muted-foreground text-xs'>{props.label}</div>
      <code className='text-foreground mt-1.5 block font-mono text-xl font-bold tracking-[0.12em] tabular-nums'>
        {props.value}
      </code>
    </div>
  )
}
