/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com.
*/
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import i18n from '@/i18n/config'
import { getIntlLocale } from '@/i18n/format'
import {
  Check,
  ChevronDown,
  ChevronUp,
  ChevronsUpDown,
  GripVertical,
  LoaderCircle,
  Plus,
  Sparkles,
  Waypoints,
  X,
  Zap,
} from 'lucide-react'
import {
  AnimatePresence,
  motion,
  Reorder,
  useDragControls,
  useReducedMotion,
} from 'motion/react'
import { Trans, useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatQuota } from '@/lib/format'
import { cn } from '@/lib/utils'
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import {
  getMarketplaceRoutePools,
  getMarketplaceBatchTest,
  startMarketplaceBatchTest,
} from '@/features/marketplace/api'
import {
  useMarketplaceAutoRoutePool,
  useMarketplaceAutoRoutePoolUpdate,
  useMarketplaceRoutePool,
  useMarketplaceRoutePoolCreate,
  useMarketplaceRoutePoolDelete,
  useMarketplaceRoutePoolUpdate,
  useMarketplaceRoutePoolAutoBuildRun,
} from '@/features/marketplace/hooks'
import type {
  MarketplaceAutoRoutePoolConfig,
  MarketplaceBatchTest,
  MarketplaceBatchTestItem,
  MarketplaceGroup,
  MarketplaceRoutePoolAutoBuild,
} from '@/features/marketplace/types'
import { DawnQueryError } from '../components/query-error'
import { pct, sec } from '../lib/format'
import './pool-workbench.css'

const STRATEGY_LABEL: Record<string, string> = {
  priority: '排序轮询',
  score: '评分优先',
  cost: '最低倍率',
}

const AUTO_ID = 'auto'
const autoBuildControlClass =
  'mt-1 h-9 w-full rounded-md border border-border bg-background px-2.5 text-foreground shadow-sm outline-none focus:border-primary focus:ring-2 focus:ring-primary/20'

type PanelMode = 'pool' | 'create' | 'autobuild'

interface WorkbenchMember {
  id: string
  name: string
  source: string
  multiplier: number
  successRate: number | null
  ttft: number | null
  cache: number | null
  observing: boolean
  models: string[]
}

export type PoolPanelMode = PanelMode

