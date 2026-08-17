import {
  BarChart3,
  Beaker,
  Loader2,
  RotateCcw,
  ShieldCheck,
  Sparkles,
} from 'lucide-react'
import { calculateBlindBoxEconomics } from '@/lib/blind-box-economics'
import { formatUsdAmount, quotaUnitsToUsd } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import type { BalanceBlindBoxOverview } from '../types'
import { BalanceBoxQuantityControl } from './balance-blind-box-controls'
import {
  type SimulationHistoryItem,
  useBalanceBlindBoxSimulator,
} from './use-balance-blind-box-simulator'

export function BalanceBlindBoxSimulator(props: {
  balance?: BalanceBlindBoxOverview
}) {
  const priceUSD = props.balance?.price_usd || 2.5
  const state = useBalanceBlindBoxSimulator(priceUSD)
  if (!state.session || !state.stats) {
    return <SimulationSetup priceUSD={priceUSD} state={state} />
  }
  return (
    <SimulationWorkspace
      priceUSD={priceUSD}
      balance={props.balance}
      state={state}
    />
  )
}

function SimulationSetup(props: {
  priceUSD: number
  state: ReturnType<typeof useBalanceBlindBoxSimulator>
}) {
  return (
    <div className='border-primary/20 bg-primary/[0.035] rounded-lg border p-4 sm:p-5'>
      <div className='flex items-start gap-3'>
        <div className='bg-primary/10 text-primary flex size-9 shrink-0 items-center justify-center rounded-md'>
          <Beaker className='size-4' />
        </div>
        <div className='min-w-0'>
          <h3 className='text-sm font-semibold'>建立模拟额度账户</h3>
          <p className='text-muted-foreground mt-1 max-w-2xl text-xs leading-5'>
            输入一笔虚拟通用额度，按真实奖池、开启保底和“再来一抽”连续结算。模拟结果不会进入钱包、库存或使用记录。
          </p>
        </div>
      </div>
      <div className='mt-5 max-w-md space-y-2'>
        <Label htmlFor='blind-box-simulation-balance'>模拟初始额度</Label>
        <div className='relative'>
          <span className='text-muted-foreground pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-sm'>
            $
          </span>
          <Input
            id='blind-box-simulation-balance'
            type='number'
            min={props.priceUSD}
            max={1_000_000}
            step='0.01'
            inputMode='decimal'
            className='pl-7 text-base tabular-nums'
            value={props.state.initialAmount}
            onChange={(event) =>
              props.state.setInitialAmount(event.target.value)
            }
            onKeyDown={(event) => {
              if (event.key === 'Enter' && props.state.canStart)
                props.state.start()
            }}
          />
        </div>
        <div className='flex flex-wrap items-center justify-between gap-3'>
          <p className='text-muted-foreground text-xs'>
            每盒 {formatUsdAmount(props.priceUSD)}，最高可设置 $1,000,000
          </p>
          <Button
            type='button'
            disabled={!props.state.canStart}
            onClick={props.state.start}
          >
            <Beaker className='size-4' />
            开始模拟
          </Button>
        </div>
      </div>
    </div>
  )
}

