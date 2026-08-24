import {
  BookOpen,
  Clock3,
  ShieldCheck,
  Sparkles,
  Trophy,
  Wallet,
} from 'lucide-react'
import { motion, useReducedMotion } from 'motion/react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { formatCountdown, formatLuckyDate, formatLuckyUsd } from '../lib'
import { formatDrawTime, resolveDrawStatus } from '../lib-status'
import { EASE_OUT_QUINT } from '../motion'
import type { LuckyNumberSelfPayload } from '../types'
import { LuckyDigits } from './lucky-digits'

interface DrawStageProps {
  payload: LuckyNumberSelfPayload
  countdownSeconds: number
  onOpenRules: () => void
}

type DrawStatus = ReturnType<typeof resolveDrawStatus>

export function DrawStage(props: DrawStageProps) {
  const reduced = Boolean(useReducedMotion())
  const status = resolveDrawStatus(props.payload)

  return (
    <section className='app-page-shell overflow-hidden'>
      <StageHeader
        status={status}
        reduced={reduced}
        onOpenRules={props.onOpenRules}
      />
      <div className='grid lg:grid-cols-[minmax(0,1.4fr)_minmax(280px,0.6fr)]'>
        <DrawConsole
          payload={props.payload}
          status={status}
          countdownSeconds={props.countdownSeconds}
        />
        <JackpotSummary payload={props.payload} reduced={reduced} />
      </div>
      <ParticipationStrip
        payload={props.payload}
        countdownSeconds={props.countdownSeconds}
        status={status}
      />
    </section>
  )
}

function StageHeader(props: {
  status: DrawStatus
  reduced: boolean
  onOpenRules: () => void
}) {
  return (
    <div className='border-border/70 flex min-h-14 flex-wrap items-center justify-between gap-3 border-b px-4 py-2.5 sm:px-5'>
      <div className='flex min-w-0 items-center gap-3'>
        <span className='bg-primary text-primary-foreground flex size-9 shrink-0 items-center justify-center rounded-lg'>
          <Sparkles className='size-4' aria-hidden='true' />
        </span>
        <div className='min-w-0'>
          <h2 className='text-foreground text-sm font-semibold'>
            每日幸运数字
          </h2>
          <p className='text-muted-foreground mt-0.5 truncate text-xs'>
            月卡持续参与，盲盒每盒赠送一个当日号码
          </p>
        </div>
      </div>
      <div className='flex items-center gap-2'>
        <span className='bg-muted text-foreground inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-medium'>
          <motion.span
            className={cn('size-1.5 rounded-full', props.status.dotTone)}
            animate={
              props.reduced || props.status.phase === 'disabled'
                ? undefined
                : { opacity: [1, 0.4, 1] }
            }
            transition={{ duration: 1.8, repeat: Infinity, ease: 'easeInOut' }}
          />
          {props.status.label}
        </span>
        <Button
          variant='ghost'
          size='sm'
          onClick={props.onOpenRules}
          aria-haspopup='dialog'
        >
          <BookOpen data-icon='inline-start' />
          活动规则
        </Button>
      </div>
    </div>
  )
}

function DrawConsole(props: {
  payload: LuckyNumberSelfPayload
  status: DrawStatus
  countdownSeconds: number
}) {
  const today = props.payload.today_draw
  const title = {
    waiting: '下一组幸运数字即将揭晓',
    settling: '幸运数字已锁定，正在结算',
    completed: '今日幸运数字',
    failed: '号码已保留，系统正在重试',
    disabled: '活动暂时暂停',
  }[props.status.phase]

  return (
    <div className='lg:border-border/70 flex flex-col justify-between gap-6 px-4 py-5 sm:flex-row sm:items-center sm:px-6 sm:py-6 lg:border-r'>
      <div className='min-w-0'>
        <div className='text-info flex flex-wrap items-center gap-1.5 text-xs font-medium'>
          <Clock3 className='size-3.5' aria-hidden='true' />
          每日{' '}
          {formatDrawTime(
            props.payload.draw_hour,
            props.payload.draw_minute
          )}{' '}
          开奖
          {today ? (
            <span className='text-muted-foreground'>
              ·{' '}
              {formatLuckyDate(
                today.draw_date,
                props.payload.timezone,
                'zh-CN'
              )}
            </span>
          ) : null}
        </div>
        <h3 className='text-foreground mt-2 text-xl font-semibold tracking-tight text-balance sm:text-2xl'>
          {title}
        </h3>
        <p className='text-muted-foreground mt-1.5 max-w-xl text-sm leading-6'>
          {props.status.phase === 'disabled'
            ? '现有号码和历史记录不受影响，活动恢复后将继续自动开奖。'
            : '系统从右向左连续比对月卡尾号和当天盲盒号码，命中后奖励自动进入钱包余额。'}
        </p>
      </div>
      <div className='shrink-0'>
        <LuckyDigits
          size='lg'
          tone='activity'
          value={today?.winning_number}
          placeholder='0000'
          pending={props.status.phase === 'waiting'}
          rolling={props.status.phase === 'settling'}
          animateReveal={
            props.status.phase === 'completed' ||
            props.status.phase === 'failed'
          }
        />
      </div>
    </div>
  )
}