export function PoolWorkbench(props: {
  authed: boolean
  groups: MarketplaceGroup[]
  activePoolID: string
  onActivePoolChange: (id: string) => void
  mode: PoolPanelMode
  onModeChange: (mode: PoolPanelMode) => void
}) {
  const { t } = useTranslation()
  const {
    authed,
    groups,
    activePoolID,
    onActivePoolChange,
    mode,
    onModeChange,
  } = props

  const pools = useQuery({
    queryKey: ['marketplace-route-pools'],
    queryFn: getMarketplaceRoutePools,
    enabled: authed,
    retry: false,
    staleTime: 30_000,
    refetchOnWindowFocus: false,
  })
  const autoPool = useMarketplaceAutoRoutePool(authed)
  const poolDetail = useMarketplaceRoutePool(
    authed && activePoolID !== AUTO_ID ? activePoolID : ''
  )
  const createPool = useMarketplaceRoutePoolCreate()
  const updatePool = useMarketplaceRoutePoolUpdate()
  const deletePool = useMarketplaceRoutePoolDelete()
  const updateAutoPool = useMarketplaceAutoRoutePoolUpdate()
  const runAutoBuild = useMarketplaceRoutePoolAutoBuildRun()

  const poolOptions = useMemo(() => {
    const custom = (pools.data ?? []).map((pool) => ({
      id: pool.id,
      name: pool.name,
      count: pool.member_count,
    }))
    const auto = autoPool.data
      ? [
          {
            id: AUTO_ID,
            name: t('AUTO 池'),
            count: autoPool.data.selected_count,
          },
        ]
      : []
    return [...auto, ...custom]
  }, [pools.data, autoPool.data, t])

  useEffect(() => {
    if (!authed || pools.isPending || autoPool.isPending) return
    if (activePoolID && poolOptions.some((pool) => pool.id === activePoolID))
      return
    onActivePoolChange(poolOptions[0]?.id ?? '')
  }, [
    authed,
    activePoolID,
    poolOptions,
    onActivePoolChange,
    pools.isPending,
    autoPool.isPending,
  ])

  const isAuto = activePoolID === AUTO_ID
  const persistedMemberIds = useMemo(() => {
    const items = (isAuto ? autoPool.data?.items : poolDetail.data?.items) ?? []
    return items
      .filter((item) => item.selected)
      .sort((a, b) => a.priority - b.priority)
      .map((item) => item.group_id)
  }, [isAuto, autoPool.data, poolDetail.data])
  const persistedMemberKey = `${activePoolID}:${persistedMemberIds.join(',')}`
  const [draft, setDraft] = useState({
    key: persistedMemberKey,
    ids: persistedMemberIds,
  })
  const draftMemberIds =
    draft.key === persistedMemberKey ? draft.ids : persistedMemberIds
  const setDraftMemberIds = (ids: string[]) =>
    setDraft({ key: persistedMemberKey, ids })
  const [poolTests, setPoolTests] = useState<{
    poolID: string
    items: MarketplaceBatchTestItem[]
  }>({ poolID: activePoolID, items: [] })
  const testItems = poolTests.poolID === activePoolID ? poolTests.items : []
  const setTestItems = useCallback(
    (items: MarketplaceBatchTestItem[]) =>
      setPoolTests({ poolID: activePoolID, items }),
    [activePoolID]
  )
  const members = useMemo(() => {
    if (isAuto) {
      const items = (autoPool.data?.items ?? []).filter((item) => item.selected)
      const byId = new Map(items.map((item) => [item.group_id, item]))
      return draftMemberIds
        .map((id) => byId.get(id))
        .filter((item): item is (typeof items)[number] => Boolean(item))
        .map((item) => {
          return {
            id: item.group_id,
            name: item.system_display_name,
            source: item.source_label,
            multiplier: item.multiplier,
            successRate: item.metrics_available ? item.success_rate : null,
            ttft: item.metrics_available ? item.avg_ttft_ms : null,
            cache: item.metrics_available ? item.cache_hit_rate : null,
            observing: item.observing,
            models: item.models,
          }
        })
    }
    const byId = new Map(
      (poolDetail.data?.items ?? [])
        .filter((item) => item.selected)
        .map((item) => [item.group_id, item])
    )
    return draftMemberIds
      .map((id) => byId.get(id))
      .filter((item): item is NonNullable<typeof item> => Boolean(item))
      .map((item) => {
        return {
          id: item.group_id,
          name: item.system_display_name,
          source: item.source_label,
          multiplier: item.multiplier,
          successRate: item.metrics_available ? item.success_rate : null,
          ttft: item.metrics_available ? item.avg_ttft_ms : null,
          cache: item.metrics_available ? item.cache_hit_rate : null,
          observing: item.observing,
          models: item.models,
        }
      })
  }, [isAuto, autoPool.data, poolDetail.data, draftMemberIds])

  const config: MarketplaceAutoRoutePoolConfig | undefined = isAuto
    ? autoPool.data?.config
    : poolDetail.data?.config

  const currentConfig = config
    ? {
        strategy: config.strategy,
        max_attempts: config.max_attempts,
        failure_cooldown_seconds: config.failure_cooldown_seconds,
        max_multiplier: config.max_multiplier,
      }
    : undefined

  const weighted = useMemo(() => {
    const withMetrics = members.filter((m) => m.successRate != null)
    if (!withMetrics.length) return null
    const avg = (pick: (m: (typeof members)[number]) => number | null) => {
      let sum = 0
      let count = 0
      withMetrics.forEach((m) => {
        const value = pick(m)
        if (value != null) {
          sum += value
          count += 1
        }
      })
      return count ? sum / count : null
    }
    return {
      success: avg((m) => m.successRate),
      ttft: avg((m) => m.ttft),
      cache: avg((m) => m.cache),
    }
  }, [members])

  const saveMemberOrder = (orderedIds: string[]) => {
    if (isAuto) {
      updateAutoPool.mutate(
        { groupIds: orderedIds, config: currentConfig },
        { onSuccess: () => toast.success(i18n.t('AUTO 池已更新')) }
      )
      return
    }
    updatePool.mutate(
      { id: activePoolID, groupIds: orderedIds, config: currentConfig },
      { onSuccess: () => toast.success(i18n.t('路由池已更新')) }
    )
  }

  const move = async (index: number, delta: number) => {
    const next = [...members]
    const target = index + delta
    if (target < 0 || target >= next.length) return
    ;[next[index], next[target]] = [next[target], next[index]]
    setDraftMemberIds(next.map((m) => m.id))
  }

  const remove = async (id: string) => {
    setDraftMemberIds(members.filter((m) => m.id !== id).map((m) => m.id))
  }

  const isDirty = draftMemberIds.join(',') !== persistedMemberIds.join(',')

  const setStrategy = (strategy: string) => {
    const onSuccess = () => toast.success(i18n.t('路由方式已更新'))
    if (isAuto) {
      updateAutoPool.mutate(
        {
          groupIds: members.map((m) => m.id),
          config: { strategy: strategy as 'priority' | 'score' | 'cost' },
        },
        { onSuccess }
      )
    } else {
      updatePool.mutate(
        {
          id: activePoolID,
          groupIds: members.map((m) => m.id),
          config: { strategy: strategy as 'priority' | 'score' | 'cost' },
        },
        { onSuccess }
      )
    }
  }

  const rename = (name: string) => {
    const trimmedName = name.trim()
    if (
      isAuto ||
      !trimmedName ||
      trimmedName === poolDetail.data?.name ||
      updatePool.isPending
    )
      return
    updatePool.mutate({
      id: activePoolID,
      name: trimmedName,
      groupIds: members.map((m) => m.id),
      config: currentConfig,
    })
  }

  const removePool = () => {
    if (isAuto) return
    deletePool.mutate(activePoolID, {
      onSuccess: () => {
        onActivePoolChange('')
        toast.success(i18n.t('路由池已删除'))
      },
    })
  }

  return (
    <aside
      className='poolpanel pool-workbench'
      id='market-route-workbench'
      data-pool-target
      tabIndex={-1}
      aria-label={i18n.t('路由池工作台')}
    >
      <div className='ph'>
        <div className='pool-title-row'>
          <Waypoints size={20} aria-hidden='true' />
          <h3>
            {mode === 'create'
              ? i18n.t('新建路由池')
              : mode === 'autobuild'
                ? i18n.t('智能新建池')
                : isAuto
                  ? i18n.t('AUTO 池')
                  : (poolDetail.data?.name ?? i18n.t('路由池工作台'))}
          </h3>
        </div>
        <div className='psub'>
          {mode === 'pool'
            ? i18n.t('POOL · {{param0}} 分组 · {{param1}}', {
                param0: members.length,
                param1: config
                  ? i18n.t(STRATEGY_LABEL[config.strategy] ?? config.strategy)
                  : '—',
              })
            : mode === 'create'
              ? i18n.t('Create')
              : i18n.t('Auto build')}
        </div>
      </div>
      <div className='pb'>
        {authed &&
          (pools.isError || autoPool.isError || poolDetail.isError) && (
            <DawnQueryError
              title={i18n.t('加载失败')}
              onRetry={() => {
                void pools.refetch()
                void autoPool.refetch()
                if (activePoolID && activePoolID !== AUTO_ID)
                  void poolDetail.refetch()
              }}
              retrying={
                pools.isFetching || autoPool.isFetching || poolDetail.isFetching
              }
            />
          )}
        {!authed ? (
          <>
            <PoolTelemetry success={null} ttft={null} cache={null} />
            <div className='poolbox'>
              <div className='pool-head'>
                <span>
                  <Trans i18nKey={'分组 · 0'} />
                </span>
                <span className='c2'>{i18n.t('排序')}</span>
              </div>
              <div className='pp-empty'>
                <Waypoints size={26} />
                <span>
                  <Trans i18nKey={'登录后管理路由池'} />
                </span>
                <Link className='btn mini primary' to='/sign-in'>
                  <Trans i18nKey={'登录 / 注册'} />
                </Link>
              </div>
            </div>
          </>
        ) : mode === 'create' ? (
          <CreatePanel
            onCancel={() => onModeChange('pool')}
            onCreate={async (name, strategy) => {
              const pool = await createPool.mutateAsync({
                name,
                config: { strategy },
              })
              onActivePoolChange(pool.id)
              onModeChange('pool')
              toast.success(i18n.t('路由池已创建'))
            }}
          />
        ) : mode === 'autobuild' ? (
          <AutoBuildPanel
            groups={groups}
            onCancel={() => onModeChange('pool')}
            onGenerate={async (name, groupIds, strategy, autoBuild) => {
              let generatedName = name.trim() || i18n.t('自动池')
              if (generatedName === '自动池') {
                const existingNames = new Set(
                  (pools.data ?? []).map((pool) => pool.name)
                )
                if (existingNames.has(generatedName)) {
                  let suffix = 2
                  while (existingNames.has(`${generatedName} ${suffix}`))
                    suffix += 1
                  generatedName = `${generatedName} ${suffix}`
                }
              }
              const pool = await createPool.mutateAsync({
                name: generatedName,
                groupIds,
                config: { strategy },
                autoBuild,
              })
              onActivePoolChange(pool.id)
              onModeChange('pool')
              toast.success(
                i18n.t('已生成路由池 · {{param0}} 分组', {
                  param0: groupIds.length,
                })
              )
            }}
          />
        ) : (
          <>
            <div className='toolbar'>
              <div className='row pool-selection-row'>
                <select
                  className='iname'
                  aria-label={i18n.t('路由池')}
                  value={activePoolID}
                  onChange={(event) => onActivePoolChange(event.target.value)}
                >
                  {poolOptions.map((option) => (
                    <option key={option.id} value={option.id}>
                      {option.name} · {option.count} <Trans i18nKey={'组'} />
                    </option>
                  ))}
                </select>
                <button
                  className='btn mini'
                  aria-label={i18n.t('新建路由池')}
                  title={i18n.t('新建路由池')}
                  onClick={() => onModeChange('create')}
                >
                  <Plus size={14} />
                </button>
                <button
                  className='btn mini'
                  aria-label={i18n.t('智能新建一个路由池')}
                  title={i18n.t('智能新建一个路由池')}
                  onClick={() => onModeChange('autobuild')}
                >
                  <Sparkles size={14} />
                </button>
              </div>
              <div className='row'>
                <input
                  className='iname'
                  aria-label={i18n.t('路由池名称')}
                  defaultValue={
                    isAuto ? i18n.t('AUTO 池') : poolDetail.data?.name
                  }
                  key={`${activePoolID}-${poolDetail.data?.name}`}
                  disabled={isAuto || updatePool.isPending || isDirty}
                  onBlur={(event) => void rename(event.target.value)}
                />
                <select
                  value={config?.strategy ?? 'priority'}
                  disabled={
                    updatePool.isPending || updateAutoPool.isPending || isDirty
                  }
                  onChange={(event) => void setStrategy(event.target.value)}
                  aria-label={i18n.t('路由方式')}
                  title={i18n.t('路由方式')}
                >
                  {Object.entries(STRATEGY_LABEL).map(([value, label]) => (
                    <option key={value} value={value}>
                      {i18n.t(label)}
                    </option>
                  ))}
                </select>
                {!isAuto && (
                  <button
                    className='btn mini'
                    aria-label={i18n.t('删除池')}
                    title={i18n.t('删除池')}
                    disabled={deletePool.isPending || updatePool.isPending}
                    onClick={() => void removePool()}
                  >
                    <X size={14} />
                  </button>
                )}
              </div>
            </div>

            {!isAuto && poolDetail.data && (
              <RoutePoolAutoBuildSettings
                value={poolDetail.data.auto_build}
                groups={groups}
                busy={runAutoBuild.isPending || updatePool.isPending || isDirty}
                onRun={async () => {
                  const pool = await runAutoBuild.mutateAsync(activePoolID)
                  toast.success(
                    i18n.t('自动构建完成 · {{param0}} 分组', {
                      param0: pool.selected_count,
                    })
                  )
                }}
                onSave={async (autoBuild) => {
                  await updatePool.mutateAsync({
                    id: activePoolID,
                    groupIds: draftMemberIds,
                    config: currentConfig,
                    autoBuild,
                  })
                  toast.success(i18n.t('自动构建计划已保存'))
                }}
              />
            )}

            <PoolTelemetry
              success={weighted?.success ?? null}
              ttft={weighted?.ttft ?? null}
              cache={weighted?.cache ?? null}
            />
            <div className='pool-composition'>
              <span>
                {i18n.t('池构成')} <b>{members.length}</b>
              </span>
              <span>{isAuto ? 'AUTO' : i18n.t('手动')}</span>
              <span>
                {config
                  ? i18n.t(STRATEGY_LABEL[config.strategy] ?? config.strategy)
                  : '—'}
              </span>
            </div>
            <div className='poolbox'>
              <div className='pool-head'>
                <span>
                  {i18n.t('分组 ·')} {members.length}
                </span>
                <span className='c2'>{i18n.t('排序')}</span>
              </div>
              {members.length ? (
                <Reorder.Group
                  className='pool-node-list'
                  axis='y'
                  values={draftMemberIds}
                  onReorder={setDraftMemberIds}
                >
                  <AnimatePresence initial={false}>
                    {members.map((member, index) => (
                      <PoolNode
                        key={member.id}
                        member={member}
                        index={index}
                        count={members.length}
                        test={testItems.find(
                          (item) => item.group_id === member.id
                        )}
                        onMove={(delta) => void move(index, delta)}
                        onRemove={() => void remove(member.id)}
                      />
                    ))}
                  </AnimatePresence>
                </Reorder.Group>
              ) : (
                <div className='pp-empty'>
                  <Waypoints size={26} aria-hidden='true' />
                  <span>{i18n.t('空池')}</span>
                </div>
              )}
            </div>
            <PoolConnectivityTest
              key={activePoolID}
              members={members}
              onResults={setTestItems}
            />
            {isDirty && (
              <div className='pp-foot'>
                <span className='sub2'>
                  <Trans i18nKey={'成员调整尚未保存'} />
                </span>
                <button
                  className='btn mini primary'
                  disabled={updatePool.isPending || updateAutoPool.isPending}
                  onClick={() => void saveMemberOrder(draftMemberIds)}
                >
                  <Trans i18nKey={'保存更改'} />
                </button>
              </div>
            )}
          </>
        )}
      </div>
    </aside>
  )
}

