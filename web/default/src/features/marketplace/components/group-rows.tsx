import { useState } from 'react'
import { ChevronDown, ChevronRight, FileText, Radio } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import type { MarketplaceGroup } from '../types'
import { AddToRoutePoolButton } from './add-to-route-pool-button'
import {
  ChannelFeedbackButton,
  ChannelFeedbackSummary,
} from './channel-feedback'
import { GroupDetails } from './group-details'
import { GroupMetrics } from './group-metrics'
import {
  GroupModelResults,
  GroupModelVerificationReport,
} from './group-model-verification'
import { RecentRequestStrip } from './recent-request-strip'
import { MarketplaceStatusBadge } from './status-badge'
import { TokenBindPanel } from './token-bind-panel'

export function GroupMarketItem(props: {
  group: MarketplaceGroup
  open: boolean
  onToggle: () => void
  routePoolSelected: boolean
  routePoolBusy: boolean
  routePoolAdding: boolean
  onAddToRoutePool: () => void
  showRoutePoolAction: boolean
}) {
  const { t } = useTranslation()
  const group = props.group
  const [reportOpen, setReportOpen] = useState(false)
  const reportID = `model-report-${group.id}`

  return (
    <article className='border-border bg-card hover:border-primary/35 rounded-md border transition-colors'>
      <div className='px-3 py-3 sm:px-4'>
        <header className='grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 gap-y-2 xl:grid-cols-[minmax(280px,1.1fr)_minmax(420px,1fr)_auto]'>
          <div className='flex min-w-0 items-center gap-3'>
            <RankBadge group={group} />
            <div className='min-w-0 flex-1'>
              <div className='flex min-w-0 items-center gap-2'>
                <h4
                  className='truncate text-sm font-semibold sm:text-base'
                  title={group.system_display_name}
                >
                  {group.system_display_name}
                </h4>
                <MarketplaceStatusBadge status={group.lifecycle_status} />
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
            <Button
              variant='ghost'
              size='sm'
              className='size-8 px-0 sm:w-auto sm:px-3'
              onClick={props.onToggle}
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
        <div className='border-border bg-muted/15 border-t'>
          <GroupDetails group={group} />
          <div className='border-border border-t px-4 py-4 sm:px-5'>
            <div className='flex flex-wrap items-start justify-between gap-3'>
              <div>
                <h5 className='text-sm font-semibold'>{t('可用模型')}</h5>
                <p className='text-muted-foreground mt-0.5 text-xs'>
                  {t('展开查看完整模型及检测结论。')}
                </p>
              </div>
              <Button
                variant='outline'
                size='sm'
                onClick={() => setReportOpen((current) => !current)}
                aria-expanded={reportOpen}
                aria-controls={reportID}
              >
                <FileText />
                {reportOpen ? t('收起报告') : t('检测报告')}
              </Button>
            </div>
            <GroupModelResults group={group} />
            <div className='mt-4'>
              <TokenBindPanel groupId={group.id} compact />
            </div>
            <div className='mt-3 flex flex-wrap items-center justify-between gap-2'>
              <ChannelFeedbackSummary group={group} />
              <ChannelFeedbackButton group={group} />
            </div>
          </div>
          {reportOpen && (
            <div id={reportID}>
              <GroupModelVerificationReport group={group} />
            </div>
          )}
        </div>
      )}
    </article>
  )
}

function RankBadge({ group }: { group: MarketplaceGroup }) {
  const rank = group.observing ? 0 : group.rank
  return (
    <span
      className={cn(
        'inline-flex size-9 shrink-0 items-center justify-center rounded-md text-xs font-semibold tabular-nums',
        rank === 1 && 'bg-amber-500/15 text-amber-700 dark:text-amber-300',
        rank === 2 && 'bg-slate-500/15 text-slate-700 dark:text-slate-300',
        rank === 3 && 'bg-orange-500/15 text-orange-700 dark:text-orange-300',
        (rank === 0 || rank > 3) && 'bg-muted text-muted-foreground'
      )}
      aria-label={rank ? `#${rank}` : undefined}
    >
      {rank ? `#${rank}` : <Radio className='size-4' />}
    </span>
  )
}
