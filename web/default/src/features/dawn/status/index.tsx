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
import { useMemo, useState } from 'react'
import { Link } from '@tanstack/react-router'
import {
  ChevronDown,
  CircleSlash,
  RefreshCw,
  Store,
  Waypoints,
} from 'lucide-react'
import { SiteSeo } from '@/components/seo'
import { useMarketplaceGroups } from '@/features/marketplace/hooks'
import type { MarketplaceGroup } from '@/features/marketplace/types'
import { DawnNav } from '../components/dawn-nav'
import { DawnQueryError } from '../components/query-error'
import {
  healthState,
  pct,
  successRatePercent,
  type HealthState,
} from '../lib/format'

const STATUS_FILTERS = {
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
  page_size: 50,
}

type SourceFilter = 'all' | 'official' | 'marketplace_user'
type StateFilter = '' | HealthState

export function DawnStatus() {
  const query = useMarketplaceGroups(STATUS_FILTERS)
  const groups = useMemo(() => query.data?.items ?? [], [query.data])

  const [source, setSource] = useState<SourceFilter>('all')
  const [search, setSearch] = useState('')
  const [model, setModel] = useState('')
  const [state, setState] = useState<StateFilter>('')
  const [openSet, setOpenSet] = useState<Set<string>>(() => new Set())

  const stateOf = (group: MarketplaceGroup): HealthState => {
    // Prefer the backend's latest-window classification so the headline state
    // uses the same thresholds as the marketplace status strip. Fall back to
    // the aggregate metric for older responses that omit this field.
    if (group.latest_request_status === 'healthy') return 'ok'
    if (group.latest_request_status === 'unstable') return 'warn'
    if (group.latest_request_status === 'failed') return 'bad'
    return healthState({
      requestCount: group.request_count,
      successRate: group.success_rate,
    })
  }

  const allModels = useMemo(
    () => [...new Set(groups.flatMap((group) => group.models))].sort(),
    [groups]
  )

  const visible = useMemo(() => {
    const keyword = search.trim().toLowerCase()
    return groups
      .map((group) => ({ group, st: stateOf(group) }))
      .filter(
        ({ group, st }) =>
          (source === 'all' || group.source_type === source) &&
          (!state || st === state) &&
          (!model || group.models.includes(model)) &&
          (!keyword ||
            group.system_display_name.toLowerCase().includes(keyword) ||
            group.source_label.toLowerCase().includes(keyword) ||
            group.models.some((name) => name.toLowerCase().includes(keyword)))
      )
  }, [groups, source, state, model, search])

  const counts = useMemo(() => {
    const tally: Record<HealthState, number> = {
      ok: 0,
      warn: 0,
      bad: 0,
      idle: 0,
    }
    groups.forEach((group) => {
      tally[stateOf(group)] += 1
    })
    return tally
  }, [groups])

  const toggle = (id: string) =>
    setOpenSet((current) => {
      const next = new Set(current)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })

  return (
    <div className='dawn'>
      <SiteSeo
        title='分组状态 | Code Go'
        description='分组状态 · 实时可用率与延迟'
        canonicalPath='/status'
      />
      <DawnNav />
      <main className='dawn-wrap'>
        <div className='mhead'>
          <div>
            <div className='kick'>
              <span className='n'>S·01</span>
              GROUP STATUS
            </div>
            <h1>
              分组状态，<em style={{ color: 'var(--dawn-ok)' }}>实时</em>。
            </h1>
          </div>
          <div style={{ display: 'flex', gap: 10, alignItems: 'center' }}>
            <span className='live'>
              <i />
              LIVE · 24H
            </span>
            <button
              className='btn mini'
              onClick={() => void query.refetch()}
              disabled={query.isFetching}
            >
              <RefreshCw
                size={14}
                className={query.isFetching ? 'animate-spin' : ''}
              />
              刷新
            </button>
            <Link className='btn mini primary' to='/market'>
              <Store size={14} />
              前往分组市场
            </Link>
          </div>
        </div>

        <div className='statband'>
          <div className='cell'>
            <span className='win'>24H WINDOW</span>
            <b>{groups.length}</b>
            <span>分组</span>
          </div>
          <div className='cell'>
            <b className='c-ok'>{counts.ok}</b>
            <span className='lbl'>
              <i className='dot ok' /> 稳定
            </span>
          </div>
          <div className='cell'>
            <b className='c-warn'>{counts.warn}</b>
            <span className='lbl'>
              <i className='dot warn' /> 波动
            </span>
          </div>
          <div className='cell'>
            <b className='c-bad'>{counts.bad}</b>
            <span className='lbl'>
              <i className='dot bad' /> 异常
            </span>
          </div>
          <div className='cell'>
            <b style={{ color: 'var(--dawn-ink2)' }}>{counts.idle}</b>
            <span className='lbl'>
              <i className='dot idle' /> 无请求
            </span>
          </div>
        </div>

        <div className='filters'>
          <div className='seg'>
            <button
              className={source === 'all' ? 'on' : ''}
              onClick={() => setSource('all')}
            >
              全部
            </button>
            <button
              className={source === 'official' ? 'on' : ''}
              onClick={() => setSource('official')}
            >
              官方渠道
            </button>
            <button
              className={source === 'marketplace_user' ? 'on' : ''}
              onClick={() => setSource('marketplace_user')}
            >
              第三方渠道
            </button>
          </div>
          <input
            placeholder='搜索分组 / 来源 / 模型'
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
          <select
            className='fsel'
            value={model}
            onChange={(event) => setModel(event.target.value)}
          >
            <option value=''>全部模型</option>
            {allModels.map((name) => (
              <option key={name} value={name}>
                {name}
              </option>
            ))}
          </select>
          <select
            className='fsel'
            value={state}
            onChange={(event) => setState(event.target.value as StateFilter)}
          >
            <option value=''>全部状态</option>
            <option value='ok'>稳定</option>
            <option value='warn'>波动</option>
            <option value='bad'>异常</option>
            <option value='idle'>无请求</option>
          </select>
        </div>

        {query.isLoading ? (
          <div className='empty'>
            <span className='eic'>
              <RefreshCw size={20} className='animate-spin' />
            </span>
            <b>状态加载中</b>
          </div>
        ) : query.isError ? (
          <DawnQueryError
            title='状态数据加载失败'
            description='暂时无法获取分组状态，请稍后重试。'
            onRetry={() => void query.refetch()}
            retrying={query.isFetching}
          />
        ) : visible.length ? (
          visible.map(({ group, st }) => (
            <div
              className={`sgcard${openSet.has(group.id) ? ' open' : ''}`}
              key={group.id}
            >
              <div
                className='head'
                onClick={() => toggle(group.id)}
                role='button'
                tabIndex={0}
                onKeyDown={(event) => {
                  if (event.key === 'Enter' || event.key === ' ')
                    toggle(group.id)
                }}
              >
                <span className='sg-title'>
                  <span className={`dot ${st}`} />
                  <h3>{group.system_display_name}</h3>
                  <span className='src'>{group.source_label}</span>
                  {st === 'idle' && <span className='tag'>窗口内无调用</span>}
                </span>
                <span className='mini-recent'>
                  <RecentStrip group={group} />
                </span>
                <span className='sg-meta'>
                  <span className={`rate ${st}`}>
                    成功率{' '}
                    <b>
                      {hasTraffic(group)
                        ? `${pct(group.success_rate)}%`
                        : '—'}
                    </b>
                  </span>
                  <span className='cache'>
                    缓存 <b>{pct(group.cache_hit_rate, 0)}%</b>
                  </span>
                  <Link
                    className='btn mini'
                    to='/market'
                    onClick={(event) => event.stopPropagation()}
                  >
                    <Waypoints size={13} />
                    加入路由池
                  </Link>
                  <ChevronDown size={16} className='cv' />
                </span>
              </div>
              <div className='gbody'>
                {group.model_verification_results.length ? (
                  group.model_verification_results.map((result) => {
                    const rowState = result.status === 'passed' ? 'ok' : 'bad'
                    return (
                      <div className='mrow' key={`${group.id}-${result.model}`}>
                        <span className='mn'>
                          <span
                            className={`dot ${rowState}`}
                            style={{ width: 6, height: 6, boxShadow: 'none' }}
                          />
                          {result.model}
                        </span>
                        <span
                          className={`num ${rowState === 'ok' ? 'ok' : 'bad'}`}
                        >
                          检测{' '}
                          <b>{result.status === 'passed' ? '通过' : '失败'}</b>
                        </span>
                        <span
                          className={`num ${result.latency_ms > 1000 ? 'warn' : ''}`}
                        >
                          延迟 <b>{result.latency_ms}ms</b>
                        </span>
                        <span className='mini-recent'>
                          <RecentStrip group={group} />
                        </span>
                        <span
                          className={`stt ${rowState === 'ok' ? 'ok' : 'bad'}`}
                        >
                          {result.status === 'passed' ? '稳定' : '异常'}
                        </span>
                      </div>
                    )
                  })
                ) : group.models.length ? (
                  group.models.slice(0, 12).map((name) => (
                    <div className='mrow' key={`${group.id}-${name}`}>
                      <span className='mn'>
                        <span
                          className='dot idle'
                          style={{ width: 6, height: 6, boxShadow: 'none' }}
                        />
                        {name}
                      </span>
                      <span className='num'>
                        检测 <b>—</b>
                      </span>
                      <span className='num'>
                        延迟 <b>—</b>
                      </span>
                      <span className='mini-recent'>
                        <RecentStrip group={group} />
                      </span>
                      <span className='stt idle'>待观测</span>
                    </div>
                  ))
                ) : (
                  <div
                    className='empty'
                    style={{
                      marginTop: 0,
                      borderTopLeftRadius: 0,
                      borderTopRightRadius: 0,
                    }}
                  >
                    <span className='eic'>
                      <Waypoints size={20} />
                    </span>
                    <b>窗口内无调用</b>
                  </div>
                )}
              </div>
            </div>
          ))
        ) : (
          <div className='empty'>
            <span className='eic'>
              <CircleSlash size={20} />
            </span>
            <b>无匹配分组</b>
          </div>
        )}
      </main>
    </div>
  )
}

