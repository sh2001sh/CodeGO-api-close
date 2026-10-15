import type { LuckyNumberSelfPayload } from './types'

export type DrawPhase =
  | 'disabled'
  | 'waiting'
  | 'settling'
  | 'completed'
  | 'failed'

export interface DrawStatusView {
  phase: DrawPhase
  label: string
  tone: string
  dotTone: string
  headline: string
}

export function resolveDrawStatus(
  payload: LuckyNumberSelfPayload
): DrawStatusView {
  const today = payload.today_draw
  const published = Boolean(today?.winning_number)
  const completed = today?.status === 'completed'
  const failed = today?.status === 'failed'

  if (!payload.enabled) {
    return {
      phase: 'disabled',
      label: '活动暂不可用',
      tone: 'border-muted-foreground/20 bg-muted text-muted-foreground',
      dotTone: 'bg-muted-foreground',
      headline: '活动已暂停，历史号码与额度不受影响',
    }
  }
  if (failed) {
    return {
      phase: 'failed',
      label: '开奖待处理',
      tone: 'border-warning/25 bg-warning/10 text-warning',
      dotTone: 'bg-warning',
      headline: '本期结算异常，系统会自动重试，不会重新生成号码',
    }
  }
  if (completed) {
    return {
      phase: 'completed',
      label: '今日已结算',
      tone: 'border-success/20 bg-success/10 text-success',
      dotTone: 'bg-success',
      headline: '今日号码已公布，中奖额度已进入钱包余额',
    }
  }
  if (published) {
    return {
      phase: 'settling',
      label: '结算处理中',
      tone: 'border-primary/20 bg-primary/10 text-primary',
      dotTone: 'bg-primary',
      headline: '号码已生成，正在逐张月卡对号并结算到钱包',
    }
  }
  return {
    phase: 'waiting',
    label: '等待今日开奖',
    tone: 'border-primary/20 bg-primary/10 text-primary',
    dotTone: 'bg-primary',
    headline: '全站每天开出一个四位号码，月卡自动参与',
  }
}

export function formatDrawTime(hour: number, minute: number): string {
  return `${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}`
}
