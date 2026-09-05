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
import { useEffect, useMemo, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { Sparkles, Store, User, Waypoints } from 'lucide-react'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { SiteSeo } from '@/components/seo'
import {
  acceptMarketplaceGroupInvite,
  bindMarketplaceToken,
  createMarketplaceBargainRequest,
  getMarketplaceBatchTest,
  getMarketplaceRoutePools,
  startMarketplaceBatchTest,
} from '@/features/marketplace/api'
import {
  useMarketplaceAutoRoutePool,
  useMarketplaceGroups,
  useMarketplaceRoutePool,
  useMarketplaceTokens,
} from '@/features/marketplace/hooks'
import { MARKETPLACE_SOURCE_OPTIONS } from '@/features/marketplace/lib/channel-form'
import { MOCK_MARKETPLACE_GROUPS } from '@/features/marketplace/lib/mock-data'
import type {
  GroupFilters,
  MarketplaceBatchTest,
  MarketplaceGroup,
} from '@/features/marketplace/types'
import { QUOTA_TYPE_VALUES } from '@/features/pricing/constants'
import { usePricingData } from '@/features/pricing/hooks/use-pricing-data'
import { mergePricingModels } from '@/features/pricing/lib/merge-pricing-models'
import type { PricingModel } from '@/features/pricing/types'
import { DawnModal, ModalHead } from '../components/dawn-modal'
import { DawnNav } from '../components/dawn-nav'
import { DawnQueryError } from '../components/query-error'
import { MarketGroupCard } from './group-card'
import { OwnerView } from './owner-view'
import { PoolWorkbench, type PoolPanelMode } from './pool-workbench'

const DEFAULT_FILTERS: GroupFilters = {
  search: '',
  model: '',
  source: '',
  provider: '',
  status: '',
  verification: '',
  sort: 'score',
  direction: 'desc',
  window_hours: 24,
  page: 1,
  page_size: 20,
}

type Perspective = 'user' | 'owner'

export function DawnMarket() {
  const user = useAuthStore((state) => state.auth.user)
  const authed = !!user
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [perspective, setPerspective] = useState<Perspective>('user')
  const [filters, setFilters] = useState<GroupFilters>(DEFAULT_FILTERS)
  const [search, setSearch] = useState('')
  const [activePoolID, setActivePoolID] = useState('')
  const [poolPanelMode, setPoolPanelMode] = useState<PoolPanelMode>('pool')
  const [selected, setSelected] = useState<string | null>(null)
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [useGroup, setUseGroup] = useState<MarketplaceGroup | null>(null)
  const [bargainGroup, setBargainGroup] = useState<MarketplaceGroup | null>(
    null
  )
  const [testGroup, setTestGroup] = useState<MarketplaceGroup | null>(null)
  const [mockMode, setMockMode] = useState(
    () => new URLSearchParams(window.location.search).get('mock') === '1'
  )
  const inviteHandledRef = useRef(false)
  const marketListRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (filters.page <= 1) return
    marketListRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }, [filters.page])

  useEffect(() => {
    const timer = window.setTimeout(() => {
      setFilters((current) => ({ ...current, search: search.trim(), page: 1 }))
    }, 300)
    return () => window.clearTimeout(timer)
  }, [search])

  useEffect(() => {
    if (inviteHandledRef.current) return
    const token = new URLSearchParams(window.location.search).get('invite')
    if (!token) return
    inviteHandledRef.current = true
    if (!authed) {
      void navigate({
        to: '/sign-in',
        search: {
          redirect: `${window.location.pathname}${window.location.search}${window.location.hash}`,
        },
      })
      return
    }
    void acceptMarketplaceGroupInvite(token)
      .then((result) => {
        const url = new URL(window.location.href)
        url.searchParams.delete('invite')
        window.history.replaceState(
          {},
          '',
          `${url.pathname}${url.search}${url.hash}`
        )
        toast.success(`已获得分组访问权限：${result.group_name}`)
        void queryClient.invalidateQueries({ queryKey: ['marketplace-groups'] })
      })
      .catch((error) => {
        toast.error(error instanceof Error ? error.message : '邀请链接无效')
      })
  }, [authed, navigate, queryClient])

  const groupsQuery = useMarketplaceGroups(filters)
  const groups = useMemo(
    () =>
      mockMode ? MOCK_MARKETPLACE_GROUPS : (groupsQuery.data?.items ?? []),
    [groupsQuery.data, mockMode]
  )
  const pricing = usePricingData()

  const modelsByName = useMemo(() => {
    const models = mergePricingModels(
      pricing.models,
      pricing.pricedModelDetails
    )
    const byName = new Map<string, PricingModel>()
    models.forEach((model) => {
      if (model.model_name) byName.set(model.model_name, model)
    })
    return byName
  }, [pricing.models, pricing.pricedModelDetails])

  /** 智能精度：≥$1 两位、≥$0.01 四位、更小六位，去尾零（demo 风格）。 */
  const fmtUsd = (value: number) => {
    const digits = value >= 1 ? 2 : value >= 0.01 ? 4 : 6
    const out = value
      .toFixed(digits)
      .replace(/(\.\d*?)0+$/, '$1')
      .replace(/\.$/, '')
    return `$${out}`
  }

  /** 每模型完整计费：输入 / 输出 / 缓存写读（已乘分组倍率）。 */
  const modelFees = useMemo(() => {
    const result = new Map<
      string,
      Record<
        string,
        { mode: 'free' | 'percall' | 'token'; input: string; output: string; cache: string }
      >
    >()
    groups.forEach((group) => {
      const multiplier = group.multiplier || 1
      const map: Record<
        string,
        { mode: 'free' | 'percall' | 'token'; input: string; output: string; cache: string }
      > = {}
      group.models.forEach((name) => {
        const model = modelsByName.get(name)
        if (!model) return
        if (model.quota_type === QUOTA_TYPE_VALUES.TOKEN && model.model_ratio === 0) {
          map[name] = { mode: 'free', input: '免费', output: '免费', cache: '—' }
          return
        }
        if (model.quota_type === QUOTA_TYPE_VALUES.REQUEST) {
          const price = (model.model_price || 0) * multiplier
          map[name] = {
            mode: 'percall',
            input: fmtUsd(price),
            output: '按量',
            cache: '—',
          }
          return
        }
        const input = model.model_ratio * 2 * multiplier
        if (!Number.isFinite(input) || input <= 0) return
        const output = input * (model.completion_ratio || 1)
        const cacheWrite =
          model.create_cache_ratio != null && Number.isFinite(Number(model.create_cache_ratio))
            ? input * Number(model.create_cache_ratio)
            : null
        const cacheRead =
          model.cache_ratio != null && Number.isFinite(Number(model.cache_ratio))
            ? input * Number(model.cache_ratio)
            : null
        map[name] = {
          mode: 'token',
          input: fmtUsd(input),
          output: fmtUsd(output),
          cache:
            cacheRead == null && cacheWrite == null
              ? '不支持'
              : `${cacheWrite != null ? fmtUsd(cacheWrite) : '—'} / ${cacheRead != null ? fmtUsd(cacheRead) : '—'}`,
        }
      })
      result.set(group.id, map)
    })
    return result
  }, [groups, modelsByName])

  const groupPrices = useMemo(() => {
    const byName = modelsByName
    const result = new Map<
      string,
      { input: string; output: string; freeCount: number }
    >()
    groups.forEach((group) => {
      const multiplier = group.multiplier || 1
      let bestInput = Number.POSITIVE_INFINITY
      let bestOutput = Number.POSITIVE_INFINITY
      let freeCount = 0
      group.models.forEach((name) => {
        const model = byName.get(name)
        if (!model) return
        if (
          model.quota_type === QUOTA_TYPE_VALUES.TOKEN &&
          model.model_ratio === 0
        ) {
          freeCount += 1
          return
        }
        const input = model.model_ratio * 2 * multiplier
        if (!Number.isFinite(input) || input <= 0) return
        if (input < bestInput) {
          bestInput = input
          bestOutput = input * (model.completion_ratio || 1)
        }
      })
      result.set(group.id, {
        input: Number.isFinite(bestInput) ? fmtUsd(bestInput) : '—',
        output: Number.isFinite(bestOutput) ? fmtUsd(bestOutput) : '—',
        freeCount,
      })
    })
    return result
  }, [groups, pricing.models, pricing.pricedModelDetails])

  const modelPrices = useMemo(() => {
    const result = new Map<string, Record<string, string>>()
    groups.forEach((group) => {
      const map: Record<string, string> = {}
      group.models.forEach((name) => {
        const fee = modelFees.get(group.id)?.[name]
        if (fee) map[name] = fee.input
      })
      result.set(group.id, map)
    })
    return result
  }, [groups, modelFees])

  const pools = useQuery({
    queryKey: ['marketplace-route-pools'],
    queryFn: getMarketplaceRoutePools,
    enabled: authed,
    retry: false,
    staleTime: 30_000,
    refetchOnWindowFocus: false,
  })
  const autoPool = useMarketplaceAutoRoutePool(authed)
  const isAutoPool = activePoolID === 'auto'
  const poolDetail = useMarketplaceRoutePool(
    authed && !isAutoPool ? activePoolID : ''
  )

  const poolMemberIDs = useMemo(() => {
    if (isAutoPool) {
      return new Set(
        (autoPool.data?.items ?? [])
          .filter((item) => item.selected)
          .map((item) => item.group_id)
      )
    }
    return new Set(
      (poolDetail.data?.items ?? [])
        .filter((item) => item.selected)
        .map((item) => item.group_id)
    )
  }, [isAutoPool, autoPool.data, poolDetail.data])

  const activePoolName = useMemo(() => {
    if (isAutoPool) return 'AUTO 池'
    const pool = (pools.data ?? []).find((item) => item.id === activePoolID)
    return pool?.name
  }, [isAutoPool, pools.data, activePoolID])

  const joinPool = async (group: MarketplaceGroup) => {
    if (!activePoolID) {
      toast.error('先创建或选择一个路由池')
      return
    }
    const currentIDs = [...poolMemberIDs]
    if (currentIDs.includes(group.id)) return
    const nextIDs = [...currentIDs, group.id]
    try {
      if (isAutoPool) {
        const { updateMarketplaceAutoRoutePool } =
          await import('@/features/marketplace/api')
        await updateMarketplaceAutoRoutePool({ groupIds: nextIDs })
      } else {
        const { updateMarketplaceRoutePool } =
          await import('@/features/marketplace/api')
        await updateMarketplaceRoutePool({
          id: activePoolID,
          groupIds: nextIDs,
          config: { strategy: 'priority' },
        })
      }
      toast.success(`已加入路由池（${activePoolName ?? '当前池'}）`)
      await queryClient.invalidateQueries({
        queryKey: ['marketplace-route-pools'],
      })
      await queryClient.invalidateQueries({
        queryKey: ['marketplace-auto-route-pool'],
      })
    } catch (error) {
      toast.error(error instanceof Error ? error.message : '加入失败')
    }
  }

  const totalPages = Math.max(
    1,
    Math.ceil((groupsQuery.data?.total ?? 0) / filters.page_size)
  )

  return (
    <div className='dawn'>
      <SiteSeo
        title='分组市场 | Code Go'
        description='分组市场 · 倍率与实时指标'
        canonicalPath='/market'
      />
      <DawnNav />
      <main className='dawn-wrap'>
        <div className='mhead'>
          <div>
            <div className='kick'>
              <span className='n'>M·01</span>
              AI RESOURCES MARKET
            </div>
            <h1>
              万象，<em>明码标价</em>。
            </h1>
          </div>
          <div className='seg'>
            <button
              className={perspective === 'user' ? 'on' : ''}
              onClick={() => setPerspective('user')}
            >
              <User size={14} />
              使用者
            </button>
            <button
              className={perspective === 'owner' ? 'on' : ''}
              onClick={() => {
                if (!authed) {
                  toast.error('登录后管理渠道')
                  return
                }
                setPerspective('owner')
              }}
            >
              <Store size={14} />
              渠道主
            </button>
          </div>
        </div>

        {perspective === 'user' ? (
          <div className='cols'>
            <div ref={marketListRef} style={{ gridColumn: '1 / -1', scrollMarginTop: 20 }}>
              <div className='filters'>
                <input
                  placeholder='搜索分组 / 模型 / 来源'
                  value={search}
                  onChange={(event) => setSearch(event.target.value)}
                />
                <button
                  className={`fbtn${filters.source === '' ? ' on' : ''}`}
                  onClick={() =>
                    setFilters((current) => ({
                      ...current,
                      source: '',
                      page: 1,
                    }))
                  }
                >
                  全部
                </button>
                {MARKETPLACE_SOURCE_OPTIONS.map((source) => (
                  <button
                    key={source}
                    className={`fbtn${filters.source === source ? ' on' : ''}`}
                    onClick={() =>
                      setFilters((current) => ({ ...current, source, page: 1 }))
                    }
                  >
                    {source}
                  </button>
                ))}
                {import.meta.env.DEV && (
                  <button
                    className={`fbtn${mockMode ? ' on' : ''}`}
                    onClick={() => setMockMode((current) => !current)}
                  >
                    {mockMode ? '示例数据：开' : '示例数据'}
                  </button>
                )}
                <span
                  style={{
                    marginLeft: 'auto',
                    display: 'inline-flex',
                    gap: 10,
                  }}
                >
                  <button
                    className='btn primary'
                    onClick={() => {
                      if (!authed) {
                        toast.error('登录后管理路由池')
                        return
                      }
                      setPerspective('user')
                      setPoolPanelMode('create')
                    }}
                  >
                    <Waypoints size={14} />
                    新建路由池
                  </button>
                  <button
                    className='btn'
                    onClick={() => {
                      if (!authed) {
                        toast.error('登录后管理路由池')
                        return
                      }
                      setPerspective('user')
                      setPoolPanelMode('autobuild')
                    }}
                  >
                    <Sparkles size={14} />
                    自动构建
                  </button>
                </span>
              </div>
            </div>

            <div>
              {groupsQuery.isLoading ? (
                <div className='empty'>
                  <span className='eic'>
                    <Store size={20} className='animate-pulse' />
                  </span>
                  <b>市场加载中</b>
                </div>
              ) : groupsQuery.isError && !mockMode ? (
                <DawnQueryError
                  title='市场数据加载失败'
                  description='请检查网络连接后重试。'
                  onRetry={() => void groupsQuery.refetch()}
                  retrying={groupsQuery.isFetching}
                />
              ) : groups.length ? (
                <>
                  {groups.map((group) => (
                    <MarketGroupCard
                      key={group.id}
                      group={group}
                      selected={selected === group.id}
                      inPool={poolMemberIDs.has(group.id)}
                      poolName={activePoolName}
                      authed={authed}
                      expanded={expanded.has(group.id)}
                      onToggleSelect={() =>
                        setSelected((current) =>
                          current === group.id ? null : group.id
                        )
                      }
                      onToggleExpand={() =>
                        setExpanded((current) => {
                          const next = new Set(current)
                          if (next.has(group.id)) next.delete(group.id)
                          else next.add(group.id)
                          return next
                        })
                      }
                      onUse={(target) => setUseGroup(target)}
                      onBindKey={(target) => setUseGroup(target)}
                      onTest={(target) => setTestGroup(target)}
                      onBargain={(target) => setBargainGroup(target)}
                      onJoinPool={(target) => void joinPool(target)}
                      priceInfo={groupPrices.get(group.id)}
                      modelPrices={modelPrices.get(group.id)}
                      modelFees={modelFees.get(group.id)}
                    />
                  ))}
                  {totalPages > 1 && (
                    <div
                      className='gtable'
                      style={{
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'space-between',
                        padding: '12px 18px',
                      }}
                    >
                      <span
                        className='num'
                        style={{ color: 'var(--dawn-ink2)' }}
                      >
                        第 {filters.page} / {totalPages} 页
                      </span>
                      <span style={{ display: 'flex', gap: 8 }}>
                        <input
                          className='page-jump'
                          type='number'
                          min={1}
                          max={totalPages}
                          value={filters.page}
                          aria-label='跳转页码'
                          onChange={(event) => {
                            const page = Math.max(1, Math.min(totalPages, Number(event.target.value) || 1))
                            setFilters((current) => ({ ...current, page }))
                          }}
                        />
                        <button
                          className='btn mini'
                          disabled={filters.page <= 1}
                          onClick={() =>
                            setFilters((current) => ({
                              ...current,
                              page: current.page - 1,
                            }))
                          }
                        >
                          上一页
                        </button>
                        <button
                          className='btn mini'
                          disabled={filters.page >= totalPages}
                          onClick={() =>
                            setFilters((current) => ({
                              ...current,
                              page: current.page + 1,
                            }))
                          }
                        >
                          下一页
                        </button>
                      </span>
                    </div>
                  )}
                </>
              ) : (
                <div className='empty'>
                  <span className='eic'>
                    <Store size={20} />
                  </span>
                  <b>市场分组上架中</b>
                  <span>渠道检测通过后自动上架</span>
                </div>
              )}
            </div>

            <PoolWorkbench
              authed={authed}
              groups={groups}
              activePoolID={activePoolID}
              onActivePoolChange={setActivePoolID}
              mode={poolPanelMode}
              onModeChange={setPoolPanelMode}
            />
          </div>
        ) : (
          <OwnerView />
        )}
      </main>

      {useGroup && (
        <UseDialog group={useGroup} onClose={() => setUseGroup(null)} />
      )}
      {bargainGroup && (
        <BargainDialog
          group={bargainGroup}
          onClose={() => setBargainGroup(null)}
        />
      )}
      {testGroup && (
        <ConnectivityDialog
          group={testGroup}
          onClose={() => setTestGroup(null)}
        />
      )}
    </div>
  )
}