function PoolValue(props: { value: string; className?: string }) {
  const reducedMotion = useReducedMotion()
  return (
    <span className={cn('pool-value', props.className)}>
      <AnimatePresence initial={false} mode='popLayout'>
        <motion.span
          key={props.value}
          initial={reducedMotion ? false : { y: 8, opacity: 0 }}
          animate={{ y: 0, opacity: 1 }}
          exit={reducedMotion ? undefined : { y: -8, opacity: 0 }}
          transition={{ duration: 0.2 }}
        >
          {props.value}
        </motion.span>
      </AnimatePresence>
    </span>
  )
}

function PoolTelemetry(props: {
  success: number | null
  ttft: number | null
  cache: number | null
}) {
  const { t } = useTranslation()
  return (
    <div className='pool-telemetry' aria-label={t('池内均值')}>
      <div>
        <span>{t('成功率')}</span>
        <PoolValue
          value={props.success == null ? '—' : `${pct(props.success)}%`}
          className={
            props.success != null && props.success >= 98 ? 'ok' : undefined
          }
        />
      </div>
      <div>
        <span>{t('首字')}</span>
        <PoolValue value={props.ttft == null ? '—' : `${sec(props.ttft)}s`} />
      </div>
      <div>
        <span>{t('缓存')}</span>
        <PoolValue
          value={props.cache == null ? '—' : `${pct(props.cache, 0)}%`}
        />
      </div>
    </div>
  )
}

