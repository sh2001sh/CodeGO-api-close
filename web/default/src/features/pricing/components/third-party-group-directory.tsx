import { Network, RefreshCw, Route, ShieldCheck } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import type { MarketplaceGroup } from '@/features/marketplace/types'

export function ThirdPartyGroupDirectory(props: {
  groups: MarketplaceGroup[]
  loading: boolean
  error: boolean
  onRetry: () => void
  onConfigureAuto: () => void
}) {
  const { t } = useTranslation()
  if (props.loading) return <DirectorySkeleton />
  if (props.error) {
    return (
      <div className='border-border flex min-h-64 flex-col items-center justify-center rounded-lg border px-6 text-center'>
        <ShieldCheck className='text-destructive size-6' />
        <h2 className='mt-3 font-semibold'>{t('第三方分组加载失败')}</h2>
        <p className='text-muted-foreground mt-1 text-sm'>
          {t('当前无法读取渠道状态，请稍后重新获取。')}
        </p>
        <Button
          className='mt-4 gap-2'
          variant='outline'
          onClick={props.onRetry}
        >
          <RefreshCw className='size-4' />
          {t('重新获取')}
        </Button>
      </div>
    )
  }
  if (props.groups.length === 0) {
    return (
      <div className='border-border flex min-h-64 flex-col items-center justify-center rounded-lg border px-6 text-center'>
        <Network className='text-muted-foreground size-6' />
        <h2 className='mt-3 font-semibold'>{t('暂无可用第三方分组')}</h2>
        <p className='text-muted-foreground mt-1 text-sm'>
          {t('通过模型检测并上架的第三方渠道会显示在这里。')}
        </p>
      </div>
    )
  }

  return (
    <section className='border-border overflow-hidden rounded-lg border'>
      <div className='bg-muted/25 flex flex-col gap-4 border-b px-4 py-4 sm:flex-row sm:items-center sm:justify-between sm:px-5'>
        <div>
          <h2 className='font-semibold'>{t('第三方渠道分组')}</h2>
          <p className='text-muted-foreground mt-1 text-sm'>
            {t(
              '第三方分组统一使用通用额度，倍率和可用率由各渠道实时表现决定。'
            )}
          </p>
        </div>
        <Button
          className='gap-2 self-start sm:self-auto'
          onClick={props.onConfigureAuto}
        >
          <Route className='size-4' />
          {t('配置 Auto 路由池')}
        </Button>
      </div>
      <div className='text-muted-foreground hidden grid-cols-[minmax(240px,1.5fr)_minmax(130px,0.7fr)_120px_120px_110px] gap-4 border-b px-5 py-2.5 text-xs font-medium lg:grid'>
        <span>{t('分组与模型')}</span>
        <span>{t('来源')}</span>
        <span className='text-right'>{t('可用率')}</span>
        <span className='text-right'>{t('倍率')}</span>
        <span className='text-right'>{t('状态')}</span>
      </div>
      <div className='divide-border divide-y'>
        {props.groups.map((group) => (
          <ThirdPartyGroupRow key={group.id} group={group} />
        ))}
      </div>
    </section>
  )
}

function ThirdPartyGroupRow({ group }: { group: MarketplaceGroup }) {
  const { t } = useTranslation()
  const availability = group.observing
    ? group.success_rate
    : group.wilson_success_rate || group.success_rate
  return (
    <div className='grid gap-3 px-4 py-4 lg:grid-cols-[minmax(240px,1.5fr)_minmax(130px,0.7fr)_120px_120px_110px] lg:items-center lg:gap-4 lg:px-5'>
      <div className='min-w-0'>
        <div className='truncate font-medium'>{group.system_display_name}</div>
        <div className='mt-2 flex flex-wrap gap-1.5'>
          {group.models.slice(0, 3).map((model) => (
            <Badge
              key={model}
              variant='secondary'
              className='max-w-44 truncate'
            >
              {model}
            </Badge>
          ))}
          {group.models.length > 3 && (
            <Badge variant='outline'>+{group.models.length - 3}</Badge>
          )}
        </div>
      </div>
      <div className='text-muted-foreground text-sm'>
        {group.source_label || t('来源待审核')}
      </div>
      <Metric label={t('可用率')} value={`${availability.toFixed(1)}%`} />
      <Metric label={t('倍率')} value={`${group.multiplier}x`} />
      <div className='flex justify-end lg:block lg:text-right'>
        <Badge
          variant='outline'
          className={
            group.lifecycle_status === 'active'
              ? 'border-success/30 bg-success/10 text-success'
              : 'border-warning/30 bg-warning/10 text-warning'
          }
        >
          {group.lifecycle_status === 'active' ? t('可用') : t('降级')}
        </Badge>
      </div>
    </div>
  )
}

function Metric(props: { label: string; value: string }) {
  return (
    <div className='flex items-center justify-between gap-4 lg:block lg:text-right'>
      <span className='text-muted-foreground text-xs lg:hidden'>
        {props.label}
      </span>
      <span className='font-semibold tabular-nums'>{props.value}</span>
    </div>
  )
}

function DirectorySkeleton() {
  return (
    <div className='border-border space-y-2 rounded-lg border p-4'>
      <Skeleton className='h-16 w-full rounded-md' />
      {Array.from({ length: 5 }).map((_, index) => (
        <Skeleton key={index} className='h-20 w-full rounded-md' />
      ))}
    </div>
  )
}
