import { CalendarClock, ScanLine, Wallet, type LucideIcon } from 'lucide-react'
import { formatDrawTime } from '../lib-status'

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
  const drawTime = formatDrawTime(props.drawHour, props.drawMinute)
  const steps: FlowStep[] = [
    {
      icon: CalendarClock,
      title: '自动参与',
      detail: `有效月卡每天 ${drawTime} 自动加入`,
    },
    {
      icon: ScanLine,
      title: '尾号对奖',
      detail: '从最右侧开始连续匹配四位号码',
    },
    {
      icon: Wallet,
      title: '即时到账',
      detail: '按命中位数和月卡倍率发放奖励',
    },
  ]

  return (
    <section
      className='app-page-shell flex flex-col overflow-hidden sm:flex-row'
      aria-label='参与流程'
    >
      <div className='border-border/70 bg-muted/25 flex shrink-0 items-center px-4 py-3 sm:w-32 sm:border-r sm:px-5'>
        <div>
          <h2 className='text-foreground text-sm font-semibold'>三步看懂</h2>
          <p className='text-muted-foreground mt-0.5 text-xs'>
            {props.timezone}
          </p>
        </div>
      </div>
      <div className='grid min-w-0 flex-1 sm:grid-cols-3'>
        {steps.map((step, index) => (
          <div
            key={step.title}
            className='border-border/70 flex min-w-0 items-start gap-3 border-t px-4 py-3.5 first:border-t-0 sm:border-t-0 sm:border-l sm:first:border-l-0'
          >
            <span className='bg-primary/10 text-primary flex size-8 shrink-0 items-center justify-center rounded-lg'>
              <step.icon className='size-4' aria-hidden='true' />
            </span>
            <div className='min-w-0'>
              <div className='text-foreground flex items-center gap-1.5 text-sm font-semibold'>
                <span className='text-muted-foreground font-mono text-[10px] tabular-nums'>
                  {index + 1}
                </span>
                {step.title}
              </div>
              <p className='text-muted-foreground mt-0.5 text-xs leading-5'>
                {step.detail}
              </p>
            </div>
          </div>
        ))}
      </div>
    </section>
  )
}
