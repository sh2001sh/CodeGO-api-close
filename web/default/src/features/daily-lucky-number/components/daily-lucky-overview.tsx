import { Clock3, Info, RefreshCw, Trophy } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { LuckyDigits } from './lucky-digits'
import { formatCountdown, formatLuckyDate, getDrawTimeLabel } from '../lib'
import type { LuckyDrawView, LuckyNumberSelfPayload } from '../types'

export function DailyLuckyOverview(props: {
  payload: LuckyNumberSelfPayload
  countdownSeconds: number
  onRefresh: () => void
  refreshing?: boolean
}) {
  const { t, i18n } = useTranslation()
  const today = props.payload.today_draw
  const previous = props.payload.previous_draw
  const drawTime = getDrawTimeLabel(
    props.payload.draw_hour,
    props.payload.draw_minute,
    props.payload.timezone,
    t
  )

  return (
    <section className='app-page-shell overflow-hidden'>
      <div className='border-border/70 flex flex-wrap items-start justify-between gap-3 border-b px-4 py-4 sm:px-5'>
        <div className='flex min-w-0 items-start gap-3'>
          <div className='bg-primary/10 text-primary flex size-10 shrink-0 items-center justify-center rounded-xl'>
            <Trophy className='size-5' aria-hidden='true' />
          </div>
          <div className='min-w-0'>
            <h2 className='text-foreground text-base font-semibold tracking-tight sm:text-lg'>
              {t('Daily Lucky Number')}
            </h2>
            <p className='text-muted-foreground mt-1 text-sm leading-6'>
              {t('Your eligible monthly subscriptions enter automatically. Rewards go straight to the matched subscription quota.')}
            </p>
          </div>
        </div>
        <div className='flex items-center gap-2'>
          <div className='bg-muted/50 flex items-center gap-2 rounded-lg border px-3 py-2'>
            <Clock3 className='text-primary size-4' aria-hidden='true' />
            <div>
              <div className='text-muted-foreground text-[11px]'>
                {t('Next draw')}
              </div>
              <div className='font-mono text-sm font-semibold tabular-nums'>
                {formatCountdown(props.countdownSeconds)}
              </div>
            </div>
          </div>
          <Button
            variant='outline'
            size='icon'
            onClick={props.onRefresh}
            disabled={props.refreshing}
            aria-label={t('Refresh')}
          >
            <RefreshCw className={cn(props.refreshing && 'animate-spin')} aria-hidden='true' />
          </Button>
        </div>
      </div>

      {!props.payload.enabled ? (
        <Alert className='m-4 sm:m-5'>
          <Info aria-hidden='true' />
          <AlertDescription>
            {t('The daily lucky number activity is currently unavailable. Existing subscription quota and history are unaffected.')}
          </AlertDescription>
        </Alert>
      ) : null}

      <div className='grid lg:grid-cols-[minmax(0,1.35fr)_minmax(250px,0.65fr)]'>
        <div className='space-y-5 p-4 sm:p-5'>
          <DrawSnapshot
            label={t('Today\'s draw')}
            draw={today}
            timezone={props.payload.timezone}
            locale={i18n.language}
            emptyText={t('The winning number will appear after today\'s draw.')}
          />
          <DrawSnapshot
            label={t('Previous draw')}
            draw={previous}
            timezone={props.payload.timezone}
            locale={i18n.language}
            emptyText={t('No completed draw yet.')}
            muted
          />
        </div>

        <aside className='border-border/70 bg-muted/20 border-t p-4 sm:p-5 lg:border-t-0 lg:border-l'>
          <div className='flex items-center gap-2'>
            <span className='bg-warning/15 text-warning flex size-8 items-center justify-center rounded-lg'>
              <Trophy className='size-4' aria-hidden='true' />
            </span>
            <div>
              <div className='text-foreground text-sm font-semibold'>
                {t('Accumulated jackpot')}
              </div>
              <div className='text-muted-foreground text-xs'>{drawTime}</div>
            </div>
          </div>
          <div className='mt-6 font-mono text-4xl font-semibold tracking-tight tabular-nums'>
            ${Number(props.payload.jackpot_usd || 0).toFixed(2)}
          </div>
          <div className='text-muted-foreground mt-2 text-sm'>
            {t('Cap {{amount}} · full matches split the jackpot', {
              amount: `$${Number(props.payload.jackpot_cap_usd || 0).toFixed(2)}`,
            })}
          </div>
          <div className='border-border mt-6 border-t pt-4 text-xs leading-5'>
            <div className='text-foreground font-medium'>
              {t('How it works')}
            </div>
            <p className='text-muted-foreground mt-1.5'>
              {t('Match consecutive digits from the right. Only the highest matching tier is paid once per draw.')}
            </p>
          </div>
        </aside>
      </div>
    </section>
  )
}

function DrawSnapshot(props: {
  label: string
  draw?: LuckyDrawView
  timezone: string
  locale: string
  emptyText: string
  muted?: boolean
}) {
  const { t } = useTranslation()
  return (
    <div className={cn('space-y-2', props.muted && 'opacity-85')}>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <h3 className='text-foreground text-sm font-semibold'>{props.label}</h3>
        {props.draw ? (
          <span className='text-muted-foreground text-xs'>
            {formatLuckyDate(props.draw.draw_date, props.timezone, props.locale)} · {props.timezone}
          </span>
        ) : null}
      </div>
      {props.draw ? (
        <div className='bg-background/70 flex flex-wrap items-end justify-between gap-4 rounded-xl border px-4 py-4'>
          <div>
            <div className='text-muted-foreground mb-2 text-xs'>
              {t('Winning number')}
            </div>
            <LuckyDigits value={props.draw.winning_number} size='lg' />
          </div>
          <div className='text-right text-xs'>
            <div className='text-muted-foreground'>{t('Full matches')}</div>
            <div className='text-foreground mt-1 font-mono text-lg font-semibold tabular-nums'>
              {props.draw.full_match_count}
            </div>
          </div>
        </div>
      ) : (
        <div className='text-muted-foreground rounded-xl border border-dashed px-4 py-6 text-sm'>
          {props.emptyText}
        </div>
      )}
    </div>
  )
}