/** 绑定令牌使用分组。 */
function UseDialog(props: { group: MarketplaceGroup; onClose: () => void }) {
  const { group } = props
  const tokens = useMarketplaceTokens()
  const [tokenId, setTokenId] = useState<string>('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!tokenId && tokens.data?.length) setTokenId(String(tokens.data[0].id))
  }, [tokens.data, tokenId])

  return (
    <DawnModal open onClose={props.onClose} variant='narrow' label='使用分组'>
      <div className='m-main'>
        <ModalHead
          title={`使用 · ${group.system_display_name}`}
          onClose={props.onClose}
        />
        <div className='kv'>
          <span>倍率</span>
          <b>{group.multiplier}×</b>
        </div>
        <div className='kv'>
          <span>模型</span>
          <b>{group.models.length} 个</b>
        </div>
        <div className='field' style={{ marginTop: 12 }}>
          <label>绑定 API 令牌</label>
          <select
            value={tokenId}
            onChange={(event) => setTokenId(event.target.value)}
          >
            {(tokens.data ?? []).map((token) => (
              <option key={token.id} value={token.id}>
                {token.name}
              </option>
            ))}
          </select>
        </div>
        <div className='m-foot'>
          <button className='btn' onClick={props.onClose}>
            取消
          </button>
          <button
            className='btn primary'
            disabled={busy || !tokenId}
            onClick={async () => {
              setBusy(true)
              try {
                await bindMarketplaceToken(group.id, Number(tokenId))
                toast.success('已绑定令牌')
                props.onClose()
              } catch (error) {
                toast.error(error instanceof Error ? error.message : '绑定失败')
              } finally {
                setBusy(false)
              }
            }}
          >
            绑定并使用
          </button>
        </div>
      </div>
    </DawnModal>
  )
}

