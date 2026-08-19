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
import { AutoPoolFilters } from './marketplace-auto-pool-filters'
import { AutoPoolRow, AutoPoolSkeleton } from './marketplace-auto-pool-parts'
import { AutoPoolSourceTabs } from './marketplace-auto-pool-source-tabs'
import {
  useAutoPoolCandidates,
  useAutoPoolSelection,
} from './marketplace-auto-pool-state'

export function MarketplaceAutoPool(props: { authenticated: boolean }) {
  const { t } = useTranslation()
  const query = useMarketplaceAutoRoutePool(props.authenticated)
  const update = useMarketplaceAutoRoutePoolUpdate()

  if (!props.authenticated) return <SignInRequired />
  if (query.isLoading) return <AutoPoolSkeleton />
  if (query.isError || !query.data) {
    return <AutoPoolError onRetry={() => query.refetch()} />
  }
  return (
    <AutoPoolEditor
      items={query.data.items}
      saving={update.isPending}
      onSave={async (groupIDs) => {
        try {
          await update.mutateAsync(groupIDs)
          toast.success(t('全局 Auto 路由池已保存'))
          return true
        } catch (error) {
          toast.error(
            error instanceof Error ? error.message : t('路由池保存失败')
          )
          return false
        }
      }}
    />
  )
}

function AutoPoolEditor(props: {
  items: MarketplaceAutoRoutePoolItem[]
  saving: boolean
  onSave: (groupIDs: string[]) => Promise<boolean>
}) {
  const { t } = useTranslation()
  const selection = useAutoPoolSelection(props.items)
  const candidates = useAutoPoolCandidates(selection.unselected, props.items, {
    official: t('CodeGo 官方'),
    other: t('其他来源'),
  })
  const toggle = (groupID: string, checked: boolean) => {
    const next = selection.order.filter((item) => item !== groupID)
    if (checked) {
      next.push(groupID)
      candidates.leaveEmptySource(groupID)
    }
    selection.setDraft(next)
  }
  const move = (groupID: string, offset: -1 | 1) => {
    const index = selection.order.indexOf(groupID)
    const nextIndex = index + offset
    if (index < 0 || nextIndex < 0 || nextIndex >= selection.order.length)
      return
    const next = [...selection.order]
    ;[next[index], next[nextIndex]] = [next[nextIndex], next[index]]
    selection.setDraft(next)
  }
  const save = async () => {
    if (await props.onSave(selection.order)) selection.setDraft(null)
  }

  return (
    <section className='border-border overflow-hidden rounded-lg border'>
      <AutoPoolHeader selectedCount={selection.selected.size} />
      <SelectedRoutes
        routes={selection.routes}
        changed={selection.changed}
        saving={props.saving}
        onSave={save}
        onToggle={toggle}
        onMove={move}
      />
      <CandidateRoutes
        totalCount={props.items.length}
        selectedCount={selection.selected.size}
        candidates={candidates}
        onToggle={toggle}
      />
    </section>
  )
}

function AutoPoolHeader(props: { selectedCount: number }) {
  const { t } = useTranslation()
  return (
    <div className='bg-muted/25 border-b px-4 py-5 lg:px-5'>
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
          {t('分组')}: {props.selectedCount}
        </Badge>
        <Badge variant='outline'>{t('Token 分组')}: auto</Badge>
        <Badge variant='outline'>
          {t('额度')}: {t('通用额度')}
        </Badge>
      </div>
    </div>
  )
}

function SelectedRoutes(props: {
  routes: MarketplaceAutoRoutePoolItem[]
  changed: boolean
  saving: boolean
  onSave: () => Promise<void>
  onToggle: (groupID: string, checked: boolean) => void
  onMove: (groupID: string, offset: -1 | 1) => void
}) {
  const { t } = useTranslation()
  return (
    <div className='border-b'>
      <div className='flex flex-col gap-3 border-b px-4 py-3 sm:flex-row sm:items-center sm:justify-between lg:px-5'>
        <div>
          <div className='flex items-center gap-2'>
            <h3 className='text-sm font-semibold'>{t('已选择路由')}</h3>
            <Badge variant='secondary'>{props.routes.length}</Badge>
          </div>
          <p className='text-muted-foreground mt-1 text-xs'>
            {t('请求按以下顺序尝试；使用箭头调整优先级。')}
          </p>
        </div>
        <SavePoolButton {...props} />
      </div>
      <SelectedRouteList {...props} />
    </div>
  )
}

