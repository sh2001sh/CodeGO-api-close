import {
  BookOpen,
  Clock3,
  Gift,
  ShieldCheck,
  Sparkles,
  Trophy,
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
    <section className='overflow-hidden rounded-2xl bg-[oklch(0.235_0.035_42)] text-white shadow-[0_8px_24px_rgba(45,29,18,0.16)]'>
      <StageHeader
        status={status}
        reduced={reduced}
        onOpenRules={props.onOpenRules}
      />
      <div className='grid lg:grid-cols-[minmax(0,1.45fr)_minmax(280px,0.55fr)]'>
        <StageMain
          payload={props.payload}
          status={status}
          countdownSeconds={props.countdownSeconds}
        />
        <JackpotAside payload={props.payload} reduced={reduced} />
      </div>
    </section>
  )
}

function StageHeader(props: {
  status: DrawStatus
  reduced: boolean
  onOpenRules: () => void
}) {
  return (
    <div className='flex flex-wrap items-center justify-between gap-3 border-b border-white/10 px-4 py-3 sm:px-6'>
      <div className='flex items-center gap-2 text-xs text-white/70'>
        <span className='bg-primary text-primary-foreground flex size-7 items-center justify-center rounded-lg'>
          <Sparkles className='size-3.5' aria-hidden='true' />
        </span>
        <span className='font-semibold text-white'>每日幸运数字</span>
        <span aria-hidden='true'>/</span>
        <span>有效月卡自动参与</span>
      </div>
      <div className='flex items-center gap-2'>
        <span className='inline-flex items-center gap-1.5 rounded-full bg-white/10 px-2.5 py-1 text-xs font-medium text-white'>
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
          className='text-white hover:bg-white/10 hover:text-white'
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

function StageMain(props: {
  payload: LuckyNumberSelfPayload
  status: DrawStatus
  countdownSeconds: number
}) {
  const today = props.payload.today_draw
  const drawTime = formatDrawTime(
    props.payload.draw_hour,
    props.payload.draw_minute
  )
  const title = {
    waiting: '下一组幸运数字，即将揭晓',
    settling: '幸运数字已锁定，正在结算',
    completed: '今日幸运数字已经揭晓',
    failed: '今日号码已保留，系统正在重试',
    disabled: '活动暂时暂停',
  }[props.status.phase]
  const primaryFact = getPrimaryFact(
    props.status,
    props.countdownSeconds,
    today?.full_match_count ?? 0
  )

  return (
    <div className='px-4 py-7 sm:px-8 sm:py-9'>
      <div className='max-w-xl'>
        <div className='flex flex-wrap items-center gap-2 text-xs text-white/70'>
          <span className='inline-flex items-center gap-1.5'>
            <Clock3 className='size-3.5' aria-hidden='true' />
            每日 {drawTime} · {props.payload.timezone}
          </span>
          {today ? (
            <>
              <span aria-hidden='true'>·</span>
              <span>
                {formatLuckyDate(
                  today.draw_date,
                  props.payload.timezone,
                  'zh-CN'
                )}
              </span>
            </>
          ) : null}
        </div>
        <h2 className='mt-4 text-2xl font-semibold tracking-tight text-balance sm:text-3xl'>
          {title}
        </h2>
        <p className='mt-2 max-w-lg text-sm leading-6 text-white/70'>
          {props.status.phase === 'disabled'
            ? '现有月卡号码和历史记录不受影响，活动恢复后将继续自动开奖。'
            : '系统自动对齐你的月卡尾号，从右向左连续匹配。无需签到，也不用提交任何表单。'}
        </p>
        <div className='mt-7 flex justify-start'>
          <LuckyDigits
            size='lg'
            tone='stage'
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
        <div className='mt-7 flex flex-wrap gap-x-7 gap-y-3'>
          <StageFact {...primaryFact} />
          <StageFact label='奖励去向' value='钱包余额' />
          <StageFact label='参与方式' value='月卡自动加入' />
        </div>
      </div>
    </div>
  )
}

function JackpotAside(props: {
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
    <aside className='border-t border-white/10 bg-black/10 px-4 py-6 sm:px-6 lg:border-t-0 lg:border-l'>
      <div className='flex items-center gap-2.5'>
        <span className='flex size-9 items-center justify-center rounded-lg bg-[oklch(0.79_0.15_78)] text-[oklch(0.25_0.04_45)]'>
          <Trophy className='size-4' aria-hidden='true' />
        </span>
        <div>
          <div className='text-sm font-semibold'>本期累计奖池</div>
          <div className='mt-0.5 text-xs text-white/70'>四位全中者共同分享</div>
        </div>
      </div>
      <motion.div
        key={props.payload.jackpot_usd}
        className='mt-6 font-mono text-4xl font-semibold tabular-nums'
        initial={props.reduced ? false : { opacity: 0, y: 6 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.22, ease: EASE_OUT_QUINT }}
      >
        {formatLuckyUsd(props.payload.jackpot_usd)}
      </motion.div>
      <div className='mt-4 h-1.5 overflow-hidden rounded-full bg-white/10'>
        <motion.div
          className='h-full rounded-full bg-[oklch(0.79_0.15_78)]'
          initial={props.reduced ? false : { width: 0 }}
          animate={{ width: `${ratio}%` }}
          transition={{ duration: 0.55, ease: EASE_OUT_QUINT }}
        />
      </div>
      <div className='mt-2 text-xs text-white/70 tabular-nums'>
        上限 {formatLuckyUsd(props.payload.jackpot_cap_usd)} · 当前{' '}
        {ratio.toFixed(0)}%
      </div>
      <div className='mt-6 space-y-3 border-t border-white/10 pt-5 text-xs leading-5 text-white/70'>
        <AsideFact icon={Gift}>
          无人四位全中时，奖池自动累积至下一期。
        </AsideFact>
        <AsideFact icon={ShieldCheck}>
          中奖额度直接到账，号码和开奖记录长期保留。
        </AsideFact>
      </div>
    </aside>
  )
}

function AsideFact(props: { icon: typeof Gift; children: string }) {
  return (
    <div className='flex gap-2'>
      <props.icon
        className='mt-0.5 size-3.5 shrink-0 text-[oklch(0.79_0.15_78)]'
        aria-hidden='true'
      />
      <span>{props.children}</span>
    </div>
  )
}

function StageFact(props: { label: string; value: string }) {
  return (
    <div className='min-w-28'>
      <div className='text-[11px] text-white/60'>{props.label}</div>
      <div className='mt-1 font-mono text-sm font-semibold text-white tabular-nums'>
        {props.value}
      </div>
    </div>
  )
}

function getPrimaryFact(
  status: DrawStatus,
  countdownSeconds: number,
  fullMatchCount: number
) {
  if (status.phase === 'waiting') {
    return { label: '距离开奖', value: formatCountdown(countdownSeconds) }
  }
  if (status.phase === 'settling') {
    return { label: '当前状态', value: '结算中' }
  }
  if (status.phase === 'completed') {
    return { label: '本期四位全中', value: `${fullMatchCount} 份` }
  }
  if (status.phase === 'failed') {
    return { label: '当前状态', value: '自动重试' }
  }
  return { label: '当前状态', value: '已暂停' }
}
