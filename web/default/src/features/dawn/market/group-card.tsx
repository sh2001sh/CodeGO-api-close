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
import { BadgeCheck, ChevronDown, CircleDashed } from 'lucide-react'
import type { MarketplaceGroup } from '@/features/marketplace/types'
import { compactCount, pct, sec, successRatePercent } from '../lib/format'

/** 市场分组卡：指标网格 + 模型行 + 近期请求 + 操作。 */
export function MarketGroupCard(props: {
  group: MarketplaceGroup
  selected: boolean
  inPool: boolean
  poolName?: string
  authed: boolean
  expanded: boolean
  onToggleSelect: () => void
  onToggleExpand: () => void
  onUse: (group: MarketplaceGroup) => void
  onBindKey: (group: MarketplaceGroup) => void
  onTest: (group: MarketplaceGroup) => void
  onBargain: (group: MarketplaceGroup) => void
  onJoinPool: (group: MarketplaceGroup) => void
  priceInfo?: { input: string; output: string; freeCount: number }
  modelPrices?: Record<string, string>
  modelFees?: Record<
    string,
    { mode: 'free' | 'percall' | 'token'; input: string; output: string; cache: string }
  >
}) {
  const { group } = props
  const hasTraffic = group.request_count > 0
  const lifecycleOn = group.lifecycle_status === 'active'
  const verification = group.models.length
    ? group.model_verification_results
    : []

  return (
    <div
      className={`gcard${props.selected ? ' sel' : ''}${props.inPool ? ' in' : ''}`}
      onClick={props.onToggleSelect}
      role='button'
      tabIndex={0}
      onKeyDown={(event) => {
        if (event.key === 'Enter') props.onToggleSelect()
      }}
    >
      <div className='halo' />
      <div className='top'>
        {group.rank > 0 && (
          <span
            className={`rank-badge${group.rank <= 3 ? ' top' : ''}`}
            title={`质量排行榜第 ${group.rank} 名`}
          >
            {String(group.rank).padStart(2, '0')}
          </span>
        )}
        <span className='src'>{group.source_label}</span>
        <div>
          <h3>{group.system_display_name}</h3>
        </div>
        <div className='state'>
          <span className={`pub ${lifecycleOn ? 'on' : 'off'}`}>
            {group.observing ? (
              <CircleDashed size={12} />
            ) : (
              <BadgeCheck size={12} />
            )}
            {group.observing
              ? '观测中'
              : lifecycleOn
                ? '在售'
                : group.lifecycle_status === 'suspended'
                  ? '已暂停'
                  : '未上架'}
          </span>
          {!hasTraffic && <span className='sub2'>窗口内无请求</span>}
          {hasTraffic && (
            <span className='sub2'>
              {compactCount(group.request_count)} / 24H
            </span>
          )}
        </div>
      </div>

      <div className='metrics'>
        <div className='m'>
          <b>{props.priceInfo?.input ?? '—'}</b>
          <span>输入价</span>
        </div>
        <div className='m'>
          <b>{props.priceInfo?.output ?? '—'}</b>
          <span>输出价</span>
        </div>
        <div className='m'>
          <b>
            {group.multiplier}
            <span className='u'>×</span>
          </b>
          <span>倍率</span>
        </div>
        <div
          className={`m${hasTraffic && group.success_rate >= 0.99 ? ' good' : ''}${hasTraffic && group.success_rate < 0.98 ? ' warn' : ''}`}
        >
          <b>
            {hasTraffic ? pct(group.success_rate) : '—'}
            <span className='u'>%</span>
          </b>
          <span>24H 成功</span>
        </div>
        <div
          className={`m${hasTraffic && group.avg_ttft_ms > 600 ? ' warn' : ''}`}
        >
          <b>
            {hasTraffic ? sec(group.avg_ttft_ms) : '—'}
            <span className='u'>s</span>
          </b>
          <span>P50</span>
        </div>
        <div className='m'>
          <b>
            {hasTraffic ? pct(group.cache_hit_rate, 0) : '—'}
            <span className='u'>%</span>
          </b>
          <span>缓存命中</span>
        </div>
        <div className='m'>
          <b>{compactCount(hasTraffic ? group.request_count : null)}</b>
          <span>24H 请求</span>
        </div>
      </div>

      <div
        className='metrics'
        style={{ gridTemplateColumns: 'repeat(3, 1fr)', marginTop: 8 }}
      >
        <div className='m'>
          <b>{group.models.length}</b>
          <span>模型数</span>
        </div>
        <div
          className={`m${(props.priceInfo?.freeCount ?? 0) > 0 ? ' good' : ''}`}
        >
          <b>{props.priceInfo?.freeCount ?? 0}</b>
          <span>免费模型</span>
        </div>
        <div className='m'>
          <b>{group.score || '—'}</b>
          <span>评分</span>
        </div>
      </div>

      <div className='mline'>
        {group.models.slice(0, 5).map((model) => {
          const price = props.modelPrices?.[model]
          return (
            <span
              className={`mtag${price === '免费' ? ' free' : ''}`}
              key={model}
            >
              {model}
              {price ? <b>{price}</b> : null}
            </span>
          )
        })}
        {group.models.length > 0 && (
          <button
            className='mtag more'
            onClick={(event) => {
              event.stopPropagation()
              props.onToggleExpand()
            }}
          >
            {props.expanded ? '收起明细' : `全部 ${group.models.length} 模型`}
            <ChevronDown
              size={11}
              style={{
                transform: props.expanded ? 'rotate(180deg)' : 'none',
                transition: 'transform 0.2s',
              }}
            />
          </button>
        )}
      </div>

      {props.expanded && (
        <div className='mlist'>
          <div className='mh'>
            <span>模型</span>
            <span style={{ textAlign: 'right' }}>输入 /1M</span>
            <span style={{ textAlign: 'right' }}>输出 /1M</span>
          </div>
          {group.models.map((model) => {
            const result =
              verification.find((item) => item.model === model) ?? {
                model,
                status: '',
                latency_ms: 0,
                listed: true,
                tested_at: '',
              }
            return (() => {
            const failed = result.status === 'failed'
            const fee = props.modelFees?.[result.model]
            const free = fee?.mode === 'free'
            return (
              <div className='mr' key={result.model}>
                <span className='mn'>
                  {result.model}
                  {free ? <i className='ftag'>免费</i> : null}
                  {failed ? (
                    <i className='ftag' style={{ background: 'var(--dawn-bad-bg)', color: 'var(--dawn-bad)' }}>
                      检测失败
                    </i>
                  ) : null}
                </span>
                <span className='mp'>{fee?.input ?? '—'}</span>
                <span className='mp'>{fee?.output ?? '—'}</span>
              </div>
            )
            })()
          })}
        </div>
      )}

      <div className='recent'>
        <span className='rl'>近期</span>
        {(group.recent_request_series ?? []).map((point) => {
          const rate = successRatePercent(point.success_rate)
          const cls =
            point.request_count === 0 || rate == null
              ? ''
              : rate >= 99
                ? ''
                : rate >= 95
                  ? 'w'
                  : 'e'
          return (
            <i
              className={`rb ${cls}`}
              key={point.ts}
              title={`${new Date(point.ts).toLocaleTimeString()} · 成功率 ${rate == null ? '无数据' : `${rate.toFixed(1)}%`} · 请求 ${point.request_count}`}
              aria-label={`成功率 ${rate == null ? '无数据' : `${rate.toFixed(1)}%`}，请求 ${point.request_count}`}
            />
          )
        })}
        {!group.recent_request_series?.length && (
          <i className='rb' style={{ background: '#e4dcd2' }} />
        )}
        <span className='rn'>
          {
            (group.recent_request_series ?? []).filter((p) => {
              const rate = successRatePercent(p.success_rate)
              return rate != null && rate < 95 && p.request_count > 0
            }).length
          }{' '}
          异常
        </span>
      </div>

      <div className='gact' onClick={(event) => event.stopPropagation()}>
        {lifecycleOn && props.authed && (
          <button
            className='btn mini primary'
            onClick={() => props.onUse(group)}
          >
            订阅
          </button>
        )}
        {lifecycleOn && props.authed && (
          <button
            className='btn mini'
            onClick={() => props.onBindKey(group)}
          >
            绑定 Key
          </button>
        )}
        <button className='btn mini' onClick={props.onToggleExpand}>
          {props.expanded ? '收起明细' : '模型明细'}
        </button>
        {props.authed && (
          <button className='btn mini' onClick={() => props.onTest(group)}>
            连通性测试
          </button>
        )}
        {lifecycleOn && props.authed && (
          <button className='btn mini' onClick={() => props.onBargain(group)}>
            砍价
          </button>
        )}
        {props.authed && (
          <button
            className='btn mini'
            onClick={() => props.onJoinPool(group)}
            disabled={props.inPool}
          >
            {props.inPool ? `已在 ${props.poolName ?? '当前池'}` : '加入当前池'}
          </button>
        )}
        <span className='spacer' />
        {props.selected && (
          <span className='selhint'>已在右侧路由池工作台打开</span>
        )}
        {props.inPool && <span className='inpool'>已入池</span>}
      </div>
    </div>
  )
}