/** 发起砍价。 */
function BargainDialog(props: {
  group: MarketplaceGroup
  onClose: () => void
}) {
  const { group } = props
  const [rate, setRate] = useState('0.85')
  const [reason, setReason] = useState('')
  const [busy, setBusy] = useState(false)

  return (
    <DawnModal open onClose={props.onClose} variant='narrow' label='发起砍价'>
      <div className='m-main'>
        <ModalHead
          title={`发起砍价 · ${group.system_display_name}`}
          onClose={props.onClose}
        />
        <div className='kv'>
          <span>当前倍率</span>
          <b>{group.multiplier}×</b>
        </div>
        <div className='field' style={{ marginTop: 12 }}>
          <label>期望倍率</label>
          <input
            type='number'
            step='0.05'
            min='0.1'
            max={group.multiplier}
            value={rate}
            onChange={(event) => setRate(event.target.value)}
          />
        </div>
        <div className='field'>
          <label>留言</label>
          <input
            placeholder='一句话说明理由'
            value={reason}
            onChange={(event) => setReason(event.target.value)}
          />
        </div>
        <div className='m-foot'>
          <button className='btn' onClick={props.onClose}>
            取消
          </button>
          <button
            className='btn primary'
            disabled={busy}
            onClick={async () => {
              setBusy(true)
              try {
                await createMarketplaceBargainRequest({
                  groupId: group.id,
                  proposedMultiplier: Number(rate) || group.multiplier,
                  reason,
                })
                toast.success('砍价申请已提交')
                props.onClose()
              } catch (error) {
                toast.error(error instanceof Error ? error.message : '提交失败')
              } finally {
                setBusy(false)
              }
            }}
          >
            提交申请
          </button>
        </div>
      </div>
    </DawnModal>
  )
}