function SimulationWorkspace(props: {
  priceUSD: number
  balance?: BalanceBlindBoxOverview
  state: ReturnType<typeof useBalanceBlindBoxSimulator>
}) {
  const stats = props.state.stats!
  return (
    <div className='space-y-4'>
      <div className='border-primary/20 bg-primary/[0.035] rounded-lg border'>
        <div className='flex flex-wrap items-start justify-between gap-3 px-4 py-3 sm:px-5'>
          <div>
            <div className='flex items-center gap-2 text-sm font-semibold'>
              <Beaker className='text-primary size-4' />
              模拟账户运行中
            </div>
            <p className='text-muted-foreground mt-1 text-xs'>
              仅用于概率体验，不影响任何真实资产
            </p>
          </div>
          <Button
            type='button'
            variant='ghost'
            size='sm'
            onClick={props.state.reset}
          >
            <RotateCcw className='size-4' />
            重设额度
          </Button>
        </div>
        <SimulationMetrics
          stats={stats}
          priceUSD={props.priceUSD}
          tiers={props.balance?.tiers || []}
        />
        <div className='grid gap-4 border-t p-4 sm:p-5 lg:grid-cols-[minmax(0,1fr)_280px]'>
          <SimulationHistory history={stats.history} />
          <div className='border-primary/15 bg-background/55 space-y-4 rounded-lg border p-4'>
            <BalanceBoxQuantityControl
              count={props.state.count}
              max={Math.max(1, props.state.maxCount)}
              disabled={props.state.busy || props.state.maxCount === 0}
              onChange={props.state.setCount}
            />
            <SimulationGuaranteeStatus stats={stats} balance={props.balance} />
            <div className='flex items-end justify-between gap-3 border-t pt-3'>
              <div>
                <p className='text-muted-foreground text-[11px]'>
                  本次模拟投入
                </p>
                <p className='text-lg font-semibold tabular-nums'>
                  {formatUsdAmount(props.priceUSD * props.state.count)}
                </p>
              </div>
              <p className='text-muted-foreground text-right text-[11px] leading-4'>
                当前最多可抽 {props.state.maxCount} 个
              </p>
            </div>
            <Button
              type='button'
              className='w-full'
              disabled={!props.state.canDraw}
              onClick={() => void props.state.draw()}
            >
              {props.state.busy ? (
                <Loader2 className='size-4 animate-spin' />
              ) : (
                <Sparkles className='size-4' />
              )}
              {props.state.maxCount > 0 ? '模拟抽取' : '模拟额度已不足'}
            </Button>
          </div>
        </div>
      </div>
    </div>
  )
}

function SimulationGuaranteeStatus(props: {
  stats: NonNullable<ReturnType<typeof useBalanceBlindBoxSimulator>['stats']>
  balance?: BalanceBlindBoxOverview
}) {
  const smallThreshold = props.balance?.small_pity_threshold || 10
  const bigThreshold = props.balance?.pity_threshold || 50
  return (
    <div className='space-y-3 border-t pt-3'>
      <div className='flex items-center justify-between gap-3'>
        <div className='flex items-center gap-1.5 text-xs font-semibold'>
          <ShieldCheck className='text-primary size-3.5' aria-hidden='true' />
          模拟保底进度
        </div>
        <span className='text-muted-foreground text-[10px]'>连续请求保留</span>
      </div>
      <div className='flex items-center justify-between gap-3 text-[11px]'>
        <span className='text-muted-foreground'>首抽保底</span>
        <span className='font-medium'>
          {props.stats.firstDrawEligible ? '下一抽触发' : '本轮已触发'}
        </span>
      </div>
      <SimulationPityProgress
        label='小保底'
        progress={props.stats.smallPityProgress}
        threshold={smallThreshold}
      />
      <SimulationPityProgress
        label='大保底'
        progress={props.stats.pityProgress}
        threshold={bigThreshold}
      />
    </div>
  )
}

function SimulationPityProgress(props: {
  label: string
  progress: number
  threshold: number
}) {
  const target = Math.max(1, props.threshold - 1)
  const progress = Math.min(target, Math.max(0, props.progress))
  const ready = progress >= target
  return (
    <div>
      <div className='mb-1 flex items-center justify-between gap-3 text-[11px]'>
        <span className='text-muted-foreground'>{props.label}</span>
        <span className='font-medium tabular-nums'>
          {ready ? '下一抽触发' : `${progress}/${target}`}
        </span>
      </div>
      <div
        className='bg-muted h-1.5 overflow-hidden rounded-full'
        role='progressbar'
        aria-label={`${props.label}进度`}
        aria-valuemin={0}
        aria-valuemax={target}
        aria-valuenow={progress}
      >
        <div
          className='bg-primary h-full rounded-full transition-[width] duration-300 motion-reduce:transition-none'
          style={{ width: `${(progress / target) * 100}%` }}
        />
      </div>
    </div>
  )
}

