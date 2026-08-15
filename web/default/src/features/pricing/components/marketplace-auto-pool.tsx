import { useMemo, useState } from 'react'
import { Link } from '@tanstack/react-router'
import { Check, LoaderCircle, Route, Search, ShieldCheck } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import {
  useMarketplaceAutoRoutePool,
  useMarketplaceAutoRoutePoolUpdate,
} from '@/features/marketplace/hooks'
import type { MarketplaceAutoRoutePoolItem } from '@/features/marketplace/types'

export function MarketplaceAutoPool(props: { authenticated: boolean }) {
  const { t } = useTranslation()
  const query = useMarketplaceAutoRoutePool(props.authenticated)
  const update = useMarketplaceAutoRoutePoolUpdate()
  const [selectedDraft, setSelectedDraft] = useState<Set<string> | null>(null)
  const [search, setSearch] = useState('')
  const serverSelected = useMemo(
    () =>
      new Set(
        (query.data?.items ?? [])
          .filter((item) => item.selected)
          .map((item) => item.group_id)
      ),
    [query.data?.items]
  )
  const selected = selectedDraft ?? serverSelected

  const visibleItems = useMemo(() => {
    const keyword = search.trim().toLowerCase()
    const items = query.data?.items ?? []
    if (!keyword) return items
    return items.filter((item) =>
      [item.system_display_name, item.source_label, ...item.models]
        .join(' ')
        .toLowerCase()
        .includes(keyword)
    )
  }, [query.data?.items, search])

  const routeOrder = useMemo(() => {
    return (query.data?.items ?? [])
      .filter((item) => selected.has(item.group_id))
      .sort((left, right) => left.route_score - right.route_score)
  }, [query.data?.items, selected])

  if (!props.authenticated) return <SignInRequired />
  if (query.isLoading) return <AutoPoolSkeleton />
  if (query.isError || !query.data) {
    return (
      <div className='border-border flex min-h-64 flex-col items-center justify-center rounded-lg border px-6 text-center'>
        <ShieldCheck className='text-destructive size-6' />
        <h2 className='mt-3 font-semibold'>{t('无法读取 Auto 路由池')}</h2>
        <Button
          className='mt-4'
          variant='outline'
          onClick={() => query.refetch()}
        >
          {t('重新获取')}
        </Button>
      </div>
    )
  }

  const toggle = (groupID: string, checked: boolean) => {
    const next = new Set(selected)
    if (checked) next.add(groupID)
    else next.delete(groupID)
    setSelectedDraft(next)
  }

  const save = async () => {
    try {
      await update.mutateAsync(Array.from(selected))
      setSelectedDraft(null)
      toast.success(t('第三方 Auto 路由池已保存'))
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('路由池保存失败'))
    }
  }

  return (
    <section className='border-border overflow-hidden rounded-lg border'>
      <div className='bg-muted/25 grid gap-5 border-b px-4 py-5 lg:grid-cols-[minmax(0,1fr)_minmax(280px,0.55fr)] lg:px-5'>
        <div>
          <div className='flex items-center gap-2'>
            <Route className='text-primary size-5' />
            <h2 className='font-semibold'>{t('我的第三方 Auto 路由池')}</h2>
          </div>
          <p className='text-muted-foreground mt-2 max-w-2xl text-sm leading-6'>
            {t(
              '只在你选择的第三方分组中路由。系统先比较保守可用率，再在稳定性接近时优先低倍率分组；所有调用均使用通用额度。'
            )}
          </p>
          <div className='mt-4 flex flex-wrap gap-2'>
            <Badge variant='outline'>
              {t('分组')}: {selected.size}
            </Badge>
            <Badge variant='outline'>{t('Token 分组')}: market:auto</Badge>
            <Badge variant='outline'>
              {t('额度')}: {t('通用额度')}
            </Badge>
          </div>
        </div>
        <RouteOrder items={routeOrder} />
      </div>

      <div className='flex flex-col gap-3 border-b px-4 py-3 sm:flex-row sm:items-center sm:justify-between lg:px-5'>
        <div className='relative w-full sm:max-w-sm'>
          <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2' />
          <Input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder={t('搜索分组、来源或模型')}
            className='pl-9'
          />
        </div>
        <Button onClick={save} disabled={update.isPending} className='gap-2'>
          {update.isPending ? (
            <LoaderCircle className='size-4 animate-spin' />
          ) : (
            <Check className='size-4' />
          )}
          {t('保存路由池')}
        </Button>
      </div>

      {visibleItems.length === 0 ? (
        <div className='text-muted-foreground px-5 py-16 text-center text-sm'>
          {t('没有匹配的第三方分组。')}
        </div>
      ) : (
        <div className='divide-border divide-y'>
          {visibleItems.map((item) => (
            <AutoPoolRow
              key={item.group_id}
              item={item}
              selected={selected.has(item.group_id)}
              order={
                routeOrder.findIndex(
                  (entry) => entry.group_id === item.group_id
                ) + 1
              }
              onToggle={(checked) => toggle(item.group_id, checked)}
            />
          ))}
        </div>
      )}
    </section>
  )
}

function AutoPoolRow(props: {
  item: MarketplaceAutoRoutePoolItem
  selected: boolean
  order: number
  onToggle: (checked: boolean) => void
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
          {item.source_label || t('来源待审核')} ·{' '}
          {item.models.slice(0, 3).join(' / ')}
        </div>
      </div>
      <Metric
        label={t('路由可用率')}
        value={`${item.availability.toFixed(1)}%`}
      />
      <Metric label={t('倍率')} value={`${item.multiplier}x`} />
      <Metric label={t('路由评分')} value={item.route_score.toFixed(2)} />
    </label>
  )
}

function RouteOrder({ items }: { items: MarketplaceAutoRoutePoolItem[] }) {
  const { t } = useTranslation()
  return (
    <div className='border-border bg-background rounded-lg border px-4 py-3'>
      <div className='text-sm font-medium'>{t('当前路由顺序')}</div>
      {items.length === 0 ? (
        <p className='text-muted-foreground mt-2 text-xs leading-5'>
          {t('选择至少一个分组后即可创建第三方 Auto API Key。')}
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

function SignInRequired() {
  const { t } = useTranslation()
  return (
    <div className='border-border flex min-h-72 flex-col items-center justify-center rounded-lg border px-6 text-center'>
      <Route className='text-primary size-7' />
      <h2 className='mt-4 font-semibold'>{t('登录后配置第三方 Auto')}</h2>
      <p className='text-muted-foreground mt-2 max-w-lg text-sm leading-6'>
        {t(
          '路由池属于个人配置。登录后选择需要使用的第三方分组，再在创建 API Key 时选择“第三方 Auto”。'
        )}
      </p>
      <Button
        className='mt-5'
        render={<Link to='/sign-in' search={{ redirect: '/pricing' }} />}
      >
        {t('登录并配置')}
      </Button>
    </div>
  )
}

function AutoPoolSkeleton() {
  return (
    <div className='border-border space-y-2 rounded-lg border p-4'>
      <Skeleton className='h-32 w-full rounded-md' />
      {Array.from({ length: 5 }).map((_, index) => (
        <Skeleton key={index} className='h-20 w-full rounded-md' />
      ))}
    </div>
  )
}
