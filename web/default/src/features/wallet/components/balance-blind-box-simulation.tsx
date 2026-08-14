import { FlaskConical } from 'lucide-react'
import type { BalanceBlindBoxSimulationOverview } from '../types'

export function BalanceBoxSimulationSummary(props: {
  simulation?: BalanceBlindBoxSimulationOverview
}) {
  const simulation = props.simulation
  if (!simulation?.active) return null

  return (
    <div className='flex flex-col gap-3 rounded-lg border border-teal-500/25 bg-teal-500/[0.06] p-3 sm:flex-row sm:items-center sm:justify-between'>
      <div className='flex min-w-0 gap-2.5'>
        <FlaskConical className='mt-0.5 size-4 shrink-0 text-teal-700 dark:text-teal-300' />
        <div className='min-w-0'>
          <div className='text-sm font-medium'>限时模拟已开启</div>
          <div className='text-muted-foreground mt-0.5 text-xs leading-5'>
            有效至 {formatSimulationTime(simulation.expires_at)}
            。模拟结果不会扣款、到账、生成道具或影响真实保底。
          </div>
        </div>
      </div>
      <div className='grid shrink-0 grid-cols-3 gap-3 text-right text-xs tabular-nums'>
        <SimulationMetric
          label='已模拟'
          value={`${simulation.draw_count} 抽`}
        />
        <SimulationMetric
          label='奖励价值'
          value={`$${simulation.simulated_reward_value_usd.toFixed(2)}`}
        />
        <SimulationMetric
          label='模拟净值'
          value={`${simulation.simulated_net_usd >= 0 ? '+' : ''}$${simulation.simulated_net_usd.toFixed(2)}`}
        />
      </div>
    </div>
  )
}

function SimulationMetric(props: { label: string; value: string }) {
  return (
    <div>
      <div className='text-muted-foreground'>{props.label}</div>
      <div className='text-foreground mt-0.5 font-medium'>{props.value}</div>
    </div>
  )
}

function formatSimulationTime(timestamp?: number) {
  if (!timestamp) return '-'
  return new Date(timestamp * 1000).toLocaleString()
}
