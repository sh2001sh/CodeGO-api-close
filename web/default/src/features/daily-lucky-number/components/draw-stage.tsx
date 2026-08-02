import {
  BookOpen,
  Clock3,
  Info,
  ShieldCheck,
  Sparkles,
  Trophy,
} from 'lucide-react'
import { motion, useReducedMotion } from 'motion/react'
import { cn } from '@/lib/utils'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { formatCountdown, formatLuckyDate, formatLuckyUsd } from '../lib'
import { formatDrawTime, resolveDrawStatus } from '../lib-status'
import { EASE_OUT_QUINT, stackVariants } from '../motion'
import type { LuckyNumberSelfPayload } from '../types'
import { LuckyDigits } from './lucky-digits'

export function DrawStage(props: {
  payload: LuckyNumberSelfPayload
  countdownSeconds: number
  onOpenRules: () => void
}) {
  const reduced = Boolean(useReducedMotion())
  const { container, item } = stackVariants(reduced)
  const payload = props.payload
  const today = payload.today_draw
  const status = resolveDrawStatus(payload)
  const drawTime = formatDrawTime(payload.draw_hour, payload.draw_minute)
  const jackpotRatio =
    payload.jackpot_cap_usd > 0
      ? Math.min(100, (payload.jackpot_usd / payload.jackpot_cap_usd) * 100)
      : 0

  return (
    <motion.section
      className='app-page-shell overflow-hidden'
      variants={container}
      initial='initial'
      animate='animate'
    >
      <div className='border-border/70 flex flex-wrap items-start justify-between gap-4 border-b px-4 py-4 sm:px-6'>
        <div className='flex min-w-0 items-start gap-3'>
          <StageIcon phase={status.phase} reduced={reduced} />
          <div className='min-w-0'>
            <div className='app-section-kicker uppercase'>
              每日一次 · 自动参与
            </div>
            <h2 className='text-foreground mt-1 text-lg font-semibold tracking-tight sm:text-xl'>
              今日开奖
            </h2>
            <p className='text-muted-foreground mt-1 text-xs leading-5'>
              {status.headline}
            </p>
          </div>
        </div>
        <div className='flex flex-wrap items-center gap-2'>
          <span
            className={cn(
              'inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-medium',
              status.tone
            )}
          >
            <motion.span
              className={cn('size-1.5 rounded-full', status.dotTone)}
              animate={
                reduced || status.phase === 'disabled'
                  ? undefined
                  : { opacity: [1, 0.35, 1], scale: [1, 0.82, 1] }
              }
              transition={{
                duration: 1.9,
                repeat: Infinity,
                ease: 'easeInOut',
              }}
            />
            {status.label}
          </span>
          <Button
            variant='outline'
            size='sm'
            onClick={props.onOpenRules}
            aria-haspopup='dialog'
          >
            <BookOpen data-icon='inline-start' />
            规则说明
          </Button>
        </div>
      </div>

      {status.phase === 'disabled' ? (
        <Alert className='mx-4 mt-4 sm:mx-6'>
          <Info aria-hidden='true' />
          <AlertDescription>
            活动暂时不可用，现有套餐额度和历史记录不受影响。
          </AlertDescription>
        </Alert>
      ) : null}

      <div className='grid xl:grid-cols-[minmax(0,1.4fr)_minmax(280px,0.6fr)]'>
        <motion.div className='p-4 sm:p-6' variants={item}>
          <div className='border-primary/25 bg-primary/[0.045] overflow-hidden rounded-2xl border'>
            <div className='border-primary/20 flex flex-wrap items-center justify-between gap-3 border-b border-dashed px-4 py-3 sm:px-5'>
              <span className='text-foreground text-xs font-medium'>
                {today ? '今日开奖号码' : '本期开奖周期'}
                {today ? (
                  <span className='text-muted-foreground ml-1.5'>
                    {formatLuckyDate(
                      today.draw_date,
                      payload.timezone,
                      'zh-CN'
                    )}
                  </span>
                ) : null}
              </span>
              <span className='text-muted-foreground inline-flex items-center gap-1.5 font-mono text-xs tabular-nums'>
                <Clock3 className='size-3.5' aria-hidden='true' />
                每日 {drawTime} · {payload.timezone}
              </span>
            </div>

            <div className='px-4 py-6 sm:px-6 sm:py-7'>
              <div className='flex justify-center'>
                <LuckyDigits
                  size='lg'
                  value={today?.winning_number}
                  placeholder='0000'
                  pending={status.phase === 'waiting'}
                  rolling={status.phase === 'settling'}
                  animateReveal={
                    status.phase === 'completed' || status.phase === 'failed'
                  }
                />
              </div>

              <div className='mt-6 grid gap-3 sm:grid-cols-2'>
                <StagePrimaryMetric
                  phase={status.phase}
                  countdownSeconds={props.countdownSeconds}
                  fullMatchCount={today?.full_match_count ?? 0}
                />
                <div className='border-primary/20 bg-background/70 rounded-xl border px-4 py-3.5'>
                  <div className='text-muted-foreground text-[11px]'>
                    参与与到账
                  </div>
                  <div className='text-foreground mt-1 text-sm font-semibold'>
                    月卡参与，钱包到账
                  </div>
                  <p className='text-muted-foreground mt-1 text-[11px] leading-5'>
                    无需签到，不出售额外次数；中奖额度直接进入钱包余额，永久有效。
                  </p>
                </div>
              </div>
            </div>
          </div>
        </motion.div>

        <motion.aside
          className='border-border/70 bg-muted/20 border-t px-4 py-5 sm:px-6 xl:border-t-0 xl:border-l'
          variants={item}
        >
          <div className='flex items-center gap-2'>
            <span className='bg-warning/15 text-warning flex size-8 items-center justify-center rounded-lg'>
              <Trophy className='size-4' aria-hidden='true' />
            </span>
            <div>
              <div className='text-foreground text-sm font-semibold'>
                本期累计奖池
              </div>
              <div className='text-muted-foreground text-xs'>
                四位全中时由中奖月卡平分
              </div>
            </div>
          </div>

          <motion.div
            className='mt-5 font-mono text-4xl font-semibold tracking-tight tabular-nums'
            initial={reduced ? false : { opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.45, ease: EASE_OUT_QUINT, delay: 0.15 }}
          >
            {formatLuckyUsd(payload.jackpot_usd)}
          </motion.div>

          <div className='bg-muted mt-3 h-1.5 overflow-hidden rounded-full'>
            <motion.div
              className='bg-warning h-full rounded-full'
              initial={reduced ? false : { width: 0 }}
              animate={{ width: `${jackpotRatio}%` }}
              transition={{ duration: 0.8, ease: EASE_OUT_QUINT, delay: 0.2 }}
            />
          </div>
          <div className='text-muted-foreground mt-2 text-xs tabular-nums'>
            上限 {formatLuckyUsd(payload.jackpot_cap_usd)} · 已累积{' '}
            {jackpotRatio.toFixed(0)}%
          </div>

          <div className='border-border mt-5 space-y-2 border-t pt-4 text-xs leading-5'>
            <div className='text-foreground flex items-center gap-2 font-medium'>
              <ShieldCheck
                className='text-primary size-3.5'
                aria-hidden='true'
              />
              奖池与奖励边界
            </div>
            <p className='text-muted-foreground'>
              无人四位全中时奖池累积，有人全中后按规则分配并重置。
            </p>
            <p className='text-muted-foreground'>
              奖励只进钱包余额，不可提现、交易或转让；号码与记录长期保留。
            </p>
          </div>
        </motion.aside>
      </div>
    </motion.section>
  )
}

