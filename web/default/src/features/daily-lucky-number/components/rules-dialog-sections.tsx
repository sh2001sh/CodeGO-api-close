import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

export type RuleTier = {
  name: string
  label: string
  multiplier: number
  tone: string
}

export function ParticipationRules(props: {
  drawTime: string
  timezone: string
}) {
  const { t } = useTranslation()

  return (
    <div className='space-y-3'>
      <div className='grid gap-3 md:grid-cols-2'>
        <RuleFact title={t('Draw time')}>
          {t(
            'One draw is held every day at {{time}} ({{timezone}}). The system publishes one four-digit lucky number for the whole site, from 0000 to 9999.',
            { time: props.drawTime, timezone: props.timezone }
          )}
        </RuleFact>
        <RuleFact title={t('Eligibility')}>
          有效月卡在开奖快照时自动参与；每开启一个盲盒，还会获得一个仅限所属开奖周期参与的四位幸运号。
        </RuleFact>
        <RuleFact title={t('Purchases and upgrades')}>
          {t(
            'A membership card purchased, renewed, or upgraded before the draw uses the status and tier captured at that draw. Changes completed after the draw take effect from the next draw.'
          )}
        </RuleFact>
        <RuleFact title={t('Multiple cards')}>
          {t(
            'Each active membership card owned by the same user is matched independently. Rewards are credited to the specific card that produced the match.'
          )}
        </RuleFact>
        <RuleFact title='盲盒幸运号'>
          盲盒号码与月卡号码独立参与。每个盲盒对应一个号码，20:00
          前开出参与当日开奖，20:00 起开出参与次日开奖，并按基础 1.0 倍率结算。
        </RuleFact>
      </div>
      <p className='text-muted-foreground text-xs leading-5'>
        无需签到或手动报名。月卡号码在月卡有效期内持续参与；盲盒号码只参与所属周期对应的一次开奖。
      </p>
    </div>
  )
}

export function MatchingRules() {
  const { t } = useTranslation()

  return (
    <div className='space-y-4'>
      <div className='grid gap-2 sm:grid-cols-4'>
        <RuleStep
          number='1'
          title={t('Take the suffix')}
          detail={t('月卡取完整卡号末四位；盲盒直接使用开盒时生成的四位号码。')}
        />
        <RuleStep
          number='2'
          title={t('Read the winning number')}
          detail={t(
            'Compare it with the single four-digit lucky number announced for the whole site that day.'
          )}
        />
        <RuleStep
          number='3'
          title={t('Compare from right to left')}
          detail={t(
            'Only consecutive matches starting at the rightmost digit count.'
          )}
        />
        <RuleStep
          number='4'
          title={t('Use the highest match only')}
          detail={t('A 2-digit match does not also add the 1-digit reward.')}
        />
      </div>
      <div className='border-border overflow-x-auto rounded-lg border'>
        <div className='bg-muted/45 text-muted-foreground grid min-w-[420px] grid-cols-[minmax(0,1fr)_minmax(0,1fr)_minmax(0,1.3fr)] text-left text-xs'>
          <div className='px-3 py-2.5 font-medium'>
            {t('Membership card suffix')}
          </div>
          <div className='px-3 py-2.5 font-medium'>
            {t("Today's lucky number")}
          </div>
          <div className='px-3 py-2.5 font-medium'>{t('Match result')}</div>
        </div>
        <div className='divide-border min-w-[420px] divide-y text-sm'>
          <MatchExample
            card='7316'
            winning='5816'
            result={t('2 digits: the consecutive suffix "16" matches')}
          />
          <MatchExample
            card='7316'
            winning='7006'
            result={t('1 digit: only the rightmost "6" matches')}
          />
          <MatchExample
            card='7316'
            winning='7316'
            result={t('4 digits: all four digits match')}
          />
          <MatchExample
            card='7316'
            winning='1234'
            result={t('0 digits: the rightmost digit differs')}
          />
        </div>
      </div>
      <p className='text-muted-foreground text-xs leading-5'>
        {t(
          'Important: do not count every digit that appears somewhere in both strings. Only the uninterrupted suffix match from the rightmost digit is counted.'
        )}
      </p>
    </div>
  )
}

export function RulesSection(props: {
  title: string
  icon: ReactNode
  children: ReactNode
}) {
  return (
    <section className='space-y-3'>
      <div className='flex items-center gap-2'>
        <span className='text-primary [&>svg]:size-4'>{props.icon}</span>
        <h2 className='text-foreground text-sm font-semibold'>{props.title}</h2>
      </div>
      {props.children}
    </section>
  )
}

export function RuleFact(props: {
  icon?: ReactNode
  title: string
  children: ReactNode
}) {
  return (
    <div className='border-border bg-background rounded-lg border px-3.5 py-3'>
      <div className='text-foreground flex items-center gap-2 text-sm font-medium'>
        {props.icon ? (
          <span className='text-primary [&>svg]:size-3.5'>{props.icon}</span>
        ) : null}
        {props.title}
      </div>
      <p className='text-muted-foreground mt-1.5 text-xs leading-5'>
        {props.children}
      </p>
    </div>
  )
}

function RuleStep(props: { number: string; title: string; detail: string }) {
  return (
    <div className='border-border bg-background flex items-start gap-2.5 rounded-lg border px-3 py-3'>
      <span className='bg-primary/10 text-primary flex size-6 shrink-0 items-center justify-center rounded-full text-xs font-semibold'>
        {props.number}
      </span>
      <div className='min-w-0'>
        <div className='text-foreground text-xs font-semibold'>
          {props.title}
        </div>
        <p className='text-muted-foreground mt-1 text-[11px] leading-4'>
          {props.detail}
        </p>
      </div>
    </div>
  )
}

function MatchExample(props: {
  card: string
  winning: string
  result: string
}) {
  return (
    <div className='grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_minmax(0,1.3fr)] items-center'>
      <code className='text-foreground px-3 py-2.5 font-mono text-sm tabular-nums'>
        {props.card}
      </code>
      <code className='text-foreground px-3 py-2.5 font-mono text-sm tabular-nums'>
        {props.winning}
      </code>
      <span className='text-muted-foreground px-3 py-2.5 text-xs'>
        {props.result}
      </span>
    </div>
  )
}
