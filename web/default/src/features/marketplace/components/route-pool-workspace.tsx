import { useEffect, useMemo, useState } from 'react'
import { ArrowDown, ArrowUp, Check, GripVertical, Route } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import {
  useMarketplaceAutoRoutePool,
  useMarketplaceAutoRoutePoolUpdate,
} from '../hooks'
import type { MarketplaceAutoRoutePool } from '../types'
import { selectedAutoRoutePoolGroupIDs } from '../lib/auto-route-pool'

export function RoutePoolWorkspace() {
  const { t } = useTranslation()
  const pool = useMarketplaceAutoRoutePool()
  const updatePool = useMarketplaceAutoRoutePoolUpdate()
  const items = useMemo(() => pool.data?.items ?? [], [pool.data?.items])
  const selected = useMemo(() => selectedAutoRoutePoolGroupIDs(items), [items])
  const [draft, setDraft] = useState<string[]>([])
  const [config, setConfig] = useState<MarketplaceAutoRoutePool['config']>({
    strategy: 'priority',
    max_attempts: 3,
    failure_cooldown_seconds: 30,
    max_multiplier: 0,
  })
  useEffect(() => setDraft(selected), [selected])
  useEffect(() => {
    if (pool.data?.config) setConfig(pool.data.config)
  }, [pool.data?.config])

  const move = (index: number, direction: -1 | 1) => {
    const nextIndex = index + direction
    if (nextIndex < 0 || nextIndex >= draft.length) return
    const next = [...draft]
    ;[next[index], next[nextIndex]] = [next[nextIndex], next[index]]
    setDraft(next)
  }

  if (pool.isLoading) return <Skeleton className='h-64 w-full' />
  if (pool.isError) {
    return (
      <section className='border-border bg-card rounded-lg border p-5'>
        <div className='font-medium'>{t('路由池加载失败')}</div>
        <p className='text-muted-foreground mt-1 text-sm'>
          {t('无法读取当前 Auto 路由池，请稍后重试。')}
        </p>
        <Button
          className='mt-4'
          variant='outline'
          onClick={() => void pool.refetch()}
        >
          {t('重新加载')}
        </Button>
      </section>
    )
  }

  return (
    <section className='border-border bg-card overflow-hidden rounded-lg border'>
      <header className='border-border flex flex-wrap items-start justify-between gap-4 border-b px-4 py-4 sm:px-5'>
        <div className='flex items-start gap-3'>
          <span className='border-primary/30 text-primary bg-primary/[0.05] flex size-9 items-center justify-center rounded-md border'>
            <Route className='size-4' />
          </span>
          <div>
            <h3 className='font-semibold'>{t('路由池配置')}</h3>
            <p className='text-muted-foreground mt-1 text-xs leading-5'>
              {t(
                '配置 Auto 自动路由的优先顺序。固定绑定到 Key 的分组不受影响。'
              )}
            </p>
          </div>
        </div>
        <Button
          size='sm'
          disabled={updatePool.isPending}
          onClick={() => updatePool.mutate({ groupIds: draft, config })}
        >
          <Check />
          {updatePool.isPending ? t('保存中') : t('保存配置')}
        </Button>
      </header>
      {draft.length === 0 ? (
        <div className='text-muted-foreground flex min-h-56 flex-col items-center justify-center px-5 text-center text-sm'>
          <Route className='mb-3 size-6' />
          <div>{t('路由池还是空的')}</div>
          <p className='mt-1 text-xs'>
            {t('请在市场分组中测试并选择分组后加入路由池。')}
          </p>
        </div>
      ) : (
        <div className='divide-border divide-y px-4 sm:px-5'>
          {draft.map((groupID, index) => {
            const item = items.find(
              (candidate) => candidate.group_id === groupID
            )
            return (
              <div key={groupID} className='flex items-center gap-3 py-3'>
                <GripVertical className='text-muted-foreground size-4 shrink-0' />
                <span className='text-primary app-numeric w-5 text-center text-xs font-semibold tabular-nums'>
                  {index + 1}
                </span>
                <div className='min-w-0 flex-1'>
                  <div className='truncate text-sm font-semibold'>
                    {item?.system_display_name ?? groupID}
                  </div>
                  <div className='text-muted-foreground mt-0.5 truncate text-xs'>
                    {item?.source_label || t('第三方分组')} ·{' '}
                    {item?.latest_request_status || t('暂无近期状态')}
                  </div>
                </div>
                <div className='flex shrink-0 items-center gap-1'>
                  <Button
                    variant='ghost'
                    size='icon-sm'
                    disabled={index === 0 || updatePool.isPending}
                    onClick={() => move(index, -1)}
                    aria-label={t('上移')}
                  >
                    <ArrowUp />
                  </Button>
                  <Button
                    variant='ghost'
                    size='icon-sm'
                    disabled={
                      index === draft.length - 1 || updatePool.isPending
                    }
                    onClick={() => move(index, 1)}
                    aria-label={t('下移')}
                  >
                    <ArrowDown />
                  </Button>
                </div>
              </div>
            )
          })}
        </div>
      )}
      <div className='border-border grid gap-3 border-t px-4 py-4 sm:grid-cols-2 sm:px-5 lg:grid-cols-4'>
        <label className='text-xs'>
          <span className='text-muted-foreground mb-1 block'>
            {t('路由策略')}
          </span>
          <select
            className='border-input bg-background h-9 w-full rounded-md border px-2 text-sm'
            value={config.strategy}
            onChange={(event) =>
              setConfig((current) => ({
                ...current,
                strategy: event.target.value as typeof current.strategy,
              }))
            }
          >
            <option value='priority'>{t('手动优先级')}</option>
            <option value='score'>{t('健康评分')}</option>
            <option value='cost'>{t('倍率优先')}</option>
          </select>
        </label>
        <label className='text-xs'>
          <span className='text-muted-foreground mb-1 block'>
            {t('最大尝试次数')}
          </span>
          <input
            className='border-input bg-background h-9 w-full rounded-md border px-2 text-sm'
            type='number'
            min={1}
            max={5}
            value={config.max_attempts}
            onChange={(event) =>
              setConfig((current) => ({
                ...current,
                max_attempts: Number(event.target.value) || 1,
              }))
            }
          />
        </label>
        <label className='text-xs'>
          <span className='text-muted-foreground mb-1 block'>
            {t('失败冷却（秒）')}
          </span>
          <input
            className='border-input bg-background h-9 w-full rounded-md border px-2 text-sm'
            type='number'
            min={5}
            max={3600}
            value={config.failure_cooldown_seconds}
            onChange={(event) =>
              setConfig((current) => ({
                ...current,
                failure_cooldown_seconds: Number(event.target.value) || 5,
              }))
            }
          />
        </label>
        <label className='text-xs'>
          <span className='text-muted-foreground mb-1 block'>
            {t('倍率上限（0 为不限）')}
          </span>
          <input
            className='border-input bg-background h-9 w-full rounded-md border px-2 text-sm'
            type='number'
            min={0}
            step={0.01}
            value={config.max_multiplier}
            onChange={(event) =>
              setConfig((current) => ({
                ...current,
                max_multiplier: Number(event.target.value) || 0,
              }))
            }
          />
        </label>
      </div>
      <footer className='border-border bg-muted/15 text-muted-foreground border-t px-4 py-3 text-xs leading-5 sm:px-5'>
        {t(
          '通过批量测试加入的分组会追加到路由池末尾，已存在的分组不会重复添加。'
        )}
      </footer>
    </section>
  )
}
