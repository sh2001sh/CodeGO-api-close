import { useState } from 'react'
import { ChevronDown, ChevronUp, RefreshCcw, ShieldCheck } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { useOfficialMarketplaceGroups } from '../hooks'
import { formatDuration, formatMultiplier } from '../lib/format'
import type { OfficialMarketplaceGroup } from '../types'

const collapsedGroupCount = 4

export function OfficialGroupsSection() {
  const { t } = useTranslation()
  const query = useOfficialMarketplaceGroups()
  const [expanded, setExpanded] = useState(false)
  const groups = query.data?.items ?? []
  const visibleGroups = expanded ? groups : groups.slice(0, collapsedGroupCount)

  return (
    <section className='border-primary/20 bg-primary/[0.025] overflow-hidden rounded-lg border'>
      <header className='flex flex-wrap items-center justify-between gap-3 px-4 py-3 sm:px-5'>
        <div className='flex min-w-0 items-start gap-2.5'>
          <div className='bg-primary/10 text-primary mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-md'>
            <ShieldCheck className='size-4' aria-hidden='true' />
          </div>
          <div className='min-w-0'>
            <div className='flex flex-wrap items-center gap-2'>
              <h3 className='text-sm font-semibold'>{t('官方分组')}</h3>
              <Badge variant='secondary'>{t('平台运营')}</Badge>
            </div>
            <p className='text-muted-foreground mt-0.5 text-xs leading-5'>
              {t('平台维护的稳定线路，独立于下方第三方市场展示。')}
            </p>
          </div>
        </div>
        {groups.length > collapsedGroupCount && (
          <Button
            variant='ghost'
            size='sm'
            onClick={() => setExpanded((current) => !current)}
            aria-expanded={expanded}
          >
            {expanded
              ? t('收起')
              : t('查看全部 {{count}} 个', { count: groups.length })}
            {expanded ? <ChevronUp /> : <ChevronDown />}
          </Button>
        )}
      </header>
      <OfficialGroupsContent
        groups={visibleGroups}
        loading={query.isLoading}
        error={query.isError}
        onRetry={() => void query.refetch()}
      />
    </section>
  )
}

function OfficialGroupsContent(props: {
  groups: OfficialMarketplaceGroup[]
  loading: boolean
  error: boolean
  onRetry: () => void
}) {
  const { t } = useTranslation()
  if (props.loading) return <OfficialGroupsSkeleton />
  if (props.error) {
    return (
      <div className='border-primary/15 flex min-h-24 items-center justify-between gap-3 border-t px-4 py-4 sm:px-5'>
        <p className='text-muted-foreground text-sm'>
          {t('官方分组暂时无法加载，第三方市场不受影响。')}
        </p>
        <Button variant='outline' size='sm' onClick={props.onRetry}>
          <RefreshCcw />
          {t('重试')}
        </Button>
      </div>
    )
  }
  if (props.groups.length === 0) {
    return (
      <div className='border-primary/15 text-muted-foreground border-t px-4 py-6 text-center text-sm sm:px-5'>
        {t('当前没有可用的官方分组。')}
      </div>
    )
  }
  return (
    <div className='border-primary/15 bg-border/70 grid gap-px border-t sm:grid-cols-2 xl:grid-cols-4'>
      {props.groups.map((group) => (
        <OfficialGroupItem key={group.group_id} group={group} />
      ))}
    </div>
  )
}

function OfficialGroupItem(props: { group: OfficialMarketplaceGroup }) {
  const { t } = useTranslation()
  const group = props.group
  const models = group.models.slice(0, 3)
  return (
    <article className='bg-card min-w-0 p-4'>
      <div className='flex items-start justify-between gap-3'>
        <div className='min-w-0'>
          <h4
            className='truncate text-sm font-semibold'
            title={group.system_display_name}
          >
            {group.system_display_name}
          </h4>
          <p className='text-muted-foreground mt-1 line-clamp-2 min-h-10 text-xs leading-5'>
            {group.description || t('平台官方线路')}
          </p>
        </div>
        <OfficialHealthStatus group={group} />
      </div>
      <div className='mt-3 grid grid-cols-2 gap-2 text-xs'>
        <Metric
          label={t('余额倍率')}
          value={`${formatMultiplier(group.multiplier)}x`}
        />
        <Metric
          label={t('套餐倍率')}
          value={
            group.subscription_enabled
              ? `${formatMultiplier(group.subscription_multiplier)}x`
              : t('不支持')
          }
        />
      </div>
      <div className='mt-3 flex min-h-6 flex-wrap gap-1.5'>
        {models.map((model) => (
          <span
            key={model}
            className='bg-muted text-muted-foreground max-w-32 truncate rounded px-1.5 py-1 text-[11px]'
            title={model}
          >
            {model}
          </span>
        ))}
        {group.models.length > models.length && (
          <span className='bg-muted text-muted-foreground rounded px-1.5 py-1 text-[11px]'>
            +{group.models.length - models.length}
          </span>
        )}
      </div>
      <p className='text-muted-foreground mt-3 text-[11px] tabular-nums'>
        {group.metrics_available
          ? t('成功率 {{rate}} · 平均延迟 {{latency}}', {
              rate: `${group.success_rate.toFixed(1)}%`,
              latency: formatDuration(group.avg_latency_ms),
            })
          : t('等待近期请求样本')}
      </p>
    </article>
  )
}

function Metric(props: { label: string; value: string }) {
  return (
    <div className='bg-muted/50 rounded-md px-2.5 py-2'>
      <div className='text-muted-foreground text-[11px]'>{props.label}</div>
      <div className='mt-0.5 font-semibold tabular-nums'>{props.value}</div>
    </div>
  )
}

function OfficialHealthStatus(props: { group: OfficialMarketplaceGroup }) {
  const { t } = useTranslation()
  const status = props.group.latest_request_status
  let label = t('待观测')
  if (status === 'healthy') label = t('正常')
  if (status === 'unstable') label = t('波动')
  if (status === 'failed') label = t('异常')
  return (
    <span className='text-muted-foreground inline-flex shrink-0 items-center gap-1.5 text-[11px]'>
      <span
        className={cn(
          'bg-muted-foreground/50 size-1.5 rounded-full',
          status === 'healthy' && 'bg-emerald-500',
          status === 'unstable' && 'bg-amber-500',
          status === 'failed' && 'bg-destructive'
        )}
        aria-hidden='true'
      />
      {label}
    </span>
  )
}

function OfficialGroupsSkeleton() {
  return (
    <div className='border-primary/15 bg-border/70 grid gap-px border-t sm:grid-cols-2 xl:grid-cols-4'>
      {Array.from({ length: collapsedGroupCount }).map((_, index) => (
        <div key={index} className='bg-card space-y-3 p-4'>
          <Skeleton className='h-4 w-28' />
          <Skeleton className='h-9 w-full' />
          <div className='grid grid-cols-2 gap-2'>
            <Skeleton className='h-12' />
            <Skeleton className='h-12' />
          </div>
        </div>
      ))}
    </div>
  )
}