/** 由 recent_request_series 渲染近期状态条；hover 显示时段成功率与成功/失败次数。 */
function RecentStrip({ group }: { group: MarketplaceGroup }) {
  const series = group.recent_request_series ?? []
  if (!series.length) {
    return <i style={{ background: '#e4dcd2', width: '100%' }} />
  }
  return (
    <>
      {series.map((point) => {
        const rate = successRatePercent(point.success_rate)
        const cls =
          point.request_count === 0 || rate == null
            ? ''
            : rate >= 99
              ? ''
              : rate >= 95
                ? 'w'
                : 'e'
        const success =
          rate == null
            ? null
            : Math.round((point.request_count * rate) / 100)
        const failed =
          success == null ? null : point.request_count - success
        return (
          <span className='seg' key={point.ts} tabIndex={-1}>
            <i className={cls} />
            <span className='tip' role='tooltip'>
              {point.request_count === 0 || rate == null ? (
                <>
                  {formatSegTime(point.ts)} · 无请求
                </>
              ) : (
                <>
                  {formatSegTime(point.ts)}
                  <br />
                  成功率 <b>{rate.toFixed(1)}%</b>
                  <br />
                  成功 {success} · <span className='t-fail'>失败 {failed}</span>
                </>
              )}
            </span>
          </span>
        )
      })}
    </>
  )
}

function formatSegTime(ts: number): string {
  const ms = ts < 1e12 ? ts * 1000 : ts
  const date = new Date(ms)
  return `${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`
}

function hasTraffic(group: MarketplaceGroup): boolean {
  return group.request_count > 0
}
