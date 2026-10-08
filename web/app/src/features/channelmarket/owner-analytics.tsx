import { Suspense, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import { resourceOptions } from '../../lib/queries'
import type { Schema } from '../../lib/types'
import { useTranslation } from '../../lib/i18n'
import { credits, date } from '../../lib/format'
import { displayInt } from '../analytics/format'
import { DataTable } from '../../components/data-table'
import { Button, ErrorMessage, Loading, Panel, Stat, StatGrid, Status } from '../../components/ui'
import { MarketOwnerLogs } from './reports'
import {
  analyticsQuery,
  analyticsRange,
  localDateTime,
  successPercent,
  trendHeight,
  type OwnerAnalyticsFilters,
} from './owner-analytics-format'
import './owner-analytics.css'

export const ownerAnalyticsOptions = (filters: OwnerAnalyticsFilters) =>
  resourceOptions(
    'market-owner-analytics',
    (signal) =>
      api
        .GET('/api/marketplace/channels/mine/analytics', {
          signal,
          params: { query: filters },
        })
        .then(unwrap),
    [filters.from, filters.to, filters.channel_id ?? '', filters.model ?? ''],
  )

export function IncomeTrend({ points }: { points: Schema['ChannelOwnerAnalytics']['points'] }) {
  const { t } = useTranslation()
  const [selectedTime, setSelectedTime] = useState<string>()
  const [tableOpen, setTableOpen] = useState(false)
  const selected = points.find((point) => point.timestamp === selectedTime) ?? points.at(-1)
  const maximum = points.reduce((max, point) => {
    const amount = BigInt(point.net_micro)
    return amount > max ? amount : max
  }, 0n)
  // Each day keeps a usable touch target even for long reporting periods.
  const width = Math.max(720, points.length * 44)
  const height = 180
  const step = width / Math.max(1, points.length)
  return (
    <figure className="owner-income-trend">
      <div className="owner-income-chart-layout">
        <div className="owner-income-axis" aria-hidden>
          <span>{credits(maximum)}</span>
          <span>{credits(maximum / 2n)}</span>
          <span>{credits(0n)}</span>
        </div>
        <div className="owner-income-chart-scroll">
          <svg
            viewBox={`0 0 ${width} ${height}`}
            style={{ minWidth: points.length * 44 }}
            role="group"
            aria-label={t('所选时段净收入趋势')}
          >
            <line x1="0" x2={width} y1={height - 1} y2={height - 1} />
            {points.map((point, index) => {
              const barHeight = trendHeight(point.net_micro, maximum) * height
              return (
                <g
                  key={point.timestamp}
                  role="button"
                  tabIndex={0}
                  aria-label={`${date(point.timestamp)} · ${t('净收入')} ${credits(point.net_micro)} · ${t('请求数')} ${displayInt(point.request_count)}`}
                  aria-pressed={selected?.timestamp === point.timestamp}
                  onClick={() => setSelectedTime(point.timestamp)}
                  onFocus={() => setSelectedTime(point.timestamp)}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter' || event.key === ' ') {
                      event.preventDefault()
                      setSelectedTime(point.timestamp)
                    }
                    if (['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) {
                      event.preventDefault()
                      const next =
                        event.key === 'Home'
                          ? 0
                          : event.key === 'End'
                            ? points.length - 1
                            : Math.max(
                                0,
                                Math.min(
                                  points.length - 1,
                                  index + (event.key === 'ArrowRight' ? 1 : -1),
                                ),
                              )
                      const target =
                        event.currentTarget.parentElement?.querySelectorAll<SVGGElement>(
                          'g[role="button"]',
                        )[next]
                      target?.focus()
                    }
                  }}
                >
                  <rect
                    className="owner-income-hit"
                    x={index * step}
                    y="0"
                    width={step}
                    height={height}
                  />
                  <rect
                    className="owner-income-bar"
                    x={index * step + 3}
                    y={height - barHeight}
                    width={Math.max(1, step - 6)}
                    height={barHeight}
                    rx="2"
                  />
                  <title>
                    {date(point.timestamp)} · {credits(point.net_micro)}
                  </title>
                </g>
              )
            })}
          </svg>
        </div>
      </div>
      <figcaption>
        <span>{points[0] ? date(points[0].timestamp) : '—'}</span>
        <span>{points.at(-1) ? date(points.at(-1)?.timestamp) : '—'}</span>
      </figcaption>
      {selected && (
        <div className="owner-income-point" role="status" aria-live="polite">
          <span>{date(selected.timestamp)}</span>
          <strong>{credits(selected.net_micro)}</strong>
          <span>
            {t('请求数')} {displayInt(selected.request_count)}
          </span>
        </div>
      )}
      <Button
        variant="quiet"
        size="sm"
        aria-expanded={tableOpen}
        onClick={() => setTableOpen(!tableOpen)}
      >
        {t(tableOpen ? '收起数据表' : '查看数据表')}
      </Button>
      <div className={tableOpen ? 'table-scroll' : 'sr-only'}>
        <table>
          <caption>{t('所选时段净收入趋势')}</caption>
          <thead>
            <tr>
              <th>{t('时间')}</th>
              <th>{t('请求数')}</th>
              <th>{t('净收入')}</th>
            </tr>
          </thead>
          <tbody>
            {points.map((point) => (
              <tr key={point.timestamp}>
                <td>{date(point.timestamp)}</td>
                <td>{displayInt(point.request_count)}</td>
                <td>{credits(point.net_micro)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {maximum === 0n && <p className="muted">{t('所选时段暂无收入')}</p>}
    </figure>
  )
}

export function OwnerAnalytics({
  channels,
  onManage,
}: {
  channels: Schema['ChannelMarketChannelView'][]
  onManage: (channel?: Schema['ChannelMarketChannelView']) => void
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [filters, setFilters] = useState<OwnerAnalyticsFilters>(() => {
    const to = new Date()
    return { from: new Date(to.getTime() - 7 * 86400_000).toISOString(), to: to.toISOString() }
  })
  const [from, setFrom] = useState(() => localDateTime(new Date(filters.from)))
  const [to, setTo] = useState(() => localDateTime(new Date(filters.to)))
  const [channelID, setChannelID] = useState('')
  const [model, setModel] = useState('')
  const [filterError, setFilterError] = useState<Error | null>(null)
  const [logsOpen, setLogsOpen] = useState(false)
  const result = useQuery(ownerAnalyticsOptions(filters))
  const data = result.data
  const models = [...new Set(channels.flatMap((channel) => channel.declared_models ?? []))].sort()
  const needsAttention = channels.filter(
    (channel) =>
      ['failed', 'queued', 'running'].includes(channel.verification_status) ||
      ['draft', 'rejected', 'paused'].includes(channel.lifecycle_status) ||
      channel.name_status === 'pending' ||
      channel.name_status === 'rejected',
  )
  function setPeriod(days: number) {
    const end = new Date()
    const start = new Date(end.getTime() - days * 86400_000)
    setFrom(localDateTime(start))
    setTo(localDateTime(end))
    setFilters({
      from: start.toISOString(),
      to: end.toISOString(),
      channel_id: channelID || undefined,
      model: model || undefined,
    })
    setFilterError(null)
  }
  return (
    <div className="owner-analytics">
      <form
        className="owner-analytics-filters"
        onSubmit={(event) => {
          event.preventDefault()
          try {
            setFilters({
              ...analyticsRange(from, to),
              channel_id: channelID || undefined,
              model: model || undefined,
            })
            setFilterError(null)
          } catch (error) {
            setFilterError(error instanceof Error ? error : new Error(String(error)))
          }
        }}
      >
        <label className="field">
          <span>{t('开始时间')}</span>
          <input
            type="datetime-local"
            required
            value={from}
            onChange={(event) => setFrom(event.target.value)}
          />
        </label>
        <label className="field">
          <span>{t('结束时间')}</span>
          <input
            type="datetime-local"
            required
            value={to}
            onChange={(event) => setTo(event.target.value)}
          />
        </label>
        <label className="field">
          <span>{t('渠道')}</span>
          <select value={channelID} onChange={(event) => setChannelID(event.target.value)}>
            <option value="">{t('全部渠道')}</option>
            {channels.map((channel) => (
              <option key={channel.id} value={channel.id}>
                #{channel.id} · {channel.system_display_name}
              </option>
            ))}
          </select>
        </label>
        <label className="field">
          <span>{t('模型')}</span>
          <select value={model} onChange={(event) => setModel(event.target.value)}>
            <option value="">{t('全部模型')}</option>
            {models.map((name) => (
              <option key={name} value={name}>
                {name}
              </option>
            ))}
          </select>
        </label>
        <Button type="submit" disabled={result.isFetching}>
          {t('应用筛选')}
        </Button>
        <div className="owner-analytics-periods">
          <Button variant="quiet" onClick={() => setPeriod(1)}>
            {t('最近 24 小时')}
          </Button>
          <Button variant="quiet" onClick={() => setPeriod(7)}>
            {t('最近 7 天')}
          </Button>
          <Button variant="quiet" onClick={() => setPeriod(30)}>
            {t('最近 30 天')}
          </Button>
          <Button
            variant="quiet"
            disabled={result.isFetching}
            onClick={() => {
              void result.refetch()
              void client.invalidateQueries({ queryKey: ['market-mine'] })
            }}
          >
            {t('刷新数据')}
          </Button>
        </div>
      </form>
      <p className="muted owner-analytics-range">
        {t('当前统计时段')} · {date(filters.from)} → {date(filters.to)} · {t('本地时间')}
      </p>
      <ErrorMessage error={filterError ?? result.error} />
      {result.error && (
        <Button variant="quiet" onClick={() => void result.refetch()}>
          {t('重试')}
        </Button>
      )}
      {result.isPending && <Loading />}
      <Panel
        title="渠道健康待办"
        action={
          <Button variant="quiet" onClick={() => onManage()}>
            {t('管理渠道')}
          </Button>
        }
      >
        <div className="owner-health-summary">
          <strong>
            {channels.filter((channel) => channel.lifecycle_status === 'active').length} /{' '}
            {channels.length}
          </strong>
          <span>{t('服务中的渠道')}</span>
          <span>·</span>
          <span>
            {t('需要关注的渠道')} {needsAttention.length}
          </span>
        </div>
        {needsAttention.length === 0 ? (
          <p className="muted">{t('当前没有需要处理的渠道状态')}</p>
        ) : (
          <ul className="owner-health-list">
            {needsAttention.slice(0, 5).map((channel) => (
              <li key={channel.id}>
                <div>
                  <strong>
                    #{channel.id} · {channel.system_display_name}
                  </strong>
                  <div className="owner-health-status">
                    <Status value={channel.lifecycle_status} />
                    <Status value={channel.verification_status} />
                    {channel.name_status === 'pending' && <span>{t('名称待审核')}</span>}
                    {channel.name_status === 'rejected' && <span>{t('名称未通过审核')}</span>}
                  </div>
                  {channel.last_review_reason && (
                    <p className="muted">{channel.last_review_reason}</p>
                  )}
                </div>
                <Button variant="quiet" onClick={() => onManage(channel)}>
                  {t('查看渠道')}
                </Button>
              </li>
            ))}
          </ul>
        )}
      </Panel>
      {data && (
        <>
          <StatGrid>
            <Stat
              label="净收入"
              value={credits(data.summary.net_micro)}
              hint={t('已扣平台佣金与费用，未扣上游采购成本')}
            />
            <Stat
              label="待结算收入"
              value={credits(data.summary.pending_income_micro)}
              hint={
                data.summary.next_available_at
                  ? `${t('下笔可结算时间')} · ${date(data.summary.next_available_at)}`
                  : t('所选时段无待结算收入')
              }
            />
            <Stat
              label="请求数"
              value={displayInt(data.summary.request_count)}
              hint={`${t('成功率')} ${successPercent(data.summary.success_count, data.summary.request_count)}`}
            />
            <Stat
              label="调用用户数"
              value={displayInt(data.summary.consumer_count)}
              hint={`${t('输出 tokens')} ${displayInt(data.summary.completion_tokens)}`}
            />
          </StatGrid>
          <div className="owner-income-layout">
            <Panel title="收入趋势">
              <IncomeTrend points={data.points} />
            </Panel>
            <Panel title="收入构成">
              <dl className="owner-income-breakdown">
                <div>
                  <dt>{t('用户消费')}</dt>
                  <dd>{credits(data.summary.consumer_micro)}</dd>
                </div>
                <div>
                  <dt>{t('供给毛收入')}</dt>
                  <dd>{credits(data.summary.gross_micro)}</dd>
                </div>
                <div>
                  <dt>{t('平台佣金')}</dt>
                  <dd>{credits(data.summary.commission_micro)}</dd>
                </div>
                <div>
                  <dt>{t('费用')}</dt>
                  <dd>{credits(data.summary.fee_micro)}</dd>
                </div>
                <div>
                  <dt>{t('净收入')}</dt>
                  <dd>{credits(data.summary.net_micro)}</dd>
                </div>
                <div>
                  <dt>{t('已结算')}</dt>
                  <dd>{credits(data.summary.released_income_micro)}</dd>
                </div>
                <div>
                  <dt>{t('已回收')}</dt>
                  <dd>{credits(data.summary.reclaimed_income_micro)}</dd>
                </div>
              </dl>
              <p className="muted">
                {t('收入在保留期结束后自动结算到站内钱包；净收入不等于利润。')}
              </p>
            </Panel>
          </div>
          <Panel title="渠道与模型贡献" description="按所选时段及筛选条件统计">
            <DataTable
              rows={data.channels}
              rowKey={(row) => `${row.channel_id}:${row.model}`}
              caption="渠道与模型贡献"
              empty="所选时段暂无调用或结算记录"
              columns={[
                {
                  label: '渠道',
                  render: (row) => (
                    <>
                      <strong>{row.name}</strong>
                      <p className="muted">#{row.channel_id}</p>
                    </>
                  ),
                },
                { label: '模型', render: (row) => <code>{row.model || '—'}</code> },
                { label: '请求数', render: (row) => displayInt(row.request_count), numeric: true },
                {
                  label: '成功率',
                  render: (row) => successPercent(row.success_count, row.request_count),
                  numeric: true,
                },
                { label: '供给毛收入', render: (row) => credits(row.gross_micro), numeric: true },
                {
                  label: '平台佣金',
                  render: (row) => credits(row.commission_micro),
                  numeric: true,
                },
                { label: '费用', render: (row) => credits(row.fee_micro), numeric: true },
                { label: '净收入', render: (row) => credits(row.net_micro), numeric: true },
              ]}
            />
          </Panel>
          <Panel
            title="逐笔结算"
            action={
              <a
                href={`/api/marketplace/channels/mine/analytics/export${analyticsQuery(filters)}`}
                className="button button-secondary"
                download
              >
                {t('导出筛选结果')}
              </a>
            }
          >
            <p className="muted">
              {t('结算按创建时间筛选；调用按发生时间筛选。显示最近 100 笔，导出包含全部匹配记录。')}
            </p>
            {data.settlements_truncated && (
              <p role="status" className="notice">
                {t('结果超过 100 笔，请导出查看完整结算记录')}
              </p>
            )}
            <DataTable
              rows={data.settlements}
              rowKey={(row) => row.id}
              caption="逐笔结算"
              empty="所选时段暂无结算记录"
              columns={[
                {
                  label: '时间',
                  render: (row) => (
                    <>
                      {date(row.created_at)}
                      <p className="muted">{row.request_id}</p>
                    </>
                  ),
                },
                { label: '渠道', render: (row) => `#${row.channel_id}` },
                { label: '模型', render: (row) => <code>{row.model || '—'}</code> },
                { label: '计费来源', render: (row) => t(row.billing_source) },
                { label: '供给毛收入', render: (row) => credits(row.gross_micro), numeric: true },
                {
                  label: '平台佣金',
                  render: (row) => credits(row.commission_micro),
                  numeric: true,
                },
                { label: '费用', render: (row) => credits(row.fee_micro), numeric: true },
                { label: '净收入', render: (row) => credits(row.net_micro), numeric: true },
                { label: '可结算时间', render: (row) => date(row.available_at) },
                { label: '状态', render: (row) => <Status value={row.state} /> },
              ]}
            />
          </Panel>
          <details
            className="owner-analytics-logs"
            onToggle={(event) => setLogsOpen(event.currentTarget.open)}
          >
            <summary>{t('查看所选时段调用记录')}</summary>
            {logsOpen && (
              <Suspense fallback={<Loading />}>
                <MarketOwnerLogs key={analyticsQuery(filters)} filters={filters} />
              </Suspense>
            )}
          </details>
        </>
      )}
    </div>
  )
}