/** 连通性测试（单分组批量测试）。 */
function ConnectivityDialog(props: {
  group: MarketplaceGroup
  onClose: () => void
}) {
  const { group } = props
  const [model, setModel] = useState(group.models[0] ?? '')
  const [running, setRunning] = useState(false)
  const [result, setResult] = useState<MarketplaceBatchTest | null>(null)

  useEffect(() => {
    if (!result || result.status === 'completed' || result.status === 'failed')
      return
    const timer = window.setInterval(async () => {
      try {
        const next = await getMarketplaceBatchTest(result.id)
        setResult(next)
        if (next.status === 'completed' || next.status === 'failed') {
          window.clearInterval(timer)
          setRunning(false)
        }
      } catch {
        window.clearInterval(timer)
        setRunning(false)
      }
    }, 2000)
    return () => window.clearInterval(timer)
  }, [result])

  return (
    <DawnModal open onClose={props.onClose} variant='narrow' label='连通性测试'>
      <div className='m-main'>
        <ModalHead
          title={`连通性测试 · ${group.system_display_name}`}
          onClose={props.onClose}
        />
        <div className='field'>
          <label>模型</label>
          <select
            value={model}
            onChange={(event) => setModel(event.target.value)}
          >
            {group.models.map((name) => (
              <option key={name} value={name}>
                {name}
              </option>
            ))}
          </select>
        </div>
        {!result ? (
          <div className='pp-empty'>
            <Store size={26} />
            <span>选择模型后运行测试</span>
          </div>
        ) : (
          <div>
            {result.items.map((item) => (
              <div key={item.group_id}>
                <div className='step'>
                  {item.status === 'passed' ? (
                    <span className='ok'>✓</span>
                  ) : item.status === 'failed' ? (
                    <span style={{ color: 'var(--dawn-bad)' }}>✕</span>
                  ) : (
                    <span className='run'>…</span>
                  )}
                  {item.group_name}
                  <span className='st2'>
                    {item.status === 'passed'
                      ? `${item.latency_ms}ms`
                      : item.status === 'failed'
                        ? item.error || '失败'
                        : '运行中'}
                  </span>
                </div>
              </div>
            ))}
            <div className='kv' style={{ marginTop: 10 }}>
              <span>结论</span>
              <b
                style={{
                  color:
                    result.status === 'completed' &&
                    result.items.every((item) => item.status === 'passed')
                      ? 'var(--dawn-ok)'
                      : 'var(--dawn-warn)',
                }}
              >
                {result.status === 'completed'
                  ? result.items.every((item) => item.status === 'passed')
                    ? '连通正常'
                    : '存在异常'
                  : '运行中'}
              </b>
            </div>
          </div>
        )}
        <div className='m-foot'>
          <button className='btn' onClick={props.onClose}>
            关闭
          </button>
          <button
            className='btn primary'
            disabled={running || !model}
            onClick={async () => {
              setRunning(true)
              try {
                const test = await startMarketplaceBatchTest({
                  groupIds: [group.id],
                  model,
                })
                setResult(test)
              } catch (error) {
                toast.error(error instanceof Error ? error.message : '测试失败')
                setRunning(false)
              }
            }}
          >
            {running ? '测试中' : '运行测试'}
          </button>
        </div>
      </div>
    </DawnModal>
  )
}