function PoolNode(props: {
  member: WorkbenchMember
  index: number
  count: number
  test?: MarketplaceBatchTestItem
  onMove: (delta: number) => void
  onRemove: () => void
}) {
  const { t } = useTranslation()
  const dragControls = useDragControls()
  const reducedMotion = useReducedMotion()
  return (
    <Reorder.Item
      className='pool-item'
      value={props.member.id}
      dragListener={false}
      dragControls={dragControls}
      layout
      initial={reducedMotion ? false : { opacity: 0, y: 12, scale: 0.98 }}
      animate={{ opacity: 1, y: 0, scale: 1 }}
      exit={reducedMotion ? undefined : { opacity: 0, scale: 0.96 }}
      whileDrag={{
        scale: 1.02,
        opacity: 0.85,
        zIndex: 5,
        boxShadow: '0 14px 28px rgb(0 0 0 / 0.12)',
      }}
      transition={
        reducedMotion
          ? { duration: 0 }
          : { type: 'spring', stiffness: 300, damping: 28 }
      }
      data-test-status={props.test?.status}
    >
      <div className='r1'>
        <button
          type='button'
          className='pool-drag-handle'
          onPointerDown={(event) => dragControls.start(event)}
          aria-label={t('拖拽排序')}
          title={t('拖拽排序')}
        >
          <GripVertical size={15} aria-hidden='true' />
        </button>
        <PoolValue
          className='pool-order'
          value={String(props.index + 1).padStart(2, '0')}
        />
        <i
          className={cn(
            'dot',
            props.member.observing && 'w',
            props.member.successRate == null && 'unknown'
          )}
          aria-hidden='true'
        />
        <span className='cname' title={props.member.name}>
          {t(props.member.name)}
        </span>
      </div>
      <div className='r2'>
        <div>
          {t('倍率')}
          <b>{props.member.multiplier}×</b>
        </div>
        <div>
          {t('成功')}
          <b
            className={
              props.member.successRate != null && props.member.successRate < 98
                ? 'warn'
                : undefined
            }
          >
            {props.member.successRate == null
              ? '—'
              : `${pct(props.member.successRate)}%`}
          </b>
        </div>
        <div>
          {t('首字')}
          <b
            className={
              props.member.ttft != null && props.member.ttft > 600
                ? 'warn'
                : undefined
            }
          >
            {props.member.ttft == null ? '—' : `${sec(props.member.ttft)}s`}
          </b>
        </div>
        <div>
          {t('缓存')}
          <b>
            {props.member.cache == null
              ? '—'
              : `${pct(props.member.cache, 0)}%`}
          </b>
        </div>
      </div>
      <div className='pool-node-footer'>
        <div className='pool-node-source'>
          {t(props.member.source)}
          {props.member.observing && <span>{t('观测中')}</span>}
        </div>
        <span className='ord'>
          <button
            disabled={props.index === 0}
            onClick={() => props.onMove(-1)}
            aria-label={t('上移')}
            title={t('上移')}
          >
            <ChevronUp size={13} aria-hidden='true' />
          </button>
          <button
            disabled={props.index === props.count - 1}
            onClick={() => props.onMove(1)}
            aria-label={t('下移')}
            title={t('下移')}
          >
            <ChevronDown size={13} aria-hidden='true' />
          </button>
          <button
            onClick={props.onRemove}
            aria-label={t('移出')}
            title={t('移出')}
          >
            <X size={13} aria-hidden='true' />
          </button>
        </span>
      </div>
      {props.test && (
        <div className='pool-test-node-status' role='status'>
          {props.test.status === 'passed' ? (
            <Check size={12} aria-hidden='true' />
          ) : props.test.status === 'failed' ? (
            <X size={12} aria-hidden='true' />
          ) : (
            <LoaderCircle
              className='pool-test-spinner'
              size={12}
              aria-hidden='true'
            />
          )}
          <span>{batchItemLabel(props.test)}</span>
          {props.test.quota_charged > 0 && (
            <span>{formatQuota(props.test.quota_charged)}</span>
          )}
        </div>
      )}
    </Reorder.Item>
  )
}

function batchItemLabel(item: MarketplaceBatchTestItem): string {
  if (item.status === 'passed') return `${item.latency_ms}ms`
  if (item.status === 'failed') return item.error || i18n.t('测试失败')
  return item.status === 'queued' ? i18n.t('排队中') : i18n.t('测试中')
}