function StageIcon(props: {
  phase: ReturnType<typeof resolveDrawStatus>['phase']
  reduced: boolean
}) {
  const spin = !props.reduced && props.phase === 'settling'

  return (
    <motion.span
      className='bg-primary text-primary-foreground flex size-10 shrink-0 items-center justify-center rounded-xl shadow-sm'
      animate={spin ? { rotate: 360 } : undefined}
      transition={
        spin ? { duration: 2.4, repeat: Infinity, ease: 'linear' } : undefined
      }
    >
      <Sparkles className='size-5' aria-hidden='true' />
    </motion.span>
  )
}

function StagePrimaryMetric(props: {
  phase: ReturnType<typeof resolveDrawStatus>['phase']
  countdownSeconds: number
  fullMatchCount: number
}) {
  const config = {
    waiting: { label: '距离今日开奖', detail: '系统自动开奖，无需操作' },
    settling: { label: '当前状态', detail: '中奖额度正在写入钱包' },
    completed: { label: '四位全中', detail: '本期获得全中奖励的月卡数量' },
    failed: { label: '处理方式', detail: '系统自动重试，号码不会变更' },
    disabled: { label: '活动状态', detail: '恢复后将继续每日开奖' },
  }[props.phase]

  const value =
    props.phase === 'waiting'
      ? formatCountdown(props.countdownSeconds)
      : props.phase === 'settling'
        ? '结算中'
        : props.phase === 'completed'
          ? `${props.fullMatchCount} 份`
          : props.phase === 'failed'
            ? '自动重试'
            : '已暂停'

  return (
    <div className='border-primary/25 bg-background rounded-xl border px-4 py-3.5'>
      <div className='text-muted-foreground text-[11px]'>{config.label}</div>
      <div className='text-foreground mt-1 font-mono text-2xl font-semibold tabular-nums sm:text-3xl'>
        {value}
      </div>
      <p className='text-muted-foreground mt-1 text-[11px] leading-5'>
        {config.detail}
      </p>
    </div>
  )
}
