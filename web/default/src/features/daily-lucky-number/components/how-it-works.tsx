import { CalendarClock, ScanLine, Wallet, type LucideIcon } from 'lucide-react'
import { motion, useReducedMotion } from 'motion/react'
import { formatDrawTime } from '../lib-status'
import { stackVariants } from '../motion'

interface FlowStep {
  icon: LucideIcon
  title: string
  detail: string
}

export function HowItWorks(props: {
  drawHour: number
  drawMinute: number
  timezone: string
}) {
  const reduced = Boolean(useReducedMotion())
  const { container, item } = stackVariants(reduced)
  const drawTime = formatDrawTime(props.drawHour, props.drawMinute)

  const steps: FlowStep[] = [
    {
      icon: CalendarClock,
      title: '每天开一个号码',
      detail: `每日 ${drawTime}（${props.timezone}）全站开出唯一四位号码，你的有效月卡自动参与，无需签到或购买次数。`,
    },
    {
      icon: ScanLine,
      title: '从右往左逐位对比',
      detail:
        '拿月卡号码的后四位与开奖号码逐位对齐，从最右侧开始数连续对上几位。最右侧一位没对上就算未命中。',
    },
    {
      icon: Wallet,
      title: '奖励直接进钱包',
      detail:
        '连续命中位数决定基础奖励档位，再乘月卡档位倍率；只发放最高命中的那一档，额度直接进入钱包余额，永久有效且不随月卡到期清零。',
    },
  ]

  return (
    <motion.section
      className='grid gap-3 sm:grid-cols-3'
      variants={container}
      initial='initial'
      animate='animate'
      aria-label='玩法说明'
    >
      {steps.map((step, index) => (
        <motion.div
          key={step.title}
          variants={item}
          className='app-subtle-panel flex gap-3 p-4'
        >
          <span className='bg-primary/10 text-primary flex size-9 shrink-0 items-center justify-center rounded-lg'>
            <step.icon className='size-4' aria-hidden='true' />
          </span>
          <div className='min-w-0'>
            <div className='flex items-center gap-1.5'>
              <span className='text-muted-foreground font-mono text-[11px] tabular-nums'>
                0{index + 1}
              </span>
              <h3 className='text-foreground text-sm font-semibold'>
                {step.title}
              </h3>
            </div>
            <p className='text-muted-foreground mt-1 text-xs leading-5'>
              {step.detail}
            </p>
          </div>
        </motion.div>
      ))}
    </motion.section>
  )
}