function PoolConnectivityTest(props: {
  members: WorkbenchMember[]
  onResults: (items: MarketplaceBatchTestItem[]) => void
}) {
  const { t } = useTranslation()
  const onResults = props.onResults
  const [open, setOpen] = useState(false)
  const [model, setModel] = useState('')
  const [billingConfirmed, setBillingConfirmed] = useState(false)
  const [starting, setStarting] = useState(false)
  const [startError, setStartError] = useState('')
  const [requestedCount, setRequestedCount] = useState(0)
  const mounted = useRef(true)
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])
  const [batches, setBatches] = useState<MarketplaceBatchTest[]>([])
  const commonModels = useMemo(() => {
    const first = props.members[0]?.models ?? []
    return first.filter((candidate) =>
      props.members.every((member) =>
        member.models.some(
          (name) => name.toLowerCase() === candidate.toLowerCase()
        )
      )
    )
  }, [props.members])
  const selectedModel = commonModels.includes(model)
    ? model
    : (commonModels[0] ?? '')
  const results = useQuery({
    queryKey: [
      'marketplace-route-pool-tests',
      batches.map((batch) => batch.id),
    ],
    queryFn: () =>
      Promise.all(batches.map((batch) => getMarketplaceBatchTest(batch.id))),
    initialData: batches,
    enabled: batches.length > 0,
    retry: false,
    refetchOnWindowFocus: false,
    refetchInterval: (query) => {
      if (query.state.error) return false
      return query.state.data?.some(
        (batch) => batch.status === 'queued' || batch.status === 'running'
      )
        ? 1500
        : false
    },
  })
  const testResults = results.data
  const running =
    starting ||
    testResults.some(
      (batch) => batch.status === 'queued' || batch.status === 'running'
    )
  const items = useMemo(
    () => testResults.flatMap((batch) => batch.items),
    [testResults]
  )
  const completed = items.filter(
    (item) => item.status === 'passed' || item.status === 'failed'
  ).length
  const failed =
    !!startError ||
    testResults.some((batch) => batch.status === 'failed') ||
    items.some((item) => item.status === 'failed')
  useEffect(() => {
    onResults(items)
  }, [items, onResults])

  const start = async () => {
    if (running || !selectedModel || !billingConfirmed || !props.members.length)
      return
    setStarting(true)
    setStartError('')
    setBatches([])
    props.onResults([])
    const started: MarketplaceBatchTest[] = []
    const memberIDs = props.members.map((member) => member.id)
    setRequestedCount(memberIDs.length)
    try {
      for (let offset = 0; offset < memberIDs.length; offset += 5) {
        if (!mounted.current) break
        const batch = await startMarketplaceBatchTest({
          groupIds: memberIDs.slice(offset, offset + 5),
          model: selectedModel,
        })
        if (!mounted.current) break
        started.push(batch)
        setBatches([...started])
      }
    } catch (error) {
      if (mounted.current)
        setStartError(error instanceof Error ? error.message : t('测试失败'))
    } finally {
      if (mounted.current) setStarting(false)
    }
  }

  return (
    <div className='pool-connectivity'>
      {!open ? (
        <button
          className='btn primary pool-test-trigger'
          onClick={() => setOpen(true)}
          disabled={!props.members.length}
        >
          <Zap size={15} aria-hidden='true' />
          {t('连通性测试')}
        </button>
      ) : (
        <>
          <div className='pool-test-heading'>
            <span>{t('连通性测试')}</span>
            <button
              aria-label={t('收起参数')}
              title={t('收起参数')}
              onClick={() => setOpen(false)}
            >
              <ChevronUp size={16} aria-hidden='true' />
            </button>
          </div>
          <label className='pool-test-model'>
            <span>{t('模型')}</span>
            <select
              value={selectedModel}
              onChange={(event) => setModel(event.target.value)}
              disabled={running}
            >
              {!commonModels.length && (
                <option value=''>{t('无共同模型')}</option>
              )}
              {commonModels.map((name) => (
                <option key={name} value={name}>
                  {name}
                </option>
              ))}
            </select>
          </label>
          <label className='pool-test-billing'>
            <input
              type='checkbox'
              checked={billingConfirmed}
              disabled={running}
              onChange={(event) => setBillingConfirmed(event.target.checked)}
            />
            {t('使用账户额度计费')}
          </label>
          <button
            className={cn(
              'btn primary pool-test-trigger',
              running && 'is-testing'
            )}
            disabled={
              running ||
              !selectedModel ||
              !billingConfirmed ||
              !props.members.length
            }
            onClick={() => void start()}
          >
            {running ? (
              <LoaderCircle
                className='pool-test-spinner'
                size={15}
                aria-hidden='true'
              />
            ) : (
              <Zap size={15} aria-hidden='true' />
            )}
            {running ? t('测试中') : t('开始测试')}
            <span>{props.members.length}</span>
          </button>
        </>
      )}
      {items.length > 0 && (
        <div className='pool-test-summary' role='status' data-failed={failed}>
          <span>
            {running ? t('测试中') : failed ? t('存在异常') : t('连通正常')}
          </span>
          <PoolValue value={`${completed}/${requestedCount || items.length}`} />
          <span>
            {formatQuota(
              items.reduce((total, item) => total + item.quota_charged, 0)
            )}
          </span>
        </div>
      )}
      {startError && (
        <div className='pool-test-error' role='alert'>
          {startError}
        </div>
      )}
      {results.isError && (
        <div className='pool-test-error' role='alert'>
          <span>
            {results.error instanceof Error
              ? results.error.message
              : t('测试结果加载失败')}
          </span>
          <button
            className='btn mini'
            disabled={results.isFetching}
            onClick={() => void results.refetch()}
          >
            {t('重试')}
          </button>
        </div>
      )}
    </div>
  )
}

function CreatePanel(props: {
  onCancel: () => void
  onCreate: (
    name: string,
    strategy: 'priority' | 'score' | 'cost'
  ) => Promise<void>
}) {
  const { t } = useTranslation()
  const [submitError, setSubmitError] = useState('')
  const [name, setName] = useState('')
  const [strategy, setStrategy] = useState<'priority' | 'score' | 'cost'>(
    'priority'
  )
  const [busy, setBusy] = useState(false)
  return (
    <>
      <div className='field'>
        <label>
          <Trans i18nKey={'路由池名称'} />
        </label>
        <input
          placeholder={i18n.t('高峰保障池')}
          value={name}
          onChange={(event) => setName(event.target.value)}
        />
      </div>
      <div className='field'>
        <label>
          <Trans i18nKey={'路由方式'} />
        </label>
        <select
          value={strategy}
          onChange={(event) =>
            setStrategy(event.target.value as 'priority' | 'score' | 'cost')
          }
        >
          {Object.entries(STRATEGY_LABEL).map(([value, label]) => (
            <option key={value} value={value}>
              {i18n.t(label)}
            </option>
          ))}
        </select>
      </div>
      {submitError && (
        <p role='alert' className='text-destructive text-sm'>
          {submitError}
        </p>
      )}
      <div className='pp-foot'>
        <button className='btn mini' onClick={props.onCancel}>
          <Trans i18nKey={'取消'} />
        </button>
        <button
          className='btn mini primary'
          disabled={busy}
          onClick={async () => {
            setBusy(true)
            setSubmitError('')
            try {
              await props.onCreate(name.trim() || i18n.t('未命名池'), strategy)
            } catch (error) {
              setSubmitError(
                error instanceof Error
                  ? error.message
                  : t('路由池创建失败，请重试')
              )
            } finally {
              setBusy(false)
            }
          }}
        >
          <Trans i18nKey={'创建'} />
        </button>
      </div>
    </>
  )
}

