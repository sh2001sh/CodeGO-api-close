import { lazy, Suspense, useState } from 'react'
import { ChevronDown, ChevronRight, Percent, Radio, Shrink } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import type { MarketplaceGroup } from '../types'
import { AddToRoutePoolButton } from './add-to-route-pool-button'
import { GroupMetrics } from './group-metrics'
import { RecentRequestStrip } from './recent-request-strip'
import { MarketplaceStatusBadge } from './status-badge'
import { BargainProposalDialog } from './bargain-proposal-dialog'

const loadGroupMarketItemDetails = () => import('./group-market-item-details')
const GroupMarketItemDetails = lazy(async () => ({
  default: (await loadGroupMarketItemDetails()).GroupMarketItemDetails,
}))

type GroupMarketItemProps = {
  group: MarketplaceGroup
  open: boolean
  onToggle: () => void
  routePoolSelected: boolean
  routePoolBusy: boolean
  routePoolAdding: boolean
  onAddToRoutePool: () => void
  showRoutePoolAction: boolean
  selectable?: boolean
  selected?: boolean
  selectionDisabled?: boolean
  onSelect?: () => void
}

export function GroupMarketItem(props: GroupMarketItemProps) {
  const { t } = useTranslation()
  const group = props.group
  const [bargainOpen, setBargainOpen] = useState(false)

  return (
    <article className='marketplace-render-row border-border bg-card hover:border-primary/35 rounded-md border transition-colors'>
      <div className='px-3 py-3 sm:px-4'>
        <header className='grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 gap-y-2 xl:grid-cols-[minmax(280px,1.1fr)_minmax(420px,1fr)_auto]'>
          <div className='flex min-w-0 items-center gap-3'>
            <div className='flex items-center gap-2'>
              {props.selectable && (
                <input
                  type='checkbox'
                  checked={props.selected}
                  disabled={props.selectionDisabled}
                  title={
                    props.selectionDisabled
                      ? t('单次最多选择 5 个分组')
                      : undefined
                  }
                  onChange={props.onSelect}
                  aria-label={t('选择 {{name}} 进行批量测试', {
                    name: group.system_display_name,
                  })}
                  className='accent-primary size-4 shrink-0'
                />
              )}
              <RankBadge group={group} />
            </div>
            <div className='min-w-0 flex-1'>
              <div className='flex min-w-0 items-center gap-2'>
                <h4
                  className='truncate text-sm font-semibold sm:text-base'
                  title={group.system_display_name}
                >
                  {group.system_display_name}
                </h4>
                <MarketplaceStatusBadge status={group.lifecycle_status} />
                <RemoteCompactionBadge
                  support={group.remote_compaction_support}
                />
              </div>
              <div className='text-muted-foreground mt-1 flex min-w-0 items-center gap-2 text-xs'>
                {group.source_label && (
                  <span className='truncate'>{group.source_label}</span>
                )}
                {group.source_label && <span aria-hidden='true'>·</span>}
                <span className='shrink-0'>{group.provider_type}</span>
                <span aria-hidden='true'>·</span>
                <span className='shrink-0'>
                  {t('{{count}} 个模型', { count: group.models.length })}
                </span>
              </div>
              <ModelPreview models={group.models} />
            </div>
          </div>
          <div className='col-span-2 xl:col-span-1'>
            <GroupMetrics group={group} />
          </div>
          <div className='col-start-2 row-start-1 flex items-center justify-end gap-2 xl:col-start-3'>
            {props.showRoutePoolAction && (
              <AddToRoutePoolButton
                groupName={group.system_display_name}
                selected={props.routePoolSelected}
                busy={props.routePoolBusy}
                adding={props.routePoolAdding}
                onAdd={props.onAddToRoutePool}
              />
            )}
            <Button variant='ghost' size='icon' title={t('申请倍率议价')} onClick={() => setBargainOpen(true)}><Percent /></Button>
            <Button
              variant='ghost'
              size='sm'
              className='size-8 px-0 sm:w-auto sm:px-3'
              onClick={props.onToggle}
              onPointerEnter={() => void loadGroupMarketItemDetails()}
              onFocus={() => void loadGroupMarketItemDetails()}
              aria-label={props.open ? t('收起详情') : t('展开详情')}
              title={props.open ? t('收起详情') : t('展开详情')}
            >
              <span className='hidden sm:inline'>
                {props.open ? t('收起') : t('更多')}
              </span>
              {props.open ? <ChevronDown /> : <ChevronRight />}
            </Button>
          </div>
        </header>
        <RecentRequestStrip group={group} />
      </div>
      {props.open && (
        <Suspense fallback={<GroupDetailsSkeleton />}>
          <GroupMarketItemDetails group={group} />
        </Suspense>
      )}
      <BargainProposalDialog group={group} open={bargainOpen} onOpenChange={setBargainOpen} />
    </article>
  )
}

function GroupDetailsSkeleton() {
  return (
    <div className='border-border bg-muted/15 space-y-4 border-t px-4 py-5 sm:px-5'>
      <Skeleton className='h-20 w-full' />
      <Skeleton className='h-28 w-full' />
    </div>
  )
}

function ModelPreview(props: { models: string[] }) {
  const visibleModels = props.models.slice(0, 3)
  const remaining = Math.max(0, props.models.length - visibleModels.length)
  if (visibleModels.length === 0) return null

  return (
    <div className='codego-marketplace-model-preview' aria-label='可用模型'>
      {visibleModels.map((model) => (
        <span key={model} title={model}>
          {model}
        </span>
      ))}
      {remaining > 0 && <span>+{remaining}</span>}
    </div>
  )
}

function RemoteCompactionBadge(props: {
  support: MarketplaceGroup['remote_compaction_support']
}) {
  const { t } = useTranslation()
  if (!props.support) return null
  const label =
    props.support === 'v1'
      ? t('远程压缩 · 仅 v1')
      : props.support === 'v1_v2'
        ? t('远程压缩 · v1 + v2')
        : t('远程压缩 · 仅 v2')
  return (
    <span
      className='border-border/70 text-muted-foreground inline-flex shrink-0 items-center gap-1 rounded-[4px] border px-1.5 py-0.5 text-[11px] font-medium'
      title={t('该分组已通过对应远程压缩协议探测')}
    >
      <Shrink className='size-3' aria-hidden='true' />
      <span>{label}</span>
    </span>
  )
}

function RankBadge({ group }: { group: MarketplaceGroup }) {
  const rank = group.observing ? 0 : group.rank
  const top3 = rank >= 1 && rank <= 3
  return (
    <span
      className={cn(
        'inline-flex size-9 shrink-0 items-center justify-center rounded-[4px] border text-xs font-semibold tabular-nums transition-colors',
        top3
          ? 'border-primary/45 text-primary bg-primary/[0.06]'
          : 'border-border/70 text-muted-foreground bg-transparent'
      )}
      aria-label={rank ? `#${rank}` : undefined}
    >
      {rank ? `#${rank}` : <Radio className='size-3.5' />}
    </span>
  )
}
