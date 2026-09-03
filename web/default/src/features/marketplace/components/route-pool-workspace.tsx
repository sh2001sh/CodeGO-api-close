import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { ArrowDown, ArrowUp, Check, GripVertical, Plus, Route, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import {
  useMarketplaceRoutePool,
  useMarketplaceRoutePoolCreate,
  useMarketplaceRoutePoolDelete,
  useMarketplaceRoutePoolUpdate,
  useMarketplaceRoutePools,
} from '../hooks'
import type { MarketplaceAutoRoutePoolConfig } from '../types'
import { selectedAutoRoutePoolGroupIDs } from '../lib/auto-route-pool'

export function RoutePoolWorkspace({ compact = false, activePoolID, onActivePoolChange }: { compact?: boolean; activePoolID?: string; onActivePoolChange?: (id: string) => void }) {
  const { t } = useTranslation()
  const pools = useMarketplaceRoutePools()
  const create = useMarketplaceRoutePoolCreate()
  const remove = useMarketplaceRoutePoolDelete()
  const [localPoolID, setLocalPoolID] = useState('')
  const poolID = activePoolID ?? localPoolID
  const setPoolID = (id: string) => { setLocalPoolID(id); onActivePoolChange?.(id) }
  const [name, setName] = useState('')
  useEffect(() => {
    if (!poolID && pools.data?.[0]) setPoolID(pools.data[0].id)
  }, [poolID, pools.data])
  const createPool = async () => {
    const value = name.trim()
    if (!value) return
    const pool = await create.mutateAsync(value)
    setPoolID(pool.id)
    setName('')
  }
  return (
    <section className={compact ? 'border-border overflow-hidden border-y' : 'border-border bg-card overflow-hidden rounded-lg border'}>
      <header className='border-border flex flex-wrap items-center gap-3 border-b px-4 py-3'>
        <span className='border-primary/30 text-primary bg-primary/[0.05] flex size-8 items-center justify-center rounded-md border'><Route className='size-4' /></span>
        <div className='min-w-40 flex-1'>
          <h3 className='font-semibold'>{t('路由池配置')}</h3>
          {!compact && <p className='text-muted-foreground mt-1 text-xs'>{t('API Key 直接选择路由池名称，已存在的 Auto Key 保持兼容。')}</p>}
        </div>
        <select className='border-input bg-background h-9 min-w-36 rounded-md border px-2 text-sm' value={poolID} onChange={(event) => setPoolID(event.target.value)}>
          <option value=''>{t('选择路由池')}</option>
          {pools.data?.map((pool) => <option key={pool.id} value={pool.id}>{pool.name}</option>)}
        </select>
        <div className='flex gap-2'>
          <input className='border-input bg-background h-9 w-28 rounded-md border px-2 text-sm' value={name} onChange={(event) => setName(event.target.value)} placeholder={t('新路由池名称')} aria-label={t('新路由池名称')} />
          <Button variant='outline' size='icon-sm' disabled={!name.trim() || create.isPending} onClick={() => void createPool()} aria-label={t('创建路由池')}><Plus /></Button>
          {poolID && <Button variant='ghost' size='icon-sm' disabled={remove.isPending} onClick={() => { if (window.confirm(t('删除此路由池？已绑定的 API Key 将不能继续使用它。'))) { void remove.mutateAsync(poolID).then(() => setPoolID('')) } }} aria-label={t('删除路由池')}><Trash2 /></Button>}
        </div>
      </header>
      {poolID ? <RoutePoolEditor poolID={poolID} /> : pools.isLoading ? <Skeleton className='m-4 h-36' /> : <div className='text-muted-foreground px-4 py-10 text-center text-sm'>{t('创建一个路由池后，即可从市场分组中选择并排序。')}</div>}
    </section>
  )
}

function RoutePoolEditor({ poolID }: { poolID: string }) {
  const { t } = useTranslation()
  const pool = useMarketplaceRoutePool(poolID)
  const update = useMarketplaceRoutePoolUpdate()
  const [draft, setDraft] = useState<string[]>([])
  const [config, setConfig] = useState<MarketplaceAutoRoutePoolConfig>({ strategy: 'priority', max_attempts: 3, failure_cooldown_seconds: 30, max_multiplier: 0 })
  useEffect(() => setDraft(selectedAutoRoutePoolGroupIDs(pool.data?.items ?? [])), [pool.data?.items])
  useEffect(() => { if (pool.data?.config) setConfig(pool.data.config) }, [pool.data?.config])
  const items = useMemo(() => pool.data?.items ?? [], [pool.data?.items])
  const move = (index: number, direction: -1 | 1) => setDraft((current) => { const nextIndex = index + direction; if (nextIndex < 0 || nextIndex >= current.length) return current; const next = [...current]; [next[index], next[nextIndex]] = [next[nextIndex], next[index]]; return next })
  if (pool.isLoading) return <Skeleton className='m-4 h-56' />
  if (pool.isError) return <div className='text-destructive px-4 py-8 text-sm'>{t('无法读取路由池配置')}</div>
  return <>
    <div className='divide-border divide-y px-4'>
      {draft.length === 0 && <div className='text-muted-foreground py-7 text-sm'>{t('请在左侧市场分组中选择测试通过的分组并加入当前路由池。')}</div>}
      {draft.map((groupID, index) => {
        const item = items.find((candidate) => candidate.group_id === groupID)
        return <div key={groupID} className='flex items-center gap-3 py-3'>
          <GripVertical className='text-muted-foreground size-4 shrink-0' /><span className='text-primary w-4 text-xs font-semibold'>{index + 1}</span>
          <div className='min-w-0 flex-1'><div className='truncate text-sm font-semibold'>{item?.system_display_name ?? groupID}</div><div className='text-muted-foreground mt-1 flex flex-wrap gap-x-3 gap-y-1 text-[11px]'><span>{item?.latest_request_status || t('暂无状态')}</span><span>{t('首字 {{value}}ms', { value: Math.round(item?.avg_ttft_ms ?? 0) })}</span><span>{t('成功率 {{value}}%', { value: item?.success_rate ?? 0 })}</span><span>{t('缓存 {{value}}%', { value: item?.cache_hit_rate ?? 0 })}</span><span className='max-w-64 truncate'>{item?.models.join(', ') || '--'}</span></div></div>
          <div className='flex gap-1'><Button variant='ghost' size='icon-sm' disabled={index === 0} onClick={() => move(index, -1)} aria-label={t('上移')}><ArrowUp /></Button><Button variant='ghost' size='icon-sm' disabled={index === draft.length - 1} onClick={() => move(index, 1)} aria-label={t('下移')}><ArrowDown /></Button></div>
        </div>
      })}
    </div>
    <div className='border-border grid gap-3 border-t px-4 py-3 sm:grid-cols-2 lg:grid-cols-4'>
      <RoutePoolConfig label={t('路由策略')}><select className='border-input bg-background h-9 w-full rounded-md border px-2 text-sm' value={config.strategy} onChange={(event) => setConfig({ ...config, strategy: event.target.value as typeof config.strategy })}><option value='priority'>{t('手动优先级')}</option><option value='score'>{t('健康评分')}</option><option value='cost'>{t('倍率优先')}</option></select></RoutePoolConfig>
      <RoutePoolNumber label={t('最大尝试次数')} value={config.max_attempts} min={1} max={5} onChange={(value) => setConfig({ ...config, max_attempts: value })} />
      <RoutePoolNumber label={t('失败冷却（秒）')} value={config.failure_cooldown_seconds} min={5} max={3600} onChange={(value) => setConfig({ ...config, failure_cooldown_seconds: value })} />
      <RoutePoolNumber label={t('倍率上限（0 为不限）')} value={config.max_multiplier} min={0} step={0.01} onChange={(value) => setConfig({ ...config, max_multiplier: value })} />
    </div>
    <div className='border-border flex justify-end border-t px-4 py-3'><Button size='sm' disabled={update.isPending} onClick={() => update.mutate({ id: poolID, groupIds: draft, config })}><Check />{update.isPending ? t('保存中') : t('保存配置')}</Button></div>
  </>
}

function RoutePoolConfig(props: { label: string; children: ReactNode }) { return <label className='text-xs'><span className='text-muted-foreground mb-1 block'>{props.label}</span>{props.children}</label> }
function RoutePoolNumber(props: { label: string; value: number; min: number; max?: number; step?: number; onChange: (value: number) => void }) { return <RoutePoolConfig label={props.label}><input className='border-input bg-background h-9 w-full rounded-md border px-2 text-sm' type='number' min={props.min} max={props.max} step={props.step} value={props.value} onChange={(event) => props.onChange(Number(event.target.value) || props.min)} /></RoutePoolConfig> }
