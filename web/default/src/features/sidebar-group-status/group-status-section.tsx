import { useId } from 'react'
import { ChevronDown, ChevronRight, ListPlus } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { GroupStatusMonitorCard } from './group-status-monitor-card'
import { getStatusMeta } from './presentation'
import type { SidebarGroupStatusItem } from './types'

/** Shows model health by default while retaining per-group collapse controls. */
export function GroupStatusSection(props: {
  group: SidebarGroupStatusItem
  expanded: boolean
  onToggle: () => void
  poolName?: string
  inCurrentPool: boolean
  joining: boolean
  onJoinCurrentPool: () => void
}) {
  const contentId = useId()
  const group = props.group
  const status = getStatusMeta(group.status)

  return (
    <section className='group-status-render-section app-page-shell p-4'>
      <div
        className={cn(
          'flex flex-col items-stretch justify-between gap-3 sm:flex-row sm:items-center',
          props.expanded && 'mb-4'
        )}
      >
        <div className='min-w-0 space-y-1'>
          <div className='flex min-w-0 items-center gap-2'>
            <span className={cn('size-2 shrink-0 rounded-full', status.dot)} />
            <h3
              className='text-foreground truncate text-lg font-semibold'
              title={group.display_name || group.group}
            >
              {group.display_name || group.group}
            </h3>
            <span
              className={cn(
                'shrink-0 rounded-[4px] px-2 py-0.5 text-[10px] font-semibold',
                status.accentText,
                status.badgeBg
              )}
            >
              {status.label}
            </span>
          </div>
          <p className='text-muted-foreground truncate text-sm'>
            {(group.source_type ?? 'official') === 'marketplace_user'
              ? '第三方渠道 · 套餐与余额'
              : '官方渠道'}{' '}
            · {group.models.length} 个模型
          </p>
        </div>
        <div className='flex shrink-0 flex-wrap items-center gap-2 sm:gap-3'>
          {typeof group.request_count === 'number' &&
            typeof group.success_rate === 'number' &&
            group.request_count > 0 && (
              <div className='border-border bg-muted/30 order-3 w-full rounded-md border px-3 py-2 text-xs sm:order-none sm:w-auto sm:border-0 sm:bg-transparent sm:p-0 sm:text-right'>
                <div className='text-muted-foreground'>近 6 小时请求</div>
                <div className='mt-0.5 font-semibold tabular-nums'>
                  成功 {Math.round((group.request_count * group.success_rate) / 100).toLocaleString()}
                  {' · '}失败{' '}
                  {Math.max(
                    0,
                    group.request_count -
                      Math.round((group.request_count * group.success_rate) / 100)
                  ).toLocaleString()}
                </div>
              </div>
            )}
          <div className='hidden text-right sm:block'>
            <div className='text-muted-foreground text-xs'>缓存命中率</div>
            <div className='mt-0.5 text-base font-semibold tabular-nums'>
              {group.cache_hit_rate == null
                ? '--'
                : `${group.cache_hit_rate.toFixed(1)}%`}
            </div>
          </div>
          <Button
            variant='outline'
            size='sm'
            disabled={
              props.inCurrentPool || props.joining || !props.group.group_id
            }
            onClick={props.onJoinCurrentPool}
            title={props.poolName ? `当前路由池：${props.poolName}` : undefined}
          >
            <ListPlus className='size-3.5' />
            {props.inCurrentPool
              ? '已在当前池'
              : props.joining
                ? '加入中'
                : '加入当前池'}
          </Button>
          <Button
            variant='ghost'
            size='icon-sm'
            onClick={props.onToggle}
            aria-expanded={props.expanded}
            aria-controls={contentId}
            aria-label={props.expanded ? '收起模型' : '展开模型'}
            title={props.expanded ? '收起模型' : '展开模型'}
          >
            {props.expanded ? <ChevronDown /> : <ChevronRight />}
          </Button>
        </div>
      </div>

      {props.expanded && (
        <div
          id={contentId}
          className='grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-5'
        >
          {group.models.length === 0 ? (
            <div className='codego-empty px-4 py-6'>NO MODELS</div>
          ) : (
            group.models.map((model) => (
              <GroupStatusMonitorCard
                key={`${group.group}-${model.model}`}
                item={model}
              />
            ))
          )}
        </div>
      )}
    </section>
  )
}
