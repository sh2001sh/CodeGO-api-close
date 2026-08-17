import { ChevronDown, ChevronRight, Radio } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import type { MarketplaceGroup } from '../types'
import {
  ChannelFeedbackButton,
  ChannelFeedbackSummary,
} from './channel-feedback'
import { GroupDetails } from './group-details'
import { GroupMetrics } from './group-metrics'
import { ModelConsistencyBadge } from './model-verification'
import { MarketplaceStatusBadge } from './status-badge'
import { TokenBindPanel } from './token-bind-panel'

export function GroupMarketItem(props: {
  group: MarketplaceGroup
  open: boolean
  onToggle: () => void
}) {
  const { t } = useTranslation()
  const group = props.group

  return (
    <article className='border-border bg-card hover:border-primary/30 rounded-md border transition-colors'>
      <div className='px-4 py-5 sm:px-5'>
        <header className='flex flex-col gap-4 xl:flex-row xl:items-start xl:justify-between'>
          <div className='flex min-w-0 items-start gap-3.5'>
            <RankBadge group={group} />
            <div className='min-w-0 flex-1'>
              <div className='flex flex-wrap items-center gap-2'>
                <h4 className='text-base font-semibold text-balance'>
                  {group.system_display_name}
                </h4>
                <MarketplaceStatusBadge status={group.lifecycle_status} />
                <ModelConsistencyBadge
                  status={group.model_consistency_status}
                />
              </div>
              <div className='text-muted-foreground mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs'>
                {group.source_label && (
                  <>
                    <span>{group.source_label}</span>
                    <span aria-hidden='true'>·</span>
                  </>
                )}
                <span>{group.provider_type}</span>
                <span aria-hidden='true'>·</span>
                <span>
                  {t('{{count}} 个模型', { count: group.models.length })}
                </span>
              </div>
              <div className='mt-2.5 flex flex-wrap gap-1.5'>
                <ModelPreview models={group.models} />
              </div>
            </div>
          </div>
          <div className='flex flex-wrap items-center gap-2 xl:justify-end'>
            <ChannelFeedbackButton group={group} />
            <Button
              variant='ghost'
              size='icon'
              onClick={props.onToggle}
              aria-label={props.open ? t('收起详情') : t('展开详情')}
              title={props.open ? t('收起详情') : t('展开详情')}
            >
              {props.open ? <ChevronDown /> : <ChevronRight />}
            </Button>
          </div>
        </header>

        <GroupMetrics group={group} />
        <div className='mt-3'>
          <TokenBindPanel groupId={group.id} compact />
        </div>

        <div className='mt-3 flex flex-wrap items-center justify-between gap-2'>
          <ChannelFeedbackSummary group={group} />
          {group.observing && (
            <span className='text-muted-foreground text-xs'>
              {t('样本仍在积累 · 已记录 {{requests}} 次请求', {
                requests: group.request_count,
              })}
            </span>
          )}
        </div>
      </div>
      {props.open && (
        <div className='border-border bg-muted/15 border-t'>
          <GroupDetails group={group} />
        </div>
      )}
    </article>
  )
}

function ModelPreview({ models }: { models: string[] }) {
  const visible = models.slice(0, 4)
  const remaining = models.length - visible.length
  return (
    <>
      {visible.map((model) => (
        <Badge key={model} variant='secondary' className='max-w-52 truncate'>
          {model}
        </Badge>
      ))}
      {remaining > 0 && <Badge variant='outline'>+{remaining}</Badge>}
    </>
  )
}

function RankBadge({ group }: { group: MarketplaceGroup }) {
  const rank = group.observing ? 0 : group.rank
  return (
    <span
      className={cn(
        'inline-flex size-10 shrink-0 items-center justify-center rounded-md text-sm font-semibold tabular-nums',
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