function JackpotSummary(props: {
  payload: LuckyNumberSelfPayload
  reduced: boolean
}) {
  const ratio =
    props.payload.jackpot_cap_usd > 0
      ? Math.min(
          100,
          (props.payload.jackpot_usd / props.payload.jackpot_cap_usd) * 100
        )
      : 0

  return (
    <aside className='border-border/70 bg-muted/20 border-t px-4 py-5 sm:px-6 lg:border-t-0'>
      <div className='flex items-center gap-2 text-sm font-semibold'>
        <Trophy className='text-primary size-4' aria-hidden='true' />
        本期累计奖池
      </div>
      <motion.div
        key={props.payload.jackpot_usd}
        className='text-foreground mt-3 font-mono text-3xl font-semibold tabular-nums'
        initial={props.reduced ? false : { opacity: 0, y: 5 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.22, ease: EASE_OUT_QUINT }}
      >
        {formatLuckyUsd(props.payload.jackpot_usd)}
      </motion.div>
      <div className='bg-muted mt-3 h-1.5 overflow-hidden rounded-full'>
        <motion.div
          className='bg-primary h-full rounded-full'
          initial={props.reduced ? false : { width: 0 }}
          animate={{ width: `${ratio}%` }}
          transition={{ duration: 0.55, ease: EASE_OUT_QUINT }}
        />
      </div>
      <div className='text-muted-foreground mt-2 flex justify-between gap-3 text-xs tabular-nums'>
        <span>四位全中者共同分享</span>
        <span>
          {ratio.toFixed(0)}% / {formatLuckyUsd(props.payload.jackpot_cap_usd)}
        </span>
      </div>
    </aside>
  )
}

function ParticipationStrip(props: {
  payload: LuckyNumberSelfPayload
  countdownSeconds: number
  status: DrawStatus
}) {
  const primaryValue = getPrimaryValue(
    props.status,
    props.countdownSeconds,
    props.payload.today_draw?.full_match_count ?? 0
  )
  const items = [
    {
      icon: Clock3,
      label: '开奖进度',
      value: primaryValue,
      tone: 'text-info bg-info/10',
    },
    {
      icon: Wallet,
      label: '奖励去向',
      value: '钱包余额',
      tone: 'text-primary bg-primary/10',
    },
    {
      icon: ShieldCheck,
      label: '参与方式',
      value: '月卡持续参与 · 盲盒当日参与',
      tone: 'text-success bg-success/10',
    },
  ]

  return (
    <div className='border-border/70 grid border-t sm:grid-cols-3'>
      {items.map((item) => (
        <div
          key={item.label}
          className='border-border/70 flex items-center gap-3 border-t px-4 py-3 first:border-t-0 sm:border-t-0 sm:border-l sm:px-5 sm:first:border-l-0'
        >
          <span
            className={cn(
              'flex size-8 shrink-0 items-center justify-center rounded-lg',
              item.tone
            )}
          >
            <item.icon className='size-4' aria-hidden='true' />
          </span>
          <div className='min-w-0'>
            <div className='text-muted-foreground text-[11px]'>
              {item.label}
            </div>
            <div className='text-foreground mt-0.5 truncate font-mono text-sm font-semibold tabular-nums'>
              {item.value}
            </div>
          </div>
        </div>
      ))}
    </div>
  )
}

function getPrimaryValue(
  status: DrawStatus,
  countdownSeconds: number,
  fullMatchCount: number
) {
  if (status.phase === 'waiting') return formatCountdown(countdownSeconds)
  if (status.phase === 'settling') return '结算中'
  if (status.phase === 'completed') return `四位全中 ${fullMatchCount} 份`
  if (status.phase === 'failed') return '自动重试'
  return '已暂停'
}