function AutoBuildModelPicker(props: {
  models: string[]
  selected: string[]
  onChange: (models: string[]) => void
}) {
  useTranslation()
  const [open, setOpen] = useState(false)
  const selected = new Set(props.selected.map((model) => model.toLowerCase()))
  const toggle = (model: string) => {
    const key = model.toLowerCase()
    props.onChange(
      selected.has(key)
        ? props.selected.filter((item) => item.toLowerCase() !== key)
        : [...props.selected, model]
    )
  }
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <button
            type='button'
            className={cn(
              autoBuildControlClass,
              'flex items-center justify-between'
            )}
            role='combobox'
            aria-expanded={open}
          />
        }
      >
        <span className='truncate'>
          {props.selected.length
            ? i18n.t('已选 {{param0}} 个模型', {
                param0: props.selected.length,
              })
            : i18n.t('全部模型')}
        </span>
        <ChevronsUpDown size={14} className='text-muted-foreground' />
      </PopoverTrigger>
      <PopoverContent
        className='w-[min(360px,calc(100vw-32px))] p-0'
        align='start'
      >
        <Command>
          <CommandInput placeholder={i18n.t('搜索模型')} />
          <CommandList className='max-h-72'>
            <CommandEmpty>
              <Trans i18nKey={'没有匹配的模型'} />
            </CommandEmpty>
            <CommandGroup>
              {props.models.map((model) => (
                <CommandItem
                  key={model}
                  value={model}
                  data-checked={selected.has(model.toLowerCase())}
                  onSelect={() => toggle(model)}
                >
                  <span className='truncate font-mono text-xs'>{model}</span>
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
          {props.selected.length > 0 && (
            <button
              type='button'
              className='text-muted-foreground hover:text-foreground flex w-full items-center justify-center gap-1.5 border-t px-3 py-2 text-xs'
              onClick={() => props.onChange([])}
            >
              <X size={13} /> <Trans i18nKey={'清空模型筛选'} />
            </button>
          )}
        </Command>
      </PopoverContent>
    </Popover>
  )
}

function RoutePoolAutoBuildSettings(props: {
  value: MarketplaceRoutePoolAutoBuild
  groups: MarketplaceGroup[]
  busy: boolean
  onSave: (value: MarketplaceRoutePoolAutoBuild) => Promise<void>
  onRun: () => Promise<void>
}) {
  useTranslation()
  const [open, setOpen] = useState(false)
  const [value, setValue] = useState<MarketplaceRoutePoolAutoBuild>(props.value)
  const [running, setRunning] = useState(false)
  useEffect(() => setValue(props.value), [props.value])
  const models = useMemo(
    () => Array.from(new Set(props.groups.flatMap((g) => g.models))).sort(),
    [props.groups]
  )
  const selectedModels = value.models ?? (value.model ? [value.model] : [])
  const summary = value.enabled
    ? value.schedule === 'daily'
      ? i18n.t('每天 {{param0}}', { param0: value.daily_time || '03:00' })
      : i18n.t('每 {{param0}} 分钟', { param0: value.interval_minutes || 60 })
    : i18n.t('已停用')
  return (
    <div className='pool-autobuild-settings'>
      <button
        className='flex w-full items-center justify-between gap-2 text-left text-xs'
        onClick={() => setOpen((v) => !v)}
      >
        <span className='flex min-w-0 items-center gap-1.5 font-medium'>
          <Sparkles
            className='shrink-0 text-[var(--telemetry-warning)]'
            size={14}
          />
          <span>
            <Trans i18nKey={'自动重新构建'} />
          </span>
          <span
            className={cn(
              'shrink-0 rounded px-1.5 py-0.5 text-xs',
              value.enabled
                ? 'bg-[var(--telemetry-success-bg)] text-[var(--telemetry-success)]'
                : 'bg-[var(--telemetry-warning-bg)] text-[var(--telemetry-warning)]'
            )}
          >
            {value.enabled ? summary : i18n.t('未启用')}
          </span>
        </span>
        <span className='flex shrink-0 items-center gap-1 text-[var(--telemetry-warning)]'>
          {open ? i18n.t('收起参数') : i18n.t('配置参数')}
          {open ? <ChevronUp size={14} /> : <ChevronDown size={14} />}
        </span>
      </button>
      {open && (
        <div className='mt-3 grid grid-cols-2 gap-2 text-xs'>
          {value.next_build_at && (
            <p className='text-muted-foreground col-span-2'>
              {i18n.t('下次执行：{{param0}}', {
                param0: new Date(value.next_build_at).toLocaleString(
                  getIntlLocale()
                ),
              })}
            </p>
          )}
          <label className='col-span-2 flex items-center gap-2'>
            <input
              type='checkbox'
              checked={value.enabled}
              onChange={(e) =>
                setValue({ ...value, enabled: e.target.checked })
              }
            />
            <Trans i18nKey={'启用自动重新构建'} />
          </label>
          <label className='col-span-2 flex items-center gap-2'>
            <input
              type='checkbox'
              checked={value.credit_pack_only ?? false}
              disabled={props.busy}
              onChange={(event) =>
                setValue({ ...value, credit_pack_only: event.target.checked })
              }
            />
            <Trans i18nKey={'仅支持套餐'} />
          </label>
          <label>
            <Trans i18nKey={'调度方式'} />
            <select
              className={autoBuildControlClass}
              value={value.schedule}
              onChange={(e) =>
                setValue({
                  ...value,
                  schedule: e.target.value as 'interval' | 'daily',
                })
              }
            >
              <option value='interval'>
                <Trans i18nKey={'固定间隔'} />
              </option>
              <option value='daily'>
                <Trans i18nKey={'每天指定时间'} />
              </option>
            </select>
          </label>
          {value.schedule === 'daily' ? (
            <label>
              <Trans i18nKey={'执行时间'} />
              <input
                className={autoBuildControlClass}
                type='time'
                value={value.daily_time || '03:00'}
                onChange={(e) =>
                  setValue({ ...value, daily_time: e.target.value })
                }
              />
            </label>
          ) : (
            <label>
              <Trans i18nKey={'间隔（分钟）'} />
              <input
                className={autoBuildControlClass}
                type='number'
                min={15}
                max={10080}
                value={value.interval_minutes || 60}
                onChange={(e) =>
                  setValue({
                    ...value,
                    interval_minutes: Number(e.target.value),
                  })
                }
              />
            </label>
          )}
          <label className='col-span-2'>
            <Trans i18nKey={'模型筛选（可多选，匹配任一模型）'} />
            <AutoBuildModelPicker
              models={models}
              selected={selectedModels}
              onChange={(nextModels) =>
                setValue({ ...value, model: undefined, models: nextModels })
              }
            />
          </label>
          <label>
            <Trans i18nKey={'主选数量'} />
            <input
              className={autoBuildControlClass}
              type='number'
              min={1}
              max={10}
              value={value.size || 3}
              onChange={(e) =>
                setValue({ ...value, size: Number(e.target.value) })
              }
            />
          </label>
          <label>
            <Trans i18nKey={'探索位'} />
            <input
              className={autoBuildControlClass}
              type='number'
              min={0}
              max={10}
              value={value.explore || 0}
              onChange={(e) =>
                setValue({ ...value, explore: Number(e.target.value) })
              }
            />
          </label>
          <div className='border-border bg-background/70 col-span-2 rounded-md border p-3'>
            <div className='mb-2 font-medium'>
              <Trans i18nKey={'评分权重'} />
            </div>
            {WEIGHTS.map((weight) => {
              const key = `${weight.key}_weight` as const
              const current = value[key] ?? weight.defaultValue
              const weightTotal = WEIGHTS.reduce(
                (sum, item) =>
                  sum +
                  (value[`${item.key}_weight` as const] ?? item.defaultValue),
                0
              )
              return (
                <div
                  className='grid grid-cols-[minmax(0,1fr)_1fr_42px] items-center gap-2 py-1'
                  key={weight.key}
                >
                  <span className='truncate'>{i18n.t(weight.label)}</span>
                  <input
                    type='range'
                    min={0}
                    max={100}
                    value={current}
                    onChange={(event) =>
                      setValue({ ...value, [key]: Number(event.target.value) })
                    }
                  />
                  <b className='text-right tabular-nums'>
                    {weightTotal > 0
                      ? Math.round((current / weightTotal) * 100)
                      : 0}
                    %
                  </b>
                </div>
              )
            })}
          </div>
          <div className='col-span-2 flex justify-end gap-2 pt-1'>
            <button
              className='btn mini'
              disabled={props.busy || running}
              onClick={async () => {
                setRunning(true)
                try {
                  await props.onSave(value)
                  await props.onRun()
                } catch (error) {
                  toast.error(
                    error instanceof Error
                      ? error.message
                      : i18n.t('生成路由池失败')
                  )
                } finally {
                  setRunning(false)
                }
              }}
            >
              <Trans i18nKey={'立即构建'} />
            </button>
            <button
              className='btn mini primary'
              disabled={props.busy || running}
              onClick={async () => {
                try {
                  await props.onSave(value)
                } catch (error) {
                  toast.error(
                    error instanceof Error
                      ? error.message
                      : i18n.t('生成路由池失败')
                  )
                }
              }}
            >
              <Trans i18nKey={'保存计划'} />
            </button>
          </div>
          {value.last_error && (
            <p className='text-destructive col-span-2'>{value.last_error}</p>
          )}
        </div>
      )}
    </div>
  )
}

const WEIGHTS = [
  { key: 'consumer', label: '平均实扣/1M tokens（低好）', defaultValue: 25 },
  { key: 'success', label: '成功率', defaultValue: 35 },
  { key: 'ttft', label: '首字（低好）', defaultValue: 20 },
  { key: 'cache', label: '缓存命中率', defaultValue: 20 },
] as const

type WeightKey = (typeof WEIGHTS)[number]['key']

function selectedAverageConsumerAmount(
  group: MarketplaceGroup,
  models: string[]
): number | null {
  if (models.length === 0)
    return group.avg_consumer_amount > 0 ? group.avg_consumer_amount : null
  const entries = Object.entries(group.avg_consumer_amount_by_model ?? {})
  const amounts = models
    .map(
      (target) =>
        entries.find(
          ([model]) => model.toLowerCase() === target.toLowerCase()
        )?.[1]
    )
    .filter(
      (amount): amount is number => typeof amount === 'number' && amount > 0
    )
  return amounts.length
    ? amounts.reduce((sum, amount) => sum + amount, 0) / amounts.length
    : null
}

function AutoBuildPanel(props: {
  groups: MarketplaceGroup[]
  onCancel: () => void
  onGenerate: (
    name: string,
    groupIds: string[],
    strategy: 'priority' | 'score' | 'cost',
    autoBuild: Partial<MarketplaceRoutePoolAutoBuild>
  ) => Promise<void>
}) {
  useTranslation()
  const [name, setName] = useState(() => i18n.t('自动池'))
  const [weights, setWeights] = useState<Record<WeightKey, number>>({
    consumer: 25,
    success: 35,
    ttft: 20,
    cache: 20,
  })
  const [size, setSize] = useState(3)
  const [explore, setExplore] = useState(1)
  const [models, setModels] = useState<string[]>([])
  const [creditPackOnly, setCreditPackOnly] = useState(false)
  const [busy, setBusy] = useState(false)

  const total =
    Object.values(weights).reduce((sum, value) => sum + value, 0) || 1

  const scored = useMemo(() => {
    const candidates = props.groups.filter(
      (group) =>
        group.source_type === 'marketplace_user' &&
        (!creditPackOnly || group.credit_pack_supported === true) &&
        group.request_count > 0 &&
        (group.lifecycle_status === 'active' ||
          group.lifecycle_status === 'degraded') &&
        group.verification_status === 'passed' &&
        group.models.length > 0 &&
        (models.length === 0 ||
          group.models.some((model) =>
            models.some(
              (selected) => selected.toLowerCase() === model.toLowerCase()
            )
          ))
    )
    if (!candidates.length) return []
    const values = {
      consumer: candidates
        .map((g) => selectedAverageConsumerAmount(g, models))
        .filter((value): value is number => value != null),
      success: candidates.map((g) => g.success_rate * 100),
      ttft: candidates.map((g) => g.avg_ttft_ms),
      cache: candidates.map((g) => g.cache_hit_rate * 100),
    }
    const norm = (value: number, arr: number[]) => {
      const min = Math.min(...arr)
      const max = Math.max(...arr)
      return max === min ? 1 : (value - min) / (max - min)
    }
    return candidates
      .map((group) => {
        const consumerAmount = selectedAverageConsumerAmount(group, models)
        const consumerScore =
          consumerAmount == null || values.consumer.length === 0
            ? 0
            : 0.05 + 0.95 * (1 - norm(consumerAmount, values.consumer))
        const score =
          ((weights.consumer * consumerScore +
            weights.success * norm(group.success_rate * 100, values.success) +
            weights.ttft * (1 - norm(group.avg_ttft_ms, values.ttft)) +
            weights.cache * norm(group.cache_hit_rate * 100, values.cache)) /
            total) *
          100
        return { group, score }
      })
      .sort((a, b) => b.score - a.score)
  }, [props.groups, weights, total, models, creditPackOnly])

  const exploration = useMemo(
    () =>
      props.groups.filter(
        (group) =>
          group.source_type === 'marketplace_user' &&
          (!creditPackOnly || group.credit_pack_supported === true) &&
          (group.lifecycle_status === 'active' ||
            group.lifecycle_status === 'degraded') &&
          group.verification_status === 'passed' &&
          group.models.length > 0 &&
          (group.observing || group.request_count === 0) &&
          (models.length === 0 ||
            group.models.some((model) =>
              models.some(
                (selected) => selected.toLowerCase() === model.toLowerCase()
              )
            ))
      ),
    [props.groups, models, creditPackOnly]
  )

  const main = scored.slice(0, Math.max(1, size))
  const explorer = exploration.slice(0, Math.max(1, explore))

  return (
    <>
      <div className='field'>
        <label>
          <Trans i18nKey={'按模型筛选（可多选）'} />
        </label>
        <AutoBuildModelPicker
          models={Array.from(
            new Set(props.groups.flatMap((group) => group.models))
          ).sort()}
          selected={models}
          onChange={setModels}
        />
      </div>
      <label className='mb-3 flex items-center gap-2 text-xs'>
        <input
          type='checkbox'
          checked={creditPackOnly}
          disabled={busy}
          onChange={(event) => setCreditPackOnly(event.target.checked)}
        />
        <Trans i18nKey={'仅支持套餐'} />
      </label>
      <div className='field'>
        <label>
          <Trans i18nKey={'路由池名称'} />
        </label>
        <input value={name} onChange={(event) => setName(event.target.value)} />
      </div>
      <div className='field'>
        <label>
          <Trans i18nKey={'评分占比'} />
        </label>
        {WEIGHTS.map((weight) => (
          <div className='wrow' key={weight.key}>
            <span>{i18n.t(weight.label)}</span>
            <input
              type='range'
              min={0}
              max={50}
              value={weights[weight.key]}
              onChange={(event) =>
                setWeights((current) => ({
                  ...current,
                  [weight.key]: Number(event.target.value),
                }))
              }
            />
            <b>{Math.round((weights[weight.key] / total) * 100)}%</b>
          </div>
        ))}
      </div>
      <div className='field'>
        <div className='row'>
          <div>
            <label>
              <Trans i18nKey={'数量'} />
            </label>
            <input
              type='number'
              min={1}
              max={6}
              value={size}
              onChange={(event) =>
                setSize(
                  Math.max(1, Math.min(6, Number(event.target.value) || 1))
                )
              }
            />
          </div>
          <div>
            <label>
              <Trans i18nKey={'探索位（≥1）'} />
            </label>
            <input
              type='number'
              min={1}
              value={explore}
              onChange={(event) =>
                setExplore(Math.max(1, Number(event.target.value) || 1))
              }
            />
          </div>
        </div>
      </div>
      <div className='prevbox'>
        <div className='pool-head'>
          <span>
            <Trans i18nKey={'命中预览 · 主选'} /> {main.length}{' '}
            <Trans i18nKey={'+ 探索'} /> {explorer.length}
          </span>
          <span className='c2'>{i18n.t('PREVIEW')}</span>
        </div>
        {main.length ? (
          main.map((entry, index) => (
            <div className='prev-row' key={entry.group.id}>
              <span className='rk'>{String(index + 1).padStart(2, '0')}</span>
              <span className='nm'>
                {i18n.t(entry.group.system_display_name)}
              </span>
              <span className='mt'>
                {selectedAverageConsumerAmount(entry.group, models) != null
                  ? `${formatQuota(selectedAverageConsumerAmount(entry.group, models) ?? 0)}/1M`
                  : i18n.t('实扣暂无')}{' '}
                · {pct(entry.group.success_rate)}% ·{' '}
                {sec(entry.group.avg_ttft_ms)}s
              </span>
              <span className='bar'>
                <i style={{ width: `${Math.max(8, entry.score)}%` }} />
              </span>
            </div>
          ))
        ) : (
          <div className='prev-empty'>
            <Trans i18nKey={'无符合条件的分组'} />
          </div>
        )}
        {explorer.length > 0 ? (
          <div>
            <div className='pool-head'>
              <span>
                <Trans i18nKey={'探索位 · 观测中与无流量分组'} />
              </span>
            </div>
            {explorer.map((group, index) => (
              <div className='prev-row' key={group.id}>
                <span className='rk'>{String(index + 1).padStart(2, '0')}</span>
                <span className='nm'>
                  {i18n.t(group.system_display_name)}
                  <span className='tag'>
                    {group.observing ? i18n.t('观测中') : i18n.t('无流量')}
                  </span>
                </span>
                <span className='mt'>
                  <Trans i18nKey={'实扣待观测'} />
                </span>
                <span className='bar'>
                  <i style={{ width: '18%' }} />
                </span>
              </div>
            ))}
          </div>
        ) : null}
      </div>
      <div className='pp-foot'>
        <button className='btn mini' onClick={props.onCancel}>
          <Trans i18nKey={'取消'} />
        </button>
        <button
          className='btn mini primary'
          disabled={busy || (!main.length && !explorer.length)}
          onClick={async () => {
            setBusy(true)
            try {
              await props.onGenerate(
                name.trim() || i18n.t('自动池'),
                [
                  ...main.map((entry) => entry.group.id),
                  ...explorer.map((group) => group.id),
                ],
                'priority',
                {
                  enabled: false,
                  credit_pack_only: creditPackOnly,
                  models,
                  size,
                  explore,
                  consumer_weight: weights.consumer,
                  success_weight: weights.success,
                  ttft_weight: weights.ttft,
                  cache_weight: weights.cache,
                }
              )
            } catch (error) {
              toast.error(
                error instanceof Error
                  ? error.message
                  : i18n.t('生成路由池失败')
              )
            } finally {
              setBusy(false)
            }
          }}
        >
          <Sparkles size={13} />
          <Trans i18nKey={'生成路由池'} />
        </button>
      </div>
    </>
  )
}
