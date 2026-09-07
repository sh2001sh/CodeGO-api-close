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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import {
  ChevronDown,
  CircleSlash,
  RefreshCw,
  Store,
  Waypoints,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { cn } from '@/lib/utils'
import { SiteSeo } from '@/components/seo'
import { getMarketplaceGroupStatus } from '@/features/marketplace/api'
import { RecentRequestStrip } from '@/features/marketplace/components/recent-request-strip'
import type { MarketplaceGroup } from '@/features/marketplace/types'
import { DawnNav } from '../components/dawn-nav'
import { DawnQueryError } from '../components/query-error'
import { healthState, pct, type HealthState } from '../lib/format'
import { GroupModelStatus } from './group-model-status'

type SourceFilter = 'all' | 'official' | 'marketplace_user'
type StateFilter = '' | HealthState

export function DawnStatus() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const userID = useAuthStore((state) => state.auth.user?.id)
  const query = useQuery({
    queryKey: ['marketplace-group-status', userID],
    queryFn: getMarketplaceGroupStatus,
    staleTime: 30_000,
    refetchInterval: 60_000,
    refetchIntervalInBackground: false,
  })
  const groups = useMemo(() => query.data ?? [], [query.data])

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
    if (group.latest_request_status === 'unknown') return 'idle'
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
              LIVE · 6H
            </span>
            <button
              className='btn mini'
              onClick={() => {
                void query.refetch()
                void queryClient.invalidateQueries({
                  queryKey: ['marketplace-group-model-status'],
                })
              }}
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
            <span className='win'>6H WINDOW</span>
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

        <div className='filters status-filters'>
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
            aria-label={t('搜索分组、来源或模型')}
            placeholder='搜索分组 / 来源 / 模型'
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
          <select
            className='fsel'
            aria-label={t('模型筛选')}
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
            aria-label={t('状态筛选')}
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
              className={cn('sgcard', openSet.has(group.id) && 'open')}
              key={group.id}
            >
              <div
                className='head'
                onClick={() => toggle(group.id)}
                role='button'
                aria-expanded={openSet.has(group.id)}
                tabIndex={0}
                onKeyDown={(event) => {
                  if (event.key === 'Enter' || event.key === ' ') {
                    event.preventDefault()
                    toggle(group.id)
                  }
                }}
              >
                <span className='sg-title'>
                  <span className={`dot ${st}`} />
                  <h3>{group.system_display_name}</h3>
                  <span className='src'>{group.source_label}</span>
                  {st === 'idle' && <span className='tag'>窗口内无调用</span>}
                </span>
                <span className='mini-recent'>
                  <RecentRequestStrip group={group} compact />
                </span>
                <span className='sg-meta'>
                  <span
                    className={`rate ${healthState({ requestCount: group.request_count, successRate: group.success_rate })}`}
                  >
                    24H 成功率{' '}
                    <b>
                      {hasTraffic(group)
                        ? `${group.success_rate.toFixed(1)}%`
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
              {openSet.has(group.id) && (
                <div className='gbody'>
                  <GroupModelStatus group={group} />
                </div>
              )}
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

function hasTraffic(group: MarketplaceGroup): boolean {
  return group.request_count > 0
}
