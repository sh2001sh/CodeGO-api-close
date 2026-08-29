import { useState } from 'react'
import { formatQuota } from '@/lib/format'
import { CountUp } from '@/components/count-up'
import { Progress } from '@/components/ui/progress'

function clampPercent(used: number, total: number) {
  if (total <= 0) return 0
  return Math.max(0, Math.min(100, Math.round((used / total) * 100)))
}

type UsagePoint = {
  label: string
  value: number
}

export function UsageChart(props: { points: UsagePoint[] }) {
  const [hoveredIndex, setHoveredIndex] = useState<number | null>(null)
  const maxValue = Math.max(...props.points.map((point) => point.value), 1)
  const peakPoint =
    props.points.reduce<UsagePoint | null>((peak, point) => {
      if (!peak || point.value > peak.value) return point
      return peak
    }, null) ?? null
  const averageValue =
    props.points.length > 0
      ? props.points.reduce((sum, point) => sum + point.value, 0) /
        props.points.length
      : 0

  const displayPoint = hoveredIndex !== null ? props.points[hoveredIndex] : props.points.at(-1)

  return (
    <div className='codego-panel overview-panel-backdrop p-5 sm:p-6'>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <div className='min-w-0 flex-1'>
          <span className='codego-kicker'>USAGE · ROLLING 12H</span>
          <div className='text-foreground mt-1.5 text-lg font-semibold'>
            用量走势
          </div>
        </div>
        <span className='codego-stat-label'>HOURLY</span>
      </div>

      <div className='codego-fact-row mt-5 grid grid-cols-3'>
        <DataMetric
          label={hoveredIndex !== null ? '悬停时段' : '当前时段'}
          value={formatQuota(displayPoint?.value ?? 0)}
          hint={displayPoint?.label}
        />
        <DataMetric
          label='峰值'
          value={formatQuota(peakPoint?.value ?? 0)}
          hint={peakPoint?.label}
        />
        <DataMetric
          label='均值'
          value={formatQuota(averageValue)}
        />
      </div>

      {props.points.length > 0 ? (
        <div
          className='relative mt-6'
          onMouseLeave={() => setHoveredIndex(null)}
        >
          <div className='grid h-[180px] grid-cols-12 items-end gap-2 sm:gap-2.5'>
            {props.points.map((point, index) => {
              const isPeak = peakPoint?.label === point.label && peakPoint.value === point.value
              const isCurrent = index === props.points.length - 1
              const isHovered = hoveredIndex === index
              const height = Math.max(4, Math.round((point.value / maxValue) * 100))

              return (
                <div
                  key={`${point.label}-${index}`}
                  className='group relative flex h-full flex-col justify-end'
                  onMouseEnter={() => setHoveredIndex(index)}
                >
                  <div
                    className={`
                      dawn-bar relative rounded-none transition-[opacity,background-color] duration-200
                      ${isHovered ? 'opacity-80' : ''}
                      ${isPeak
                        ? 'bg-primary'
                        : isCurrent
                          ? 'bg-primary/45'
                          : 'bg-foreground/16'
                      }
                    `}
                    style={{
                      height: `${height}%`,
                      animationDelay: `${index * 35}ms`,
                    }}
                  >
                    {isHovered && (
                      <div className='bg-popover border-border absolute -top-14 left-1/2 z-10 -translate-x-1/2 border px-3 py-2 whitespace-nowrap'>
                        <div className='text-popover-foreground text-xs font-semibold tabular-nums'>
                          {formatQuota(point.value)}
                        </div>
                        <div className='text-muted-foreground mt-0.5 font-mono text-[10px]'>
                          {point.label}
                        </div>
                      </div>
                    )}
                  </div>
                </div>
              )
            })}
          </div>
          <div className='bg-border/70 mt-0 h-px w-full' />
          <div className='mt-2 grid grid-cols-12 gap-2 sm:gap-2.5'>
            {props.points.map((point, index) => (
              <div
                key={`label-${point.label}-${index}`}
                className={`text-center font-mono text-[9px] tabular-nums ${
                  index === props.points.length - 1
                    ? 'text-primary'
                    : 'text-muted-foreground/60'
                }`}
              >
                {point.label}
              </div>
            ))}
          </div>
        </div>
      ) : (
        <div className='codego-empty mt-6 min-h-[180px] justify-center'>
          <span aria-hidden className='block h-8 w-px bg-border' />
          NO USAGE DATA · LAST 12H
        </div>
      )}
    </div>
  )
}

export function DataMetric(props: {
  label: string
  value: string
  hint?: string
  numeric?: number
  format?: (value: number) => string
}) {
  return (
    <div className='min-w-0 sm:px-5 sm:first:pl-0 sm:last:pr-0'>
      <div className='codego-stat-label'>{props.label}</div>
      <div className='text-foreground mt-2 text-2xl leading-none font-semibold tabular-nums'>
        {props.numeric != null && props.format ? (
          <CountUp value={props.numeric} format={props.format} />
        ) : (
          props.value
        )}
      </div>
      {props.hint ? (
        <div className='text-muted-foreground mt-1.5 font-mono text-[10px] tabular-nums'>
          {props.hint}
        </div>
      ) : null}
    </div>
  )
}

export function ProgressBlock(props: {
  label: string
  used: number
  total: number
  remainingLabel: string
  hint: string
  className?: string
}) {
  const percent = clampPercent(props.used, props.total)

  return (
    <div className='p-3'>
      <div className='flex items-center justify-between gap-3 text-sm'>
        <div className='text-foreground font-medium'>{props.label}</div>
        <div className='text-muted-foreground text-xs tabular-nums'>
          {props.remainingLabel}
        </div>
      </div>
      <div className='mt-3'>
        <Progress className={props.className} value={percent} />
      </div>
      <div className='text-muted-foreground mt-2 text-xs tabular-nums'>
        {props.hint}
      </div>
    </div>
  )
}
