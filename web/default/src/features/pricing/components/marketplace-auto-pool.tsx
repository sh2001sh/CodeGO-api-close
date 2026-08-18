import { useMemo, useState } from 'react'
import { Link } from '@tanstack/react-router'
import { Check, LoaderCircle, Route, ShieldCheck } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  useMarketplaceAutoRoutePool,
  useMarketplaceAutoRoutePoolUpdate,
} from '@/features/marketplace/hooks'
import type { MarketplaceAutoRoutePoolItem } from '@/features/marketplace/types'
import {
  AutoPoolFilters,
  AutoPoolRow,
  AutoPoolSkeleton,
  type AutoPoolSort,
} from './marketplace-auto-pool-parts'

export function MarketplaceAutoPool(props: { authenticated: boolean }) {
  const { t } = useTranslation()
  const query = useMarketplaceAutoRoutePool(props.authenticated)
  const update = useMarketplaceAutoRoutePoolUpdate()
  const [selectedDraft, setSelectedDraft] = useState<string[] | null>(null)
  const [search, setSearch] = useState('')
  const [sourceFilter, setSourceFilter] = useState('all')
  const [modelFilter, setModelFilter] = useState('all')
  const [sortBy, setSortBy] = useState<AutoPoolSort>('route')
  const serverSelected = useMemo(
    () =>
      (query.data?.items ?? [])
        .filter((item) => item.selected)
        .sort((left, right) => left.priority - right.priority)
        .map((item) => item.group_id),
    [query.data?.items]
  )
  const selectedOrder = selectedDraft ?? serverSelected
  const selected = useMemo(() => new Set(selectedOrder), [selectedOrder])

  const availableItems = useMemo(() => {
    const keyword = search.trim().toLowerCase()
    const items = (query.data?.items ?? []).filter(
      (item) => !selected.has(item.group_id)
    )
    const filtered = items.filter((item) => {
      const matchesKeyword =
        !keyword ||
        [item.system_display_name, item.source_label, ...item.models]
          .join(' ')
          .toLowerCase()
          .includes(keyword)
      const source =
        item.source_type === 'official'
          ? '官方分组'
          : item.source_label || '来源待审核'
      const matchesSource = sourceFilter === 'all' || source === sourceFilter
      const matchesModel =
        modelFilter === 'all' || item.models.includes(modelFilter)
      return matchesKeyword && matchesSource && matchesModel
    })
    return [...filtered].sort((left, right) => {
      if (sortBy === 'multiplier') return left.multiplier - right.multiplier
      if (sortBy === 'success') return right.success_rate - left.success_rate
      if (sortBy === 'cache') return right.cache_hit_rate - left.cache_hit_rate
      if (sortBy === 'latency')
        return left.avg_latency_ms - right.avg_latency_ms
      return left.route_score - right.route_score
    })
  }, [modelFilter, query.data?.items, search, selected, sortBy, sourceFilter])

  const sourceOptions = useMemo(
    () =>
      Array.from(
        new Set(
          (query.data?.items ?? []).map((item) =>
            item.source_type === 'official'
              ? '官方分组'
              : item.source_label || '来源待审核'
          )
        )
      ).sort(),
    [query.data?.items]
  )
  const modelOptions = useMemo(
    () =>
      Array.from(
        new Set((query.data?.items ?? []).flatMap((item) => item.models))
      ).sort(),
    [query.data?.items]
  )

  const routeOrder = useMemo(() => {
    const itemsByID = new Map(
      (query.data?.items ?? []).map((item) => [item.group_id, item])
    )
    return selectedOrder
      .map((groupID) => itemsByID.get(groupID))
      .filter((item): item is MarketplaceAutoRoutePoolItem => Boolean(item))
  }, [query.data?.items, selectedOrder])
  const hasChanges =
    selectedOrder.length !== serverSelected.length ||
    selectedOrder.some((groupID, index) => groupID !== serverSelected[index])

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
    const next = selectedOrder.filter((item) => item !== groupID)
    if (checked) next.push(groupID)
    setSelectedDraft(next)
  }

  const move = (groupID: string, offset: -1 | 1) => {
    const index = selectedOrder.indexOf(groupID)
    const nextIndex = index + offset
    if (index < 0 || nextIndex < 0 || nextIndex >= selectedOrder.length) return
    const next = [...selectedOrder]
    ;[next[index], next[nextIndex]] = [next[nextIndex], next[index]]
    setSelectedDraft(next)
  }

  const save = async () => {
    try {
      await update.mutateAsync(selectedOrder)
      setSelectedDraft(null)
      toast.success(t('全局 Auto 路由池已保存'))
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('路由池保存失败'))
    }
  }

  return (
    <section className='border-border overflow-hidden rounded-lg border'>
      <div className='bg-muted/25 border-b px-4 py-5 lg:px-5'>
        <div>
          <div className='flex items-center gap-2'>
            <Route className='text-primary size-5' />
            <h2 className='font-semibold'>{t('我的全局 Auto 路由池')}</h2>
          </div>
          <p className='text-muted-foreground mt-2 max-w-2xl text-sm leading-6'>
            {t(
              '在你选择的官方与第三方分组中路由，并按保存的优先级依次尝试；初始推荐顺序综合倍率与保守可用率。'
            )}
          </p>
          <div className='mt-4 flex flex-wrap gap-2'>
            <Badge variant='outline'>
              {t('分组')}: {selected.size}
            </Badge>
            <Badge variant='outline'>{t('Token 分组')}: auto</Badge>
            <Badge variant='outline'>
              {t('额度')}: {t('通用额度')}
            </Badge>
          </div>
        </div>
      </div>

      <div className='border-b'>
        <div className='flex flex-col gap-3 border-b px-4 py-3 sm:flex-row sm:items-center sm:justify-between lg:px-5'>
          <div>
            <div className='flex items-center gap-2'>
              <h3 className='text-sm font-semibold'>{t('已选择路由')}</h3>
              <Badge variant='secondary'>{routeOrder.length}</Badge>
            </div>
            <p className='text-muted-foreground mt-1 text-xs'>
              {t('请求按以下顺序尝试；使用箭头调整优先级。')}
            </p>
          </div>
          <Button
            onClick={save}
            disabled={update.isPending || !hasChanges}
            className='gap-2'
          >
            {update.isPending ? (
              <LoaderCircle className='size-4 animate-spin' />
            ) : (
              <Check className='size-4' />
            )}
            {t('保存路由池')}
          </Button>
        </div>
        {routeOrder.length === 0 ? (
          <div className='text-muted-foreground px-5 py-10 text-center text-sm'>
            {t('尚未选择路由，请从下方待选择项中添加。')}
          </div>
        ) : (
          <div className='divide-border divide-y'>
            {routeOrder.map((item, index) => (
              <AutoPoolRow
                key={item.group_id}
                item={item}
                selected
                order={index + 1}
                onToggle={(checked) => toggle(item.group_id, checked)}
                onMoveUp={() => move(item.group_id, -1)}
                onMoveDown={() => move(item.group_id, 1)}
                canMoveUp={index > 0}
                canMoveDown={index < routeOrder.length - 1}
              />
            ))}
          </div>
        )}
      </div>

      <div className='flex flex-col gap-3 border-b px-4 py-3 lg:px-5'>
        <div>
          <div className='flex items-center gap-2'>
            <h3 className='text-sm font-semibold'>{t('待选择项')}</h3>
            <Badge variant='outline'>{availableItems.length}</Badge>
          </div>
          <p className='text-muted-foreground mt-1 text-xs'>
            {t('选择后会追加到上方路由列表末尾。')}
          </p>
        </div>
        <AutoPoolFilters
          search={search}
          onSearchChange={setSearch}
          sourceFilter={sourceFilter}
          onSourceFilterChange={setSourceFilter}
          sourceOptions={sourceOptions}
          modelFilter={modelFilter}
          onModelFilterChange={setModelFilter}
          modelOptions={modelOptions}
          sortBy={sortBy}
          onSortChange={setSortBy}
          onReset={() => {
            setSearch('')
            setSourceFilter('all')
            setModelFilter('all')
            setSortBy('route')
          }}
        />
      </div>

      {availableItems.length === 0 ? (
        <div className='text-muted-foreground px-5 py-16 text-center text-sm'>
          {query.data.items.length === selected.size
            ? t('所有可用分组都已加入路由池。')
            : t('没有匹配的待选择分组。')}
        </div>
      ) : (
        <div className='grid gap-3 p-4 sm:grid-cols-2 lg:p-5'>
          {availableItems.map((item) => (
            <AutoPoolRow
              key={item.group_id}
              item={item}
              selected={false}
              order={0}
              onToggle={(checked) => toggle(item.group_id, checked)}
              onMoveUp={() => move(item.group_id, -1)}
              onMoveDown={() => move(item.group_id, 1)}
              canMoveUp={false}
              canMoveDown={false}
            />
          ))}
        </div>
      )}
    </section>
  )
}

function SignInRequired() {
  const { t } = useTranslation()
  return (
    <div className='border-border flex min-h-72 flex-col items-center justify-center rounded-lg border px-6 text-center'>
      <Route className='text-primary size-7' />
      <h2 className='mt-4 font-semibold'>{t('登录后配置全局 Auto')}</h2>
      <p className='text-muted-foreground mt-2 max-w-lg text-sm leading-6'>
        {t(
          '路由池属于个人配置。登录后选择需要使用的官方或第三方分组，再在创建 API Key 时选择“Auto”。'
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