function SimulationMetrics(props: {
  stats: NonNullable<ReturnType<typeof useBalanceBlindBoxSimulator>['stats']>
  priceUSD: number
  tiers: BalanceBlindBoxOverview['tiers']
}) {
  const economics = calculateBlindBoxEconomics(props.tiers, props.priceUSD)
  const metrics = [
    [
      '当前模拟余额',
      formatUsdAmount(quotaUnitsToUsd(props.stats.balanceQuota)),
    ],
    ['累计投入', formatUsdAmount(quotaUnitsToUsd(props.stats.spentQuota))],
    ['累计额度奖励', formatUsdAmount(quotaUnitsToUsd(props.stats.rewardQuota))],
    ['账户回报率', `${props.stats.accountReturnRate.toFixed(1)}%`],
  ]
  return (
    <div className='border-t'>
      <div className='grid grid-cols-2 sm:grid-cols-4'>
        {metrics.map(([label, value]) => (
          <div
            key={label}
            className='border-border/70 px-4 py-3 not-last:border-r max-sm:nth-[2n]:border-r-0'
          >
            <p className='text-muted-foreground text-[11px]'>{label}</p>
            <p className='mt-1 text-sm font-semibold tabular-nums'>{value}</p>
          </div>
        ))}
      </div>
      <div className='bg-muted/30 text-muted-foreground border-t px-4 py-2 text-[11px] leading-5 sm:px-5'>
        账户回报率按开抽前后余额变化计算；累计返奖率为{' '}
        {props.stats.payoutRate.toFixed(2)}%，普通奖池理论返奖率约{' '}
        {economics.payoutRate.toFixed(2)}
        %。理论值包含“再来一抽”，不含首抽与大小保底。
      </div>
    </div>
  )
}

function SimulationHistory(props: { history: SimulationHistoryItem[] }) {
  if (props.history.length === 0) {
    return (
      <div className='border-border flex min-h-44 flex-col items-center justify-center rounded-lg border border-dashed px-5 text-center'>
        <BarChart3 className='text-muted-foreground size-5' />
        <p className='mt-2 text-sm font-medium'>尚未开始抽取</p>
        <p className='text-muted-foreground mt-1 text-xs'>
          首批结果会在这里按最新顺序展示
        </p>
      </div>
    )
  }
  return (
    <div className='min-w-0'>
      <div className='mb-2 flex items-center justify-between gap-3'>
        <h3 className='text-sm font-medium'>最近模拟结果</h3>
        <span className='text-muted-foreground text-xs'>保留最近 40 条</span>
      </div>
      <div className='max-h-72 divide-y overflow-y-auto rounded-lg border'>
        {props.history.map((item) => (
          <div
            key={item.id}
            className='flex items-center justify-between gap-3 px-3 py-2.5'
          >
            <div className='min-w-0'>
              <p className='truncate text-sm font-medium'>
                {item.reward_title}
              </p>
              <p className='text-muted-foreground mt-0.5 text-[11px]'>
                第 {item.id} 抽 ·{' '}
                {guaranteeLabel(item.guarantee_type) ?? item.reward_tier}
              </p>
            </div>
            <span className='bg-muted shrink-0 rounded-md px-2 py-1 text-xs font-medium tabular-nums'>
              {item.reward_type === 'prop'
                ? '权益卡'
                : formatUsdAmount(item.reward_usd)}
            </span>
          </div>
        ))}
      </div>
    </div>
  )
}

function guaranteeLabel(type: string) {
  switch (type) {
    case 'first':
      return '首抽保底'
    case 'small':
      return '小保底'
    case 'big':
      return '大保底'
    default:
      return null
  }
}
