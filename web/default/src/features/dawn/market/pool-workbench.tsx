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
import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import {
  BadgeCheck,
  ChevronDown,
  ChevronUp,
  ChevronsUpDown,
  GripVertical,
  Info,
  Plus,
  Sparkles,
  Waypoints,
  X,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
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
import { getMarketplaceRoutePools } from '@/features/marketplace/api'
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
  MarketplaceGroup,
  MarketplaceRoutePoolAutoBuild,
} from '@/features/marketplace/types'
import { pct, sec } from '../lib/format'

const STRATEGY_LABEL: Record<string, string> = {
  priority: '排序轮询',
  score: '评分优先',
  cost: '最低倍率',
}

const AUTO_ID = 'auto'
const autoBuildControlClass =
  'mt-1 h-9 w-full rounded-md border border-border bg-background px-2.5 text-foreground shadow-sm outline-none focus:border-primary focus:ring-2 focus:ring-primary/20'

type PanelMode = 'pool' | 'create' | 'autobuild'

export type PoolPanelMode = PanelMode

export function PoolWorkbench(props: {
  authed: boolean
  groups: MarketplaceGroup[]
  activePoolID: string
  onActivePoolChange: (id: string) => void
  mode: PoolPanelMode
  onModeChange: (mode: PoolPanelMode) => void
}) {
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
            name: 'AUTO 池',
            count: autoPool.data.selected_count,
          },
        ]
      : []
    return [...auto, ...custom]
  }, [pools.data, autoPool.data])

  useEffect(() => {
    if (!authed) return
    if (activePoolID && poolOptions.some((pool) => pool.id === activePoolID))
      return
    onActivePoolChange(poolOptions[0]?.id ?? '')
  }, [authed, activePoolID, poolOptions, onActivePoolChange])

  const isAuto = activePoolID === AUTO_ID
  const persistedMemberIds = useMemo(() => {
    const items = (isAuto ? autoPool.data?.items : poolDetail.data?.items) ?? []
    return items
      .filter((item) => item.selected)
      .sort((a, b) => a.priority - b.priority)
      .map((item) => item.group_id)
  }, [isAuto, autoPool.data, poolDetail.data])
  const [draftMemberIds, setDraftMemberIds] = useState<string[]>([])
  useEffect(() => {
    setDraftMemberIds(persistedMemberIds)
  }, [activePoolID, persistedMemberIds.join(',')])
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

  const saveMemberOrder = async (orderedIds: string[]) => {
    if (isAuto) {
      await updateAutoPool.mutateAsync({
        groupIds: orderedIds,
        config: currentConfig,
      })
      toast.success('AUTO 池已更新')
      return
    }
    await updatePool.mutateAsync({
      id: activePoolID,
      groupIds: orderedIds,
      config: currentConfig,
    })
    toast.success('路由池已更新')
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

  const setStrategy = async (strategy: string) => {
    if (isAuto) {
      await updateAutoPool.mutateAsync({
        groupIds: members.map((m) => m.id),
        config: { strategy: strategy as 'priority' | 'score' | 'cost' },
      })
    } else {
      await updatePool.mutateAsync({
        id: activePoolID,
        groupIds: members.map((m) => m.id),
        config: { strategy: strategy as 'priority' | 'score' | 'cost' },
      })
    }
    toast.success('路由方式已更新')
  }

  const rename = async (name: string) => {
    if (isAuto || !name.trim()) return
    await updatePool.mutateAsync({
      id: activePoolID,
      name: name.trim(),
      groupIds: members.map((m) => m.id),
      config: currentConfig,
    })
  }

  const removePool = async () => {
    if (isAuto) return
    await deletePool.mutateAsync(activePoolID)
    onActivePoolChange('')
    toast.success('路由池已删除')
  }

  return (
    <aside className='poolpanel'>
      <div className='ph'>
        <div className='kick'>
          <span className='n'>M·02</span>
          ROUTE POOL
        </div>
        <h3>
          {mode === 'create'
            ? '新建路由池'
            : mode === 'autobuild'
              ? '智能新建池'
              : isAuto
                ? 'AUTO 池'
                : (poolDetail.data?.name ?? '路由池工作台')}
        </h3>
        <div className='psub'>
          {mode === 'pool'
            ? `POOL · ${members.length} 分组 · ${config ? (STRATEGY_LABEL[config.strategy] ?? config.strategy) : '—'}`
            : mode === 'create'
              ? 'CREATE'
              : 'AUTO BUILD'}
        </div>
      </div>
      <div className='pb'>
        {!authed ? (
          <>
            <div className='compare'>
              <div className='col'>
                <h5>
                  <BadgeCheck size={11} />
                  池构成
                </h5>
                <div className='crow'>
                  分组 <b>0</b>
                </div>
                <div className='crow'>
                  方式 <b>—</b>
                </div>
                <div className='crow'>
                  类型 <b>AUTO</b>
                </div>
              </div>
              <div className='col pool'>
                <h5>
                  <Waypoints size={11} />
                  池内均值
                </h5>
                <div className='crow'>
                  成功 <b>—</b>
                </div>
                <div className='crow'>
                  首字 <b>—</b>
                </div>
                <div className='crow'>
                  缓存 <b>—</b>
                </div>
              </div>
            </div>
            <div className='poolbox'>
              <div className='pool-head'>
                <span>分组 · 0</span>
                <span className='c2'>ORDER</span>
              </div>
              <div className='pp-empty'>
                <Waypoints size={26} />
                <span>登录后管理路由池</span>
                <Link className='btn mini primary' to='/sign-in'>
                  登录 / 注册
                </Link>
              </div>
            </div>
            <div
              className='pp-note'
              style={{
                display: 'flex',
                alignItems: 'center',
                whiteSpace: 'nowrap',
              }}
            >
              <Info size={12} />
              登录后可创建与编辑路由池
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
              toast.success('路由池已创建')
            }}
          />
        ) : mode === 'autobuild' ? (
          <AutoBuildPanel
            groups={groups}
            onCancel={() => onModeChange('pool')}
            onGenerate={async (name, groupIds, strategy) => {
              let generatedName = name.trim() || '自动池'
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
              })
              onActivePoolChange(pool.id)
              onModeChange('pool')
              toast.success(`已生成路由池 · ${groupIds.length} 分组`)
            }}
          />
        ) : (
          <>
            <div className='toolbar'>
              <div className='row'>
                <select
                  className='iname'
                  value={activePoolID}
                  onChange={(event) => onActivePoolChange(event.target.value)}
                >
                  {poolOptions.map((option) => (
                    <option key={option.id} value={option.id}>
                      {option.name} · {option.count} 组
                    </option>
                  ))}
                </select>
                <button
                  className='btn mini'
                  title='新建路由池'
                  onClick={() => onModeChange('create')}
                >
                  <Plus size={14} />
                </button>
                <button
                  className='btn mini'
                  title='智能新建一个路由池'
                  onClick={() => onModeChange('autobuild')}
                >
                  <Sparkles size={14} />
                </button>
              </div>
              <div className='row'>
                <input
                  className='iname'
                  defaultValue={isAuto ? 'AUTO 池' : poolDetail.data?.name}
                  key={`${activePoolID}-${poolDetail.data?.name}`}
                  disabled={isAuto}
                  onBlur={(event) => void rename(event.target.value)}
                />
                <select
                  value={config?.strategy ?? 'priority'}
                  onChange={(event) => void setStrategy(event.target.value)}
                  title='路由方式'
                >
                  {Object.entries(STRATEGY_LABEL).map(([value, label]) => (
                    <option key={value} value={value}>
                      {label}
                    </option>
                  ))}
                </select>
                {!isAuto && (
                  <button
                    className='btn mini'
                    title='删除池'
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
                busy={runAutoBuild.isPending || updatePool.isPending}
                onRun={async () => {
                  const pool = await runAutoBuild.mutateAsync(activePoolID)
                  toast.success(`自动构建完成 · ${pool.selected_count} 分组`)
                }}
                onSave={async (autoBuild) => {
                  await updatePool.mutateAsync({
                    id: activePoolID,
                    groupIds: draftMemberIds,
                    config: currentConfig,
                    autoBuild,
                  })
                  toast.success('自动构建计划已保存')
                }}
              />
            )}

            <div className='compare'>
              <div className='col'>
                <h5>
                  <BadgeCheck size={11} />
                  池构成
                </h5>
                <div className='crow'>
                  分组 <b>{members.length}</b>
                </div>
                <div className='crow'>
                  方式{' '}
                  <b>
                    {config
                      ? (STRATEGY_LABEL[config.strategy] ?? config.strategy)
                      : '—'}
                  </b>
                </div>
                <div className='crow'>
                  类型 <b>{isAuto ? 'AUTO' : '手动'}</b>
                </div>
              </div>
              <div className='col pool'>
                <h5>
                  <Waypoints size={11} />
                  池内均值
                </h5>
                <div className='crow'>
                  成功{' '}
                  <b
                    className={
                      weighted &&
                      weighted.success != null &&
                      weighted.success >= 0.98
                        ? 'ok'
                        : 'bad'
                    }
                  >
                    {weighted?.success != null
                      ? `${pct(weighted.success)}%`
                      : '—'}
                  </b>
                </div>
                <div className='crow'>
                  首字{' '}
                  <b
                    className={
                      weighted && weighted.ttft != null && weighted.ttft <= 600
                        ? 'ok'
                        : 'bad'
                    }
                  >
                    {weighted?.ttft != null ? `${sec(weighted.ttft)}s` : '—'}
                  </b>
                </div>
                <div className='crow'>
                  缓存{' '}
                  <b>
                    {weighted?.cache != null
                      ? `${pct(weighted.cache, 0)}%`
                      : '—'}
                  </b>
                </div>
              </div>
            </div>

            <div className='poolbox'>
              <div className='pool-head'>
                <span>分组 · {members.length}</span>
                <span className='c2'>ORDER</span>
              </div>
              {members.length ? (
                members.map((member, index) => (
                  <div className='pool-item' key={member.id}>
                    <div className='r1'>
                      <GripVertical size={13} color='var(--dawn-ink2)' />
                      <i className={cn('dot', member.observing && 'w')} />
                      <span className='cname'>{member.name}</span>
                      <span className='csrc'>{member.source}</span>
                      <span className='ord'>
                        <button
                          disabled={index === 0}
                          onClick={() => void move(index, -1)}
                          title='上移'
                        >
                          <ChevronUp size={13} />
                        </button>
                        <button
                          disabled={index === members.length - 1}
                          onClick={() => void move(index, 1)}
                          title='下移'
                        >
                          <ChevronDown size={13} />
                        </button>
                        <button
                          onClick={() => void remove(member.id)}
                          title='移出'
                        >
                          <X size={13} />
                        </button>
                      </span>
                    </div>
                    <div className='r2'>
                      <div>
                        倍率<b>{member.multiplier}×</b>
                      </div>
                      <div>
                        成功
                        <b
                          className={
                            member.successRate != null &&
                            member.successRate < 0.98
                              ? 'warn'
                              : ''
                          }
                        >
                          {member.successRate != null
                            ? `${pct(member.successRate)}%`
                            : '—'}
                        </b>
                      </div>
                      <div>
                        首字
                        <b
                          className={
                            member.ttft != null && member.ttft > 600
                              ? 'warn'
                              : ''
                          }
                        >
                          {member.ttft != null ? `${sec(member.ttft)}s` : '—'}
                        </b>
                      </div>
                      <div>
                        缓存
                        <b>
                          {member.cache != null
                            ? `${pct(member.cache, 0)}%`
                            : '—'}
                        </b>
                      </div>
                    </div>
                  </div>
                ))
              ) : (
                <div className='pp-empty'>
                  <Waypoints size={26} />
                  <span>空池 · 在市场条目点「加入当前池」</span>
                </div>
              )}
            </div>
            {isDirty && (
              <div className='pp-foot'>
                <span className='sub2'>成员调整尚未保存</span>
                <button
                  className='btn mini primary'
                  disabled={updatePool.isPending || updateAutoPool.isPending}
                  onClick={() => void saveMemberOrder(draftMemberIds)}
                >
                  保存更改
                </button>
              </div>
            )}
            <div className='text-muted-foreground mt-3 flex items-start gap-1.5 text-xs leading-5'>
              <Info size={12} className='mt-1 shrink-0' />
              <span>排序即调度顺序；指标取窗口均值。</span>
            </div>
          </>
        )}
      </div>
    </aside>
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
        <label>路由池名称</label>
        <input
          placeholder='高峰保障池'
          value={name}
          onChange={(event) => setName(event.target.value)}
        />
      </div>
      <div className='field'>
        <label>路由方式</label>
        <select
          value={strategy}
          onChange={(event) =>
            setStrategy(event.target.value as 'priority' | 'score' | 'cost')
          }
        >
          {Object.entries(STRATEGY_LABEL).map(([value, label]) => (
            <option key={value} value={value}>
              {label}
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
          取消
        </button>
        <button
          className='btn mini primary'
          disabled={busy}
          onClick={async () => {
            setBusy(true)
            setSubmitError('')
            try {
              await props.onCreate(name.trim() || '未命名池', strategy)
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
          创建
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
            ? `已选 ${props.selected.length} 个模型`
            : '全部模型'}
        </span>
        <ChevronsUpDown size={14} className='text-muted-foreground' />
      </PopoverTrigger>
      <PopoverContent
        className='w-[min(360px,calc(100vw-32px))] p-0'
        align='start'
      >
        <Command>
          <CommandInput placeholder='搜索模型' />
          <CommandList className='max-h-72'>
            <CommandEmpty>没有匹配的模型</CommandEmpty>
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
              <X size={13} /> 清空模型筛选
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
  const [open, setOpen] = useState(false)
  const [value, setValue] = useState<MarketplaceRoutePoolAutoBuild>(props.value)
  useEffect(() => setValue(props.value), [props.value])
  const models = useMemo(
    () => Array.from(new Set(props.groups.flatMap((g) => g.models))).sort(),
    [props.groups]
  )
  const selectedModels = value.models ?? (value.model ? [value.model] : [])
  const summary = value.enabled
    ? value.schedule === 'daily'
      ? `每天 ${value.daily_time || '03:00'}`
      : `每 ${value.interval_minutes || 60} 分钟`
    : '已停用'
  return (
    <div className='mb-3 rounded-md border border-amber-500/45 bg-amber-500/5 px-3 py-2 shadow-sm'>
      <button
        className='flex w-full items-center justify-between gap-2 text-left text-xs'
        onClick={() => setOpen((v) => !v)}
      >
        <span className='flex min-w-0 items-center gap-1.5 font-medium'>
          <Sparkles className='shrink-0 text-amber-600' size={14} />
          <span>自动重新构建</span>
          <span
            className={cn(
              'shrink-0 rounded px-1.5 py-0.5 text-[10px]',
              value.enabled
                ? 'bg-emerald-500/15 text-emerald-700'
                : 'bg-amber-500/15 text-amber-700'
            )}
          >
            {value.enabled ? summary : '未启用'}
          </span>
        </span>
        <span className='flex shrink-0 items-center gap-1 text-amber-700'>
          {open ? '收起参数' : '配置参数'}
          {open ? <ChevronUp size={14} /> : <ChevronDown size={14} />}
        </span>
      </button>
      {open && (
        <div className='mt-3 grid grid-cols-2 gap-2 text-xs'>
          <p className='text-muted-foreground col-span-2'>
            定时按模型、主选数量和探索位重新替换当前池成员。
            {value.next_build_at
              ? ` 下次执行：${new Date(value.next_build_at).toLocaleString()}`
              : ''}
          </p>
          <label className='col-span-2 flex items-center gap-2'>
            <input
              type='checkbox'
              checked={value.enabled}
              onChange={(e) =>
                setValue({ ...value, enabled: e.target.checked })
              }
            />
            启用自动重新构建
          </label>
          <label>
            调度方式
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
              <option value='interval'>固定间隔</option>
              <option value='daily'>每天指定时间</option>
            </select>
          </label>
          {value.schedule === 'daily' ? (
            <label>
              执行时间
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
              间隔（分钟）
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
            模型筛选（可多选，匹配任一模型）
            <AutoBuildModelPicker
              models={models}
              selected={selectedModels}
              onChange={(nextModels) =>
                setValue({ ...value, model: undefined, models: nextModels })
              }
            />
          </label>
          <label>
            主选数量
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
            探索位
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
            <div className='mb-2 font-medium'>评分权重</div>
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
                  <span className='truncate'>{weight.label}</span>
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
              disabled={props.busy}
              onClick={() => void props.onRun()}
            >
              立即构建
            </button>
            <button
              className='btn mini primary'
              disabled={props.busy}
              onClick={() => void props.onSave(value)}
            >
              保存计划
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
    strategy: 'priority' | 'score' | 'cost'
  ) => Promise<void>
}) {
  const [name, setName] = useState('自动池')
  const [weights, setWeights] = useState<Record<WeightKey, number>>({
    consumer: 25,
    success: 35,
    ttft: 20,
    cache: 20,
  })
  const [size, setSize] = useState(3)
  const [explore, setExplore] = useState(1)
  const [models, setModels] = useState<string[]>([])
  const [busy, setBusy] = useState(false)

  const total =
    Object.values(weights).reduce((sum, value) => sum + value, 0) || 1

  const scored = useMemo(() => {
    const candidates = props.groups.filter(
      (group) =>
        group.source_type === 'marketplace_user' &&
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
  }, [props.groups, weights, total, models])

  const exploration = useMemo(
    () =>
      props.groups.filter(
        (group) =>
          group.source_type === 'marketplace_user' &&
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
    [props.groups, models]
  )

  const main = scored.slice(0, Math.max(1, size))
  const explorer = exploration.slice(0, Math.max(1, explore))

  return (
    <>
      <div className='field'>
        <label>按模型筛选（可多选）</label>
        <AutoBuildModelPicker
          models={Array.from(
            new Set(props.groups.flatMap((group) => group.models))
          ).sort()}
          selected={models}
          onChange={setModels}
        />
      </div>
      <div className='field'>
        <label>路由池名称</label>
        <input value={name} onChange={(event) => setName(event.target.value)} />
      </div>
      <div className='field'>
        <label>评分占比</label>
        {WEIGHTS.map((weight) => (
          <div className='wrow' key={weight.key}>
            <span>{weight.label}</span>
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
            <label>数量</label>
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
            <label>探索位（≥1）</label>
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
            命中预览 · 主选 {main.length} + 探索 {explorer.length}
          </span>
          <span className='c2'>PREVIEW</span>
        </div>
        {main.length ? (
          main.map((entry, index) => (
            <div className='prev-row' key={entry.group.id}>
              <span className='rk'>{String(index + 1).padStart(2, '0')}</span>
              <span className='nm'>{entry.group.system_display_name}</span>
              <span className='mt'>
                {selectedAverageConsumerAmount(entry.group, models) != null
                  ? `${formatQuota(selectedAverageConsumerAmount(entry.group, models) ?? 0)}/1M`
                  : '实扣暂无'}{' '}
                · {pct(entry.group.success_rate)}% ·{' '}
                {sec(entry.group.avg_ttft_ms)}s
              </span>
              <span className='bar'>
                <i style={{ width: `${Math.max(8, entry.score)}%` }} />
              </span>
            </div>
          ))
        ) : (
          <div className='prev-empty'>无符合条件的分组</div>
        )}
        {explorer.length > 0 ? (
          <div>
            <div className='pool-head'>
              <span>探索位 · 观测中与无流量分组</span>
            </div>
            {explorer.map((group, index) => (
              <div className='prev-row' key={group.id}>
                <span className='rk'>{String(index + 1).padStart(2, '0')}</span>
                <span className='nm'>
                  {group.system_display_name}
                  <span className='tag'>
                    {group.observing ? '观测中' : '无流量'}
                  </span>
                </span>
                <span className='mt'>实扣待观测</span>
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
          取消
        </button>
        <button
          className='btn mini primary'
          disabled={busy || (!main.length && !explorer.length)}
          onClick={async () => {
            setBusy(true)
            try {
              await props.onGenerate(
                name.trim() || '自动池',
                [
                  ...main.map((entry) => entry.group.id),
                  ...explorer.map((group) => group.id),
                ],
                'priority'
              )
            } catch (error) {
              toast.error(
                error instanceof Error ? error.message : '生成路由池失败'
              )
            } finally {
              setBusy(false)
            }
          }}
        >
          <Sparkles size={13} />
          生成路由池
        </button>
      </div>
    </>
  )
}
