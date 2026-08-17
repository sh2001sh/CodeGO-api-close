import { ArrowDown, ArrowUp } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Skeleton } from '@/components/ui/skeleton'
import type { MarketplaceAutoRoutePoolItem } from '@/features/marketplace/types'

export function AutoPoolRow(props: {
  item: MarketplaceAutoRoutePoolItem
  selected: boolean
  order: number
  onToggle: (checked: boolean) => void
  onMoveUp: () => void
  onMoveDown: () => void
  canMoveUp: boolean
  canMoveDown: boolean
}) {
  const { t } = useTranslation()
  const item = props.item
  return (
    <label className='hover:bg-muted/20 grid cursor-pointer gap-3 px-4 py-4 transition-colors lg:grid-cols-[28px_minmax(240px,1.5fr)_minmax(130px,0.7fr)_110px_110px] lg:items-center lg:px-5'>
      <Checkbox checked={props.selected} onCheckedChange={props.onToggle} />
      <div className='min-w-0'>
        <div className='flex items-center gap-2'>
          {props.order > 0 && (
            <span className='bg-primary/10 text-primary inline-flex size-6 items-center justify-center rounded-md text-xs font-semibold tabular-nums'>
              {props.order}
            </span>
          )}
          <span className='truncate font-medium'>
            {item.system_display_name}
          </span>
        </div>
        <div className='text-muted-foreground mt-1 truncate text-xs'>
          {item.source_type === 'official' ? t('官方分组') : item.source_label || t('来源待审核')} ·{' '}
          {item.models.slice(0, 3).join(' / ')}
        </div>
      </div>
      <Metric
        label={t('路由可用率')}
        value={`${item.availability.toFixed(1)}%`}
      />
      <Metric label={t('倍率')} value={`${item.multiplier}x`} />
      <Metric label={t('路由评分')} value={item.route_score.toFixed(2)} />
      {props.selected && (
        <div className='flex justify-end gap-1 lg:col-span-5'>
          <PriorityButton
            label={t('提高路由优先级')}
            disabled={!props.canMoveUp}
            onClick={props.onMoveUp}
            icon={ArrowUp}
          />
          <PriorityButton
            label={t('降低路由优先级')}
            disabled={!props.canMoveDown}
            onClick={props.onMoveDown}
            icon={ArrowDown}
          />
        </div>
      )}
    </label>
  )
}

function PriorityButton(props: {
  label: string
  disabled: boolean
  onClick: () => void
  icon: typeof ArrowUp
}) {
  const Icon = props.icon
  return (
    <Button
      type='button'
      size='icon-sm'
      variant='ghost'
      disabled={props.disabled}
      onClick={(event) => {
        event.preventDefault()
        props.onClick()
      }}
      title={props.label}
    >
      <Icon className='size-4' />
    </Button>
  )
}

export function RouteOrder({
  items,
}: {
  items: MarketplaceAutoRoutePoolItem[]
}) {
  const { t } = useTranslation()
  return (
    <div className='border-border bg-background rounded-lg border px-4 py-3'>
      <div className='text-sm font-medium'>{t('当前路由顺序')}</div>
      {items.length === 0 ? (
        <p className='text-muted-foreground mt-2 text-xs leading-5'>
          {t('选择至少一个分组后即可使用 Auto API Key。')}
        </p>
      ) : (
        <ol className='mt-2 space-y-1.5'>
          {items.slice(0, 4).map((item, index) => (
            <li key={item.group_id} className='flex items-center gap-2 text-xs'>
              <span className='text-muted-foreground w-4 tabular-nums'>
                {index + 1}
              </span>
              <span className='min-w-0 flex-1 truncate'>
                {item.system_display_name}
              </span>
              <span className='text-muted-foreground tabular-nums'>
                {item.multiplier}x
              </span>
            </li>
          ))}
        </ol>
      )}
    </div>
  )
}

function Metric(props: { label: string; value: string }) {
  return (
    <div className='flex items-center justify-between gap-4 lg:block lg:text-right'>
      <span className='text-muted-foreground text-xs lg:block'>
        {props.label}
      </span>
      <span className='mt-0.5 font-semibold tabular-nums'>{props.value}</span>
    </div>
  )
}

export function AutoPoolSkeleton() {
  return (
    <div className='border-border space-y-2 rounded-lg border p-4'>
      <Skeleton className='h-32 w-full rounded-md' />
      {Array.from({ length: 5 }).map((_, index) => (
        <Skeleton key={index} className='h-20 w-full rounded-md' />
      ))}
    </div>
  )
}