function SavePoolButton(props: {
  changed: boolean
  saving: boolean
  onSave: () => Promise<void>
}) {
  const { t } = useTranslation()
  return (
    <Button
      onClick={() => void props.onSave()}
      disabled={props.saving || !props.changed}
      className='gap-2'
    >
      {props.saving ? (
        <LoaderCircle className='size-4 animate-spin' />
      ) : (
        <Check className='size-4' />
      )}
      {t('保存路由池')}
    </Button>
  )
}

function SelectedRouteList(props: {
  routes: MarketplaceAutoRoutePoolItem[]
  onToggle: (groupID: string, checked: boolean) => void
  onMove: (groupID: string, offset: -1 | 1) => void
}) {
  const { t } = useTranslation()
  if (props.routes.length === 0) {
    return (
      <div className='text-muted-foreground px-5 py-10 text-center text-sm'>
        {t('尚未选择路由，请从下方待选择项中添加。')}
      </div>
    )
  }
  return (
    <div className='divide-border divide-y'>
      {props.routes.map((item, index) => (
        <AutoPoolRow
          key={item.group_id}
          item={item}
          selected
          order={index + 1}
          onToggle={(checked) => props.onToggle(item.group_id, checked)}
          onMoveUp={() => props.onMove(item.group_id, -1)}
          onMoveDown={() => props.onMove(item.group_id, 1)}
          canMoveUp={index > 0}
          canMoveDown={index < props.routes.length - 1}
        />
      ))}
    </div>
  )
}

function CandidateRoutes(props: {
  totalCount: number
  selectedCount: number
  candidates: ReturnType<typeof useAutoPoolCandidates>
  onToggle: (groupID: string, checked: boolean) => void
}) {
  const { t } = useTranslation()
  const state = props.candidates
  return (
    <>
      <div className='flex flex-col gap-3 border-b px-4 py-3 lg:px-5'>
        <div>
          <div className='flex items-center gap-2'>
            <h3 className='text-sm font-semibold'>{t('待选择项')}</h3>
            <Badge variant='outline'>{state.visible.length}</Badge>
          </div>
          <p className='text-muted-foreground mt-1 text-xs'>
            {t('按来源浏览分组；选择后会追加到上方路由列表末尾。')}
          </p>
        </div>
        <AutoPoolSourceTabs
          value={state.source}
          options={state.sources}
          onChange={state.setSource}
        />
        <AutoPoolFilters
          search={state.search}
          onSearchChange={state.setSearch}
          modelFilter={state.model}
          onModelFilterChange={state.setModel}
          modelOptions={state.models}
          sortBy={state.sort}
          onSortChange={state.setSort}
          onReset={state.resetFilters}
        />
      </div>
      <CandidateRouteList {...props} />
    </>
  )
}

function CandidateRouteList(props: {
  totalCount: number
  selectedCount: number
  candidates: ReturnType<typeof useAutoPoolCandidates>
  onToggle: (groupID: string, checked: boolean) => void
}) {
  const { t } = useTranslation()
  const items = props.candidates.visible
  if (items.length === 0) {
    return (
      <div className='text-muted-foreground px-5 py-16 text-center text-sm'>
        {props.totalCount === props.selectedCount
          ? t('所有可用分组都已加入路由池。')
          : t('当前来源下没有匹配的待选择分组。')}
      </div>
    )
  }
  return (
    <div className='grid gap-3 p-4 sm:grid-cols-2 lg:p-5'>
      {items.map((item) => (
        <AutoPoolRow
          key={item.group_id}
          item={item}
          selected={false}
          order={0}
          onToggle={(checked) => props.onToggle(item.group_id, checked)}
          onMoveUp={() => undefined}
          onMoveDown={() => undefined}
          canMoveUp={false}
          canMoveDown={false}
        />
      ))}
    </div>
  )
}

function AutoPoolError(props: { onRetry: () => void }) {
  const { t } = useTranslation()
  return (
    <div className='border-border flex min-h-64 flex-col items-center justify-center rounded-lg border px-6 text-center'>
      <ShieldCheck className='text-destructive size-6' />
      <h2 className='mt-3 font-semibold'>{t('无法读取 Auto 路由池')}</h2>
      <Button className='mt-4' variant='outline' onClick={props.onRetry}>
        {t('重新获取')}
      </Button>
    </div>
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
