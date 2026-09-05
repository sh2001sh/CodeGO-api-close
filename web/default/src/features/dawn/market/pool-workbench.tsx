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
  GripVertical,
  Info,
  Plus,
  Sparkles,
  Waypoints,
  X,
} from 'lucide-react'
import { toast } from 'sonner'
import { getMarketplaceRoutePools } from '@/features/marketplace/api'
import {
  useMarketplaceAutoRoutePool,
  useMarketplaceAutoRoutePoolUpdate,
  useMarketplaceRoutePool,
  useMarketplaceRoutePoolCreate,
  useMarketplaceRoutePoolDelete,
  useMarketplaceRoutePoolUpdate,
} from '@/features/marketplace/hooks'
import type {
  MarketplaceAutoRoutePoolConfig,
  MarketplaceGroup,
} from '@/features/marketplace/types'
import { pct, sec } from '../lib/format'

const STRATEGY_LABEL: Record<string, string> = {
  priority: '排序轮询',
  score: '评分优先',
  cost: '最低倍率',
}

const AUTO_ID = 'auto'

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
  })
  const autoPool = useMarketplaceAutoRoutePool(authed)
  const poolDetail = useMarketplaceRoutePool(
    authed && activePoolID !== AUTO_ID ? activePoolID : ''
  )
  const createPool = useMarketplaceRoutePoolCreate()
  const updatePool = useMarketplaceRoutePoolUpdate()
  const deletePool = useMarketplaceRoutePoolDelete()
  const updateAutoPool = useMarketplaceAutoRoutePoolUpdate()

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
    if (activePoolID) return
    if (poolOptions.length) onActivePoolChange(poolOptions[0].id)
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
      await updateAutoPool.mutateAsync({ groupIds: orderedIds })
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
              ? '自动构建'
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
            <div className='pp-note'>
              <Info size={12} />
              登录后可创建与编辑路由池
            </div>
          </>
        ) : mode === 'create' ? (
          <CreatePanel
            onCancel={() => onModeChange('pool')}
            onCreate={async (name, strategy) => {
              const pool = await createPool.mutateAsync(name)
              await updatePool.mutateAsync({
                id: pool.id,
                groupIds: [],
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
              const pool = await createPool.mutateAsync(name)
              await updatePool.mutateAsync({
                id: pool.id,
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
                  title='自动构建'
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
                      <i className={`dot${member.observing ? 'w' : ''}`} />
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
            <div className='pp-note'>
              <Info size={12} />
              排序即调度顺序；指标取窗口均值。
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
      <div className='pp-foot'>
        <button className='btn mini' onClick={props.onCancel}>
          取消
        </button>
        <button
          className='btn mini primary'
          disabled={busy}
          onClick={async () => {
            setBusy(true)
            try {
              await props.onCreate(name.trim() || '未命名池', strategy)
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

const WEIGHTS = [
  { key: 'multiplier', label: '倍率（低好）' },
  { key: 'success', label: '成功率' },
  { key: 'ttft', label: '首字（低好）' },
  { key: 'cache', label: '缓存命中率' },
] as const

type WeightKey = (typeof WEIGHTS)[number]['key']

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
    multiplier: 25,
    success: 35,
    ttft: 20,
    cache: 20,
  })
  const [size, setSize] = useState(3)
  const [explore, setExplore] = useState(1)
  const [model, setModel] = useState('')
  const [busy, setBusy] = useState(false)

  const total =
    Object.values(weights).reduce((sum, value) => sum + value, 0) || 1

  const scored = useMemo(() => {
    const candidates = props.groups.filter(
      (group) =>
        group.request_count > 0 &&
        group.lifecycle_status === 'active' &&
        (!model || group.models.includes(model))
    )
    if (!candidates.length) return []
    const values = {
      multiplier: candidates.map((g) => g.multiplier),
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
        const score =
          ((weights.multiplier *
            (1 - norm(group.multiplier, values.multiplier)) +
            weights.success * norm(group.success_rate * 100, values.success) +
            weights.ttft * (1 - norm(group.avg_ttft_ms, values.ttft)) +
            weights.cache * norm(group.cache_hit_rate * 100, values.cache)) /
            total) *
          100
        return { group, score }
      })
      .sort((a, b) => b.score - a.score)
  }, [props.groups, weights, total, model])

  const exploration = useMemo(
    () =>
      props.groups.filter(
        (group) => group.observing || group.request_count === 0
      ),
    [props.groups]
  )

  const main = scored.slice(0, Math.max(1, size))
  const explorer = exploration.slice(0, Math.max(1, explore))

  return (
    <>
      <div className='field'>
        <label>按模型筛选（可选）</label>
        <select
          value={model}
          onChange={(event) => setModel(event.target.value)}
        >
          <option value=''>全部模型</option>
          {Array.from(new Set(props.groups.flatMap((group) => group.models)))
            .sort()
            .map((name) => (
              <option key={name} value={name}>
                {name}
              </option>
            ))}
        </select>
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
                {entry.group.multiplier}× · {pct(entry.group.success_rate)}% ·{' '}
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
                <span className='mt'>{group.multiplier}×</span>
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
                weights.multiplier >= 30 ? 'cost' : 'priority'
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
