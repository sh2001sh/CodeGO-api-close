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
import { Link } from '@tanstack/react-router'
import i18n from '@/i18n/config'
import { Check, ChevronDown, Info, Plus, Store } from 'lucide-react'
import { Trans, useTranslation } from 'react-i18next'
import { formatQuota } from '@/lib/format'
import { classifyRequestHealth } from '@/lib/request-health'
import { cn } from '@/lib/utils'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { MarketReportButton } from '@/features/marketplace/components/market-report-button'
import { ModelConsistencyBadge } from '@/features/marketplace/components/model-verification'
import { RecentRequestStrip } from '@/features/marketplace/components/recent-request-strip'
import { formatMultiplier } from '@/features/marketplace/lib/format'
import {
  localizedGroupName,
  localizedSourceLabel,
} from '@/features/marketplace/lib/localized-group-name'
import type { MarketplaceGroup } from '@/features/marketplace/types'
import { compactCount, pct, sec } from '../lib/format'
import './market-cards.css'

/** 紧凑市场资源卡：核心指标、完整费率与可展开的渠道详情。 */
export function MarketGroupCard(props: {
  group: MarketplaceGroup
  windowHours?: number
  selected: boolean
  inPool: boolean
  poolName?: string
  authed: boolean
  joining?: boolean
  joinDisabled?: boolean
  expanded: boolean
  selectedModels: string[]
  onToggleSelect: () => void
  onToggleExpand: () => void
  onUse: (group: MarketplaceGroup) => void
  onBindKey: (group: MarketplaceGroup) => void
  onTest: (group: MarketplaceGroup) => void
  onBargain: (group: MarketplaceGroup) => void
  onJoinPool: (group: MarketplaceGroup) => void
  onVisitShop?: (group: MarketplaceGroup) => void
  onTag?: (tag: string) => void
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
  const windowHours = props.windowHours ?? 24
  const displayName = group.custom_name
    ? `${group.channel_id}-${group.custom_name}`
    : localizedGroupName(
        t(group.system_display_name),
        group.source_label,
        i18n.language
      )
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

  const statusLabel = !hasTraffic
    ? t('无请求')
    : group.observing
      ? t('观测中')
      : lifecycleOn
        ? t('在售')
        : group.lifecycle_status === 'suspended'
          ? t('已暂停')
          : t('未上架')
  const statusTone = !hasTraffic || !lifecycleOn ? 'unknown' : health
  const lifecycleLabel = lifecycleOn
    ? t('在售')
    : group.lifecycle_status === 'suspended'
      ? t('已暂停')
      : group.lifecycle_status === 'disabled'
        ? t('已下架')
        : group.lifecycle_status === 'verifying'
          ? t('核验中')
          : group.lifecycle_status === 'pending_review'
            ? t('待审核')
            : group.lifecycle_status === 'degraded'
              ? t('降级')
              : t('未上架')

  return (
    <article
      className={cn('gcard', props.selected && 'sel', props.inPool && 'in')}
      data-market-group={group.id}
    >
      <div className='top'>
        <div className='market-card-identity'>
          <span
            className={`market-status-dot ${statusTone}`}
            aria-label={statusLabel}
            title={statusLabel}
          />
          <div className='market-card-name'>
            <h3>
              <button
                type='button'
                className='market-group-select'
                aria-pressed={props.selected}
                onClick={props.onToggleSelect}
                title={displayName}
              >
                {displayName}
              </button>
            </h3>
            <div className='market-card-meta'>
              <span className='market-channel-id'>ID {group.channel_id}</span>
              <span className='market-card-lifecycle'>{lifecycleLabel}</span>
              <Popover>
                <PopoverTrigger
                  render={
                    <button
                      type='button'
                      className='market-source-trigger max-w-full min-w-0'
                    />
                  }
                >
                  <span className='min-w-0 truncate'>
                    {!isOfficial && `${t('渠道主声明')} · `}
                    {localizedSourceLabel(
                      group.source_label || '未声明来源',
                      i18n.language
                    )}
                  </span>
                  <Info size={12} className='shrink-0' aria-hidden='true' />
                </PopoverTrigger>
                <PopoverContent
                  className='market-source-popover w-[min(360px,calc(100vw-32px))] rounded-2xl p-4'
                  align='start'
                >
                  <strong>
                    {isOfficial ? t('来源核验') : t('渠道主声明')}
                  </strong>
                  {!isOfficial && (
                    <p className='text-muted-foreground text-xs leading-relaxed'>
                      {t('来源由渠道主自行声明，平台检测不代表来源核验。')}
                    </p>
                  )}
                  <ModelConsistencyBadge
                    status={group.model_consistency_status}
                  />
                  <dl className='market-source-facts'>
                    <div>
                      <dt>{t('来源')}</dt>
                      <dd>
                        {localizedSourceLabel(
                          group.source_label || '未声明来源',
                          i18n.language
                        )}
                      </dd>
                    </div>
                    <div>
                      <dt>{t('核验状态')}</dt>
                      <dd>{group.verification_status || '—'}</dd>
                    </div>
                    <div>
                      <dt>{t('核验阶段')}</dt>
                      <dd>{group.verification_stage || '—'}</dd>
                    </div>
                    <div>
                      <dt>{t('核验时间')}</dt>
                      <dd>{group.verification_completed_at || '—'}</dd>
                    </div>
                  </dl>
                  {group.verification_summary && (
                    <p className='text-xs leading-relaxed [overflow-wrap:anywhere]'>
                      {group.verification_summary}
                    </p>
                  )}
                </PopoverContent>
              </Popover>
              {group.rank > 0 && (
                <span
                  className={cn('rank-badge', group.rank <= 3 && 'top')}
                  title={t('质量排行榜第 {{rank}} 名', { rank: group.rank })}
                >
                  #{String(group.rank).padStart(2, '0')}
                </span>
              )}
            </div>
          </div>
        </div>
        {props.authed ? (
          <button
            type='button'
            className='market-join-button'
            data-market-join={group.id}
            onClick={() => props.onJoinPool(group)}
            disabled={
              props.joinDisabled ||
              props.joining ||
              props.inPool ||
              !lifecycleOn
            }
            aria-busy={props.joining || undefined}
            title={
              props.inPool
                ? t('已在 {{param0}}', {
                    param0: props.poolName ?? t('当前池'),
                  })
                : undefined
            }
          >
            {props.inPool ? (
              <Check size={14} aria-hidden='true' />
            ) : (
              <Plus size={14} aria-hidden='true' />
            )}
            {props.joining
              ? t('处理中')
              : props.inPool
                ? t('已入池')
                : t('加入池')}
          </button>
        ) : (
          <span className={`market-card-status ${statusTone}`}>
            {statusLabel}
          </span>
        )}
      </div>

      <div className='metrics market-primary-metrics'>
        <div className='m'>
          <b>
            {formatMultiplier(group.multiplier)}
            <span className='u'>×</span>
          </b>
          <span>{t('倍率')}</span>
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
            {windowHours}H {t('成功率')}
          </span>
        </div>
        <div
          className={cn('m', hasTraffic && group.avg_ttft_ms > 600 && 'warn')}
        >
          <b>
            {hasTraffic && group.avg_ttft_ms > 0 ? sec(group.avg_ttft_ms) : '—'}
            <span className='u'>s</span>
          </b>
          <span>{t('平均首字')}</span>
        </div>
        <div className='m'>
          <b>{compactCount(hasTraffic ? group.request_count : null)}</b>
          <span>
            {windowHours}H {t('请求')}
          </span>
        </div>
      </div>

      <dl className='market-secondary-metrics'>
        <div
          className='market-average-metric'
          data-market-metric='average'
          title={t(
            '近 24 小时钱包/通用额度成功请求每 100 万实际 token 的平均扣费；套餐请求和无 token 记录不计入。这是按真实输入、输出与缓存结构形成的综合单价，不等于官方输入单价。'
          )}
        >
          <dt>{t('平均实扣/1M tokens')}</dt>
          <dd>{displayedAverage > 0 ? formatQuota(displayedAverage) : '—'}</dd>
        </div>
        <div>
          <dt>{t('缓存命中')}</dt>
          <dd>{hasTraffic ? `${pct(group.cache_hit_rate, 0)}%` : '—'}</dd>
        </div>
        <div>
          <dt>{t('模型数')}</dt>
          <dd>{group.models.length}</dd>
        </div>
        <div>
          <dt>{t('并发')}</dt>
          <dd>
            {group.current_concurrency}/{group.max_concurrency || '∞'}
          </dd>
        </div>
        <div>
          <dt>{t('评分')}</dt>
          <dd>{hasTraffic ? group.score.toFixed(2) : '—'}</dd>
        </div>
      </dl>

      <div className='market-capabilities'>
        <span
          className='market-capability'
          data-market-capability='remote-compaction'
        >
          <span>{t('远程压缩')}</span>
          <b>
            {group.remote_compaction_support === 'v1_v2'
              ? 'v1 + v2'
              : group.remote_compaction_support === 'v1'
                ? t('仅 v1')
                : group.remote_compaction_support === 'v2'
                  ? t('仅 v2')
                  : t('不支持')}
          </b>
        </span>
        <span
          className='market-capability'
          data-market-capability='credit-pack'
        >
          <span>{t('套餐')}</span>
          <b>
            {isOfficial || group.credit_pack_supported === true
              ? t('支持使用')
              : t('不支持')}
          </b>
        </span>
        <span
          className='market-capability'
          data-market-capability='multiplier-card'
        >
          <span>{t('倍率卡')}</span>
          <b>
            {group.multiplier_card_user_enabled ? t('支持使用') : t('不支持')}
          </b>
        </span>
      </div>
      <div className='market-request-strip'>
        <RecentRequestStrip
          group={group}
          segmentCount={group.recent_request_series?.length || 24}
        />
      </div>

      <div className='mline'>
        {group.models.slice(0, 3).map((model) => {
          const price = props.modelPrices?.[model]
          return (
            <span
              className={cn('mtag', price === t('免费') && 'free')}
              key={model}
              title={model}
            >
              {model}
              {price ? <b>{price}</b> : null}
            </span>
          )
        })}
        {group.models.length > 0 && (
          <button
            type='button'
            className='mtag more'
            aria-label={
              props.expanded
                ? t('收起明细')
                : t('全部 {{param0}} 模型', { param0: group.models.length })
            }
            aria-expanded={props.expanded}
            aria-controls={`market-models-${group.id}`}
            onClick={props.onToggleExpand}
          >
            {props.expanded
              ? t('收起明细')
              : group.models.length > 3
                ? `+${group.models.length - 3}`
                : t('费率')}
            <ChevronDown
              size={12}
              aria-hidden='true'
              style={{
                transform: props.expanded ? 'rotate(180deg)' : undefined,
              }}
            />
          </button>
        )}
      </div>

      {props.expanded && (
        <div className='mlist cache-prices' id={`market-models-${group.id}`}>
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
                    <Link
                      to='/pricing/$modelId'
                      params={{ modelId: result.model }}
                      search={{ group: group.id }}
                    >
                      {result.model}
                    </Link>
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

      {!isOfficial && !!group.tags?.length && (
        <details className='market-card-details'>
          <summary>
            <Info size={13} aria-hidden='true' />
            {t('详情')}
            <ChevronDown size={12} aria-hidden='true' />
          </summary>
          <div className='market-card-detail-body'>
            {!isOfficial && !!group.tags?.length && (
              <div className='market-card-tags'>
                {group.tags.map((tag) => (
                  <button
                    type='button'
                    key={tag}
                    onClick={() => props.onTag?.(tag)}
                  >
                    #{tag}
                  </button>
                ))}
              </div>
            )}
          </div>
        </details>
      )}

      <div className='gact' onClick={(event) => event.stopPropagation()}>
        {!isOfficial && !!group.shop_id && props.onVisitShop && (
          <button
            type='button'
            className='market-shop-link'
            onClick={() => props.onVisitShop?.(group)}
          >
            <Store size={13} aria-hidden='true' />
            {group.shop_name || t('进入店铺')}
          </button>
        )}
        {lifecycleOn && props.authed && (
          <button
            type='button'
            className='btn mini'
            onClick={() => props.onBindKey(group)}
          >
            {t('绑定 Key')}
          </button>
        )}
        {props.authed && (
          <button
            type='button'
            className='btn mini'
            onClick={() => props.onTest(group)}
          >
            {t('连通性测试')}
          </button>
        )}
        {!isOfficial && lifecycleOn && props.authed && (
          <button
            type='button'
            className='btn mini'
            onClick={() => props.onBargain(group)}
          >
            {t('砍价')}
          </button>
        )}
        {!isOfficial && (
          <MarketReportButton
            type='channel'
            id={group.channel_id}
            name={group.custom_name || group.system_display_name}
            authed={props.authed}
          />
        )}
      </div>
    </article>
  )
}
