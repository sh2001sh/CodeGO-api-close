import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'

export interface PricingMarketHighlightProps {
  totalCount: number
  freeCount: number
  visibleFreeCount: number
  activeGroupLabel?: string
  className?: string
}

function TinyMetric(props: {
  label: string
  value: string
}) {
  return (
    <div className='rounded-md border border-border/70 bg-background/75 p-4'>
      <div className='text-[11px] font-semibold tracking-[0.14em] text-muted-foreground'>
        {props.label}
      </div>
      <div className='mt-2 text-2xl font-semibold tabular-nums text-foreground'>
        {props.value}
      </div>
    </div>
  )
}

export function PricingMarketHighlight(props: PricingMarketHighlightProps) {
  const freeRatio =
    props.totalCount > 0
      ? Math.round((props.freeCount / props.totalCount) * 100)
      : 0

  return (
    <section
      className={cn(
        'overview-soft-card flex flex-col gap-4 p-4 sm:p-5',
        props.className
      )}
    >
      <div className='flex flex-wrap items-center gap-2'>
        <Badge className='gap-1.5 rounded-full bg-primary/12 px-2.5 text-primary hover:bg-primary/12'>
          当前分组概览
        </Badge>
        {props.activeGroupLabel && (
          <Badge variant='secondary' className='rounded-full px-2.5'>
            {props.activeGroupLabel}
          </Badge>
        )}
      </div>

      <div className='grid gap-3 md:grid-cols-3'>
        <TinyMetric
          label='当前可用'
          value={props.totalCount.toString()}
        />
        <TinyMetric
          label='免费模型'
          value={props.freeCount.toString()}
        />
        <TinyMetric
          label='筛选后免费'
          value={`${props.visibleFreeCount} · 免费率 ${freeRatio}%`}
        />
      </div>
    </section>
  )
}
