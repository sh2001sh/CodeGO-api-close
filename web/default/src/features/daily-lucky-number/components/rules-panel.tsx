import { useState, type ReactNode } from 'react'
import {
  BookOpen,
  Check,
  ChevronDown,
  Clock3,
  Coins,
  ShieldCheck,
  Trophy,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { formatLuckyUsd, normalizeLuckyNumberRules } from '../lib'
import type { LuckyNumberRules } from '../types'

const rewardRows = [
  {
    label: '未命中',
    match: '0 位',
    key: 'none',
    description: '本期不发放奖励',
  },
  {
    label: '基础奖励',
    match: '1 位',
    key: 'base_reward_1_usd',
    description: '连续匹配最右侧 1 位',
  },
  {
    label: '进阶奖励',
    match: '2 位',
    key: 'base_reward_2_usd',
    description: '连续匹配最右侧 2 位',
  },
  {
    label: '高阶奖励',
    match: '3 位',
    key: 'base_reward_3_usd',
    description: '连续匹配最右侧 3 位',
  },
  {
    label: '全中奖励',
    match: '4 位',
    key: 'base_reward_4_usd',
    description: '基础奖励加累计奖池',
  },
] as const

export function DailyLuckyRulesPanel(props: {
  open?: boolean
  defaultOpen?: boolean
  onOpenChange?: (open: boolean) => void
  rules?: Partial<LuckyNumberRules> | null
  timezone?: string
  drawHour?: number
  drawMinute?: number
}) {
  const rules = normalizeLuckyNumberRules(props.rules)
  const timezone = props.timezone || 'Asia/Shanghai'
  const drawTime = `${String(props.drawHour ?? 20).padStart(2, '0')}:${String(props.drawMinute ?? 0).padStart(2, '0')}`
  const [internalOpen, setInternalOpen] = useState(props.defaultOpen ?? false)
  const isOpen = props.open ?? internalOpen
  const handleOpenChange = (nextOpen: boolean) => {
    if (props.open === undefined) setInternalOpen(nextOpen)
    props.onOpenChange?.(nextOpen)
  }
  const tiers = [
    {
      name: 'Lite',
      label: '轻享月卡',
      multiplier: rules.multiplier_lite,
      tone: 'text-slate-600',
    },
    {
      name: 'Standard',
      label: '标准月卡',
      multiplier: rules.multiplier_standard,
      tone: 'text-blue-600',
    },
    {
      name: 'Pro',
      label: '专业月卡',
      multiplier: rules.multiplier_pro,
      tone: 'text-violet-600',
    },
    {
      name: 'Ultra',
      label: '旗舰月卡',
      multiplier: rules.multiplier_ultra,
      tone: 'text-amber-700',
    },
  ]

  return (
    <Collapsible
      id='daily-lucky-rules'
      className='app-page-shell scroll-mt-4 overflow-hidden'
      open={isOpen}
      onOpenChange={handleOpenChange}
    >
      <CollapsibleTrigger
        aria-controls='daily-lucky-rules-content'
        className='border-border/70 hover:bg-muted/20 focus-visible:bg-muted/20 flex w-full flex-wrap items-start justify-between gap-4 border-b px-4 py-4 text-left transition-colors sm:px-6'
      >
        <span className='flex min-w-0 items-start gap-3'>
          <span className='bg-primary/10 text-primary flex size-9 shrink-0 items-center justify-center rounded-lg'>
            <BookOpen className='size-4' aria-hidden='true' />
          </span>
          <span className='min-w-0'>
            <span
              role='heading'
              aria-level={2}
              className='text-foreground block text-base font-semibold sm:text-lg'
            >
              活动规则与月卡倍率
            </span>
            <span className='text-muted-foreground mt-1 block max-w-3xl text-sm leading-6'>
              四档月卡共享同一个全站开奖号码，奖励先按命中档位确定，再乘以对应月卡倍率。点击展开查看完整规则。
            </span>
          </span>
        </span>
        <span className='text-muted-foreground inline-flex shrink-0 items-center gap-2 text-xs'>
          <span className='inline-flex items-center gap-1.5'>
            <Clock3 className='size-3.5' aria-hidden='true' />
            每天 {drawTime} · {timezone}
          </span>
          <span className='text-primary font-medium'>
            {isOpen ? '收起规则' : '展开规则'}
          </span>
          <ChevronDown
            className={cn(
              'size-4 transition-transform',
              isOpen && 'rotate-180'
            )}
            aria-hidden='true'
          />
        </span>
      </CollapsibleTrigger>

      <CollapsibleContent
        id='daily-lucky-rules-content'
        className='data-closed:animate-accordion-up data-open:animate-accordion-down overflow-hidden'
      >
        <div className='grid gap-5 p-4 sm:p-6 xl:grid-cols-[minmax(0,1.05fr)_minmax(320px,0.95fr)]'>
          <div className='min-w-0 space-y-4'>
            <div>
              <h3 className='text-foreground text-sm font-semibold'>
                月卡奖励倍率
              </h3>
              <p className='text-muted-foreground mt-1 text-xs leading-5'>
                倍率只影响每日幸运号奖励，不改变月卡价格或基础额度。
              </p>
            </div>
            <div className='overflow-x-auto rounded-lg border'>
              <table className='w-full min-w-[430px] text-sm'>
                <thead className='bg-muted/45 text-muted-foreground text-left text-xs'>
                  <tr>
                    <th className='px-3 py-2.5 font-medium'>月卡等级</th>
                    <th className='px-3 py-2.5 font-medium'>显示名称</th>
                    <th className='px-3 py-2.5 text-right font-medium'>
                      奖励倍率
                    </th>
                  </tr>
                </thead>
                <tbody className='divide-border divide-y'>
                  {tiers.map((tier) => (
                    <tr key={tier.name}>
                      <td className={cn('px-3 py-3 font-semibold', tier.tone)}>
                        {tier.name}
                      </td>
                      <td className='text-muted-foreground px-3 py-3'>
                        {tier.label}
                      </td>
                      <td className='text-foreground px-3 py-3 text-right font-mono font-semibold tabular-nums'>
                        {Number(tier.multiplier).toFixed(1)}x
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className='border-primary/20 bg-primary/[0.045] flex items-start gap-2.5 rounded-lg border px-3.5 py-3 text-xs leading-5'>
              <Coins
                className='text-primary mt-0.5 size-4 shrink-0'
                aria-hidden='true'
              />
              <p className='text-muted-foreground'>
                示例：Pro 月卡命中 2 位时，奖励为{' '}
                {formatLuckyUsd(rules.base_reward_2_usd)} ×{' '}
                {Number(rules.multiplier_pro).toFixed(1)} ={' '}
                {formatLuckyUsd(rules.base_reward_2_usd * rules.multiplier_pro)}
                。
              </p>
            </div>
          </div>

          <div className='border-border bg-muted/20 min-w-0 rounded-xl border p-4 sm:p-5'>
            <h3 className='text-foreground text-sm font-semibold'>参与流程</h3>
            <div className='mt-4 space-y-4'>
              <RuleStep
                number='1'
                title='有效月卡自动参与'
                detail='开奖快照时，处于有效期且开启每日幸运号的月卡自动进入本期。'
              />
              <RuleStep
                number='2'
                title='全站统一四位开奖号码'
                detail='系统每天只开奖一次，号码范围为 0000-9999，前导零会保留。'
              />
              <RuleStep
                number='3'
                title='从右向左连续匹配'
                detail='只计算连续尾号匹配，命中多档时只领取最高档，不重复叠加。'
              />
              <RuleStep
                number='4'
                title='奖励自动进入月卡额度'
                detail='无需签到、领取或购买额外次数，结算后自动记入对应月卡。'
              />
            </div>
          </div>
        </div>

        <div className='border-border/70 border-t px-4 py-4 sm:px-6 sm:py-5'>
          <div className='flex flex-wrap items-end justify-between gap-2'>
            <div>
              <h3 className='text-foreground text-sm font-semibold'>
                基础奖励档位
              </h3>
              <p className='text-muted-foreground mt-1 text-xs'>
                基础奖励乘以月卡倍率后，计入对应月卡额度。
              </p>
            </div>
            <span className='text-muted-foreground text-xs'>
              同一期只按最高命中档结算
            </span>
          </div>
          <div className='mt-4 grid grid-cols-2 gap-2 sm:grid-cols-5'>
            {rewardRows.map((row) => {
              const amount = row.key === 'none' ? 0 : rules[row.key]
              return (
                <div
                  key={row.key}
                  className='border-border bg-background min-w-0 rounded-lg border px-3 py-3'
                >
                  <div className='text-muted-foreground text-xs'>
                    {row.label}
                  </div>
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
        </div>

        <div className='border-border/70 border-t px-4 py-4 sm:px-6 sm:py-5'>
          <div className='grid gap-4 text-sm md:grid-cols-3'>
            <RuleNote icon={<Trophy aria-hidden='true' />} title='累计奖池'>
              初始 {formatLuckyUsd(rules.jackpot_initial_usd)}
              ；无人四位全中时增加 {formatLuckyUsd(rules.jackpot_increment_usd)}
              ，上限 {formatLuckyUsd(rules.jackpot_cap_usd)}
              。有人四位全中时由全中月卡平分，下一期开奖重置为初始值。
            </RuleNote>
            <RuleNote
              icon={<ShieldCheck aria-hidden='true' />}
              title='号码与历史'
            >
              完整月卡编号全局唯一，四位幸运尾号允许重复。月卡过期后编号和开奖记录仍可查看，但不再参与新开奖。
            </RuleNote>
            <RuleNote icon={<Check aria-hidden='true' />} title='奖励限制'>
              奖励进入对应月卡额度，不进入普通钱包，不能提现、交易或转让；活动不提供额外购买开奖次数。
            </RuleNote>
          </div>
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}

function RuleStep(props: { number: string; title: string; detail: string }) {
  return (
    <div className='flex items-start gap-3'>
      <span className='bg-primary/10 text-primary flex size-6 shrink-0 items-center justify-center rounded-full text-xs font-semibold'>
        {props.number}
      </span>
      <div className='min-w-0'>
        <div className='text-foreground text-sm font-medium'>{props.title}</div>
        <p className='text-muted-foreground mt-1 text-xs leading-5'>
          {props.detail}
        </p>
      </div>
    </div>
  )
}

function RuleNote(props: {
  icon: ReactNode
  title: string
  children: ReactNode
}) {
  return (
    <div className='flex items-start gap-2.5'>
      <span className='text-primary mt-0.5 size-4 shrink-0 [&>svg]:size-4'>
        {props.icon}
      </span>
      <div className='min-w-0'>
        <div className='text-foreground font-medium'>{props.title}</div>
        <p className='text-muted-foreground mt-1 leading-5'>{props.children}</p>
      </div>
    </div>
  )
}
