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
import i18n from '@/i18n/config'
import { BadgeCheck, ChevronDown, CircleDashed } from 'lucide-react'
import { Trans, useTranslation } from 'react-i18next'
import { formatQuota } from '@/lib/format'
import { classifyRequestHealth } from '@/lib/request-health'
import { cn } from '@/lib/utils'
import { RecentRequestStrip } from '@/features/marketplace/components/recent-request-strip'
import {
  localizedGroupName,
  localizedSourceLabel,
} from '@/features/marketplace/lib/localized-group-name'
import type { MarketplaceGroup } from '@/features/marketplace/types'
import { compactCount, pct, sec } from '../lib/format'

/** 市场分组卡：指标网格 + 模型行 + 近期请求 + 操作。 */
export function MarketGroupCard(props: {
  group: MarketplaceGroup
  selected: boolean
  inPool: boolean
  poolName?: string
  authed: boolean
  expanded: boolean
  selectedModels: string[]
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
    {
      mode: 'free' | 'percall' | 'token'
      input: string
      output: string
      cacheWrite: string
      cacheRead: string
      tiered: boolean
    }
  >
}) {
  const { t } = useTranslation()
  const { group } = props
  const isOfficial = group.source_type === 'official'
  const hasTraffic = group.request_count > 0
  const health = classifyRequestHealth(group.success_rate, group.request_count)
  const lifecycleOn = group.lifecycle_status === 'active'
  const selectedModelAmounts = props.selectedModels
    .map(
      (selected) =>
        Object.entries(group.avg_consumer_amount_by_model ?? {}).find(
          ([model]) => model.toLowerCase() === selected.toLowerCase()
        )?.[1]
    )
    .filter(
      (amount): amount is number => typeof amount === 'number' && amount > 0
    )
  const selectedModelAverage = selectedModelAmounts.length
    ? selectedModelAmounts.reduce((sum, amount) => sum + amount, 0) /
      selectedModelAmounts.length
    : 0
  const displayedAverage =
    props.selectedModels.length > 0
      ? selectedModelAverage
      : group.avg_consumer_amount
  const verification = group.models.length
    ? group.model_verification_results
    : []

  return (
    <div
      className={cn('gcard', props.selected && 'sel', props.inPool && 'in')}
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
            className={cn('rank-badge', group.rank <= 3 && 'top')}
            title={i18n.t('质量排行榜第 {{rank}} 名', { rank: group.rank })}
          >
            {String(group.rank).padStart(2, '0')}
          </span>
        )}
        <span className='src'>
          {localizedSourceLabel(group.source_label, i18n.language)}
        </span>
        <div>
          <h3>
            {localizedGroupName(
              group.system_display_name,
              group.source_label,
              i18n.language
            )}
          </h3>
        </div>
        <div className='state'>
          <span
            className={`pub ${!hasTraffic ? 'idle' : lifecycleOn ? 'on' : 'off'}`}
          >
            {group.observing ? (
              <CircleDashed size={12} />
            ) : (
              <BadgeCheck size={12} />
            )}
            {!hasTraffic
              ? i18n.t('无请求')
              : group.observing
                ? i18n.t('观测中')
                : lifecycleOn
                  ? i18n.t('在售')
                  : group.lifecycle_status === 'suspended'
                    ? i18n.t('已暂停')
                    : i18n.t('未上架')}
          </span>
          {!hasTraffic && (
            <span className='sub2'>
              <Trans i18nKey={'窗口内无请求'} />
            </span>
          )}
          {hasTraffic && (
            <span className='sub2'>
              {compactCount(group.request_count)} / 24H
            </span>
          )}
        </div>
      </div>

      <div className='metrics'>
        <div className='m'>
          <b>
            {group.multiplier}
            <span className='u'>×</span>
          </b>
          <span>
            <Trans i18nKey={'倍率'} />
          </span>
        </div>
        <div
          className={cn(
            'm',
            health === 'healthy' && 'good',
            health === 'unstable' && 'warn',
            health === 'failed' && 'bad'
          )}
        >
          <b>
            {hasTraffic ? group.success_rate.toFixed(1) : '—'}
            <span className='u'>%</span>
          </b>
          <span>
            <Trans i18nKey={'24H 成功'} />
          </span>
        </div>
        <div
          className={cn('m', hasTraffic && group.avg_ttft_ms > 600 && 'warn')}
        >
          <b>
            {hasTraffic && group.avg_ttft_ms > 0 ? sec(group.avg_ttft_ms) : '—'}
            <span className='u'>s</span>
          </b>
          <span>
            <Trans i18nKey={'平均首字'} />
          </span>
        </div>
        <div className='m'>
          <b>
            {hasTraffic ? pct(group.cache_hit_rate, 0) : '—'}
            <span className='u'>%</span>
          </b>
          <span>
            <Trans i18nKey={'缓存命中'} />
          </span>
        </div>
        <div className='m'>
          <b>{compactCount(hasTraffic ? group.request_count : null)}</b>
          <span>
            <Trans i18nKey={'24H 请求'} />
          </span>
        </div>
        <div
          className='m'
          title={i18n.t(
            '近 24 小时钱包/通用额度成功请求每 100 万实际 token 的平均扣费；套餐请求和无 token 记录不计入。这是按真实输入、输出与缓存结构形成的综合单价，不等于官方输入单价。'
          )}
        >
          <b>{displayedAverage > 0 ? formatQuota(displayedAverage) : '—'}</b>
          <span>
            <Trans i18nKey={'平均实扣/1M tokens'} />
          </span>
        </div>
      </div>

      <div
        className='metrics'
        style={{ gridTemplateColumns: 'repeat(3, 1fr)', marginTop: 8 }}
      >
        <div className='m'>
          <b>{group.models.length}</b>
          <span>
            <Trans i18nKey={'模型数'} />
          </span>
        </div>
        <div className='m'>
          <b>
            {group.current_concurrency}/{group.max_concurrency || '∞'}
          </b>
          <span>
            <Trans i18nKey={'并发'} />
          </span>
        </div>
        <div className='m'>
          <b>{hasTraffic ? group.score.toFixed(2) : '—'}</b>
          <span>
            <Trans i18nKey={'评分'} />
          </span>
        </div>
      </div>

      <div className='capline'>
        <span>
          <Trans i18nKey={'远程压缩'} />
        </span>
        <b>
          {group.remote_compaction_support === 'v1_v2'
            ? 'v1 + v2'
            : group.remote_compaction_support === 'v1'
              ? i18n.t('仅 v1')
              : group.remote_compaction_support === 'v2'
                ? i18n.t('仅 v2')
                : i18n.t('不支持')}
        </b>
      </div>

      {group.multiplier_card_user_enabled && (
        <div className='capline'>
          <span>
            <Trans i18nKey={'倍率卡'} />
          </span>
          <b>
            <Trans i18nKey={'支持使用'} />
          </b>
        </div>
      )}

      <div className='mline'>
        {group.models.slice(0, 5).map((model) => {
          const price = props.modelPrices?.[model]
          return (
            <span
              className={cn('mtag', price === '免费' && 'free')}
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
            {props.expanded
              ? i18n.t('收起明细')
              : i18n.t('全部 {{param0}} 模型', { param0: group.models.length })}
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
        <div className='mlist cache-prices'>
          <div className='mh'>
            <span>
              <Trans i18nKey={'模型'} />
            </span>
            <span style={{ textAlign: 'right' }}>
              <Trans i18nKey={'输入 /1M · 按次价格'} />
            </span>
            <span style={{ textAlign: 'right' }}>
              <Trans i18nKey={'输出 /1M'} />
            </span>
            <span className='mp'>{t('缓存写入')} /1M</span>
            <span className='mp'>{t('缓存读取')} /1M</span>
            <span style={{ textAlign: 'right' }}>
              <Trans i18nKey={'平均实扣/1M tokens'} />
            </span>
            <span style={{ textAlign: 'right' }}>
              <Trans i18nKey={'延迟'} />
            </span>
          </div>
          {group.models.map((model) => {
            const result = verification.find(
              (item) => item.model === model
            ) ?? {
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
                    {free ? (
                      <i className='ftag'>
                        <Trans i18nKey={'免费'} />
                      </i>
                    ) : null}
                    {fee?.tiered ? (
                      <i
                        className='ftag'
                        title={t('展示首档价格，完整阶梯价格见模型详情')}
                      >
                        {t('首档')}
                      </i>
                    ) : null}
                    {failed ? (
                      <i
                        className='ftag'
                        style={{
                          background: 'var(--dawn-bad-bg)',
                          color: 'var(--dawn-bad)',
                        }}
                      >
                        <Trans i18nKey={'检测失败'} />
                      </i>
                    ) : null}
                  </span>
                  <span className='mp'>
                    {fee?.mode === 'percall' && fee.input !== '—'
                      ? i18n.t('{{param0}} /次', { param0: fee.input })
                      : (fee?.input ?? '—')}
                  </span>
                  <span className='mp'>
                    {fee?.mode === 'percall'
                      ? i18n.t('按次计费')
                      : (fee?.output ?? '—')}
                  </span>
                  <span className='mp'>{fee?.cacheWrite ?? '—'}</span>
                  <span className='mp'>{fee?.cacheRead ?? '—'}</span>
                  <span
                    className='mp'
                    title={i18n.t(
                      '近 24 小时该模型钱包/通用额度成功请求每 100 万实际 token 的平均扣费；套餐请求和无 token 记录不计入'
                    )}
                  >
                    {(group.avg_consumer_amount_by_model?.[result.model] ?? 0) >
                    0
                      ? formatQuota(
                          group.avg_consumer_amount_by_model[result.model]
                        )
                      : '—'}
                  </span>
                  <span className='mp'>
                    {result.latency_ms > 0 ? `${result.latency_ms}ms` : '—'}
                  </span>
                </div>
              )
            })()
          })}
        </div>
      )}

      <RecentRequestStrip group={group} />

      <div className='gact' onClick={(event) => event.stopPropagation()}>
        {lifecycleOn && props.authed && (
          <button className='btn mini' onClick={() => props.onBindKey(group)}>
            <Trans i18nKey={'绑定 Key'} />
          </button>
        )}
        {props.authed && (
          <button className='btn mini' onClick={() => props.onTest(group)}>
            <Trans i18nKey={'连通性测试'} />
          </button>
        )}
        {!isOfficial && lifecycleOn && props.authed && (
          <button className='btn mini' onClick={() => props.onBargain(group)}>
            <Trans i18nKey={'砍价'} />
          </button>
        )}
        {props.authed && (
          <button
            className='btn mini'
            onClick={() => props.onJoinPool(group)}
            disabled={props.inPool}
          >
            {props.inPool
              ? i18n.t('已在 {{param0}}', {
                  param0: props.poolName ?? '当前池',
                })
              : i18n.t('加入当前池')}
          </button>
        )}
        <span className='spacer' />
        {props.selected && (
          <span className='selhint'>
            <Trans i18nKey={'已在右侧路由池工作台打开'} />
          </span>
        )}
        {props.inPool && (
          <span className='inpool'>
            <Trans i18nKey={'已入池'} />
          </span>
        )}
      </div>
    </div>
  )
}
