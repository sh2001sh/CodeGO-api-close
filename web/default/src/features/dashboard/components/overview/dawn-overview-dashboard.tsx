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

For commercial licensing, please contact support@quantumnous.com
*/
import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { getRouteApi, Link, useNavigate } from '@tanstack/react-router'
import i18n from '@/i18n/config'
import { ArrowUpRight, Copy, Gift, KeyRound, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { copyToClipboard } from '@/lib/copy-to-clipboard'
import { formatCompactNumber, formatNumber, formatQuota } from '@/lib/format'
import { getConfiguredServerAddress } from '@/lib/server-url'
import { SegmentedControl } from '@/components/ui/segmented-control'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import {
  getUserGroupOverview,
  getUserQuotaDates,
} from '@/features/dashboard/api'
import {
  buildOverviewUsageTimeline,
  getOverviewGroupLatency,
} from '@/features/dashboard/lib/overview-usage'
import type { QuotaDataItem } from '@/features/dashboard/types'
import { DawnQueryError } from '@/features/dawn/components/query-error'
import { getUserLogs } from '@/features/usage-logs/api'
import type { UsageLog } from '@/features/usage-logs/data/schema'
import { getTokenComposition } from '../../lib/token-composition'
import './overview.css'

type TimeRange = '24h' | '7d' | '30d'

const RANGE_CONFIG: Record<
  TimeRange,
  { hours: number; defaultTime: 'hour' | 'day' }
> = {
  '24h': { hours: 24, defaultTime: 'hour' },
  '7d': { hours: 24 * 7, defaultTime: 'day' },
  '30d': { hours: 24 * 30, defaultTime: 'day' },
}

export function DawnOverviewDashboard() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const navigate = useNavigate()
  const { range: selectedRange } = getRouteApi(
    '/_authenticated/dashboard/$section'
  ).useSearch()
  const range: TimeRange = selectedRange ?? '24h'
  const [queryNow, setQueryNow] = useState(() => Math.floor(Date.now() / 1000))

  const config = RANGE_CONFIG[range]
  const startTimestamp = queryNow - config.hours * 3600
  const endTimestamp = queryNow

  const usageQuery = useQuery({
    queryKey: [
      'dawn-overview',
      'usage',
      user?.id,
      range,
      startTimestamp,
      endTimestamp,
      config.defaultTime,
    ],
    queryFn: async () => {
      const response = await getUserQuotaDates({
        start_timestamp: startTimestamp,
        end_timestamp: endTimestamp,
        default_time: config.defaultTime,
      })
      if (!response.success) throw new Error(i18n.t('用量记录加载失败'))
      return response
    },
    staleTime: 60_000,
    placeholderData: (previousData, previousQuery) =>
      previousQuery?.queryKey[2] === user?.id &&
      previousQuery?.queryKey[3] === range
        ? previousData
        : undefined,
  })

  const groupQuery = useQuery({
    queryKey: [
      'dawn-overview',
      'groups',
      user?.id,
      range,
      config.hours,
      queryNow,
    ],
    queryFn: async () => {
      const response = await getUserGroupOverview(config.hours)
      if (!response.success)
        throw new Error(response.message || i18n.t('用量记录加载失败'))
      return response
    },
    staleTime: 60_000,
    placeholderData: (previousData, previousQuery) =>
      previousQuery?.queryKey[2] === user?.id &&
      previousQuery?.queryKey[3] === range
        ? previousData
        : undefined,
  })

  const logsQuery = useQuery({
    queryKey: [
      'dawn-overview',
      'logs',
      user?.id,
      range,
      startTimestamp,
      endTimestamp,
    ],
    queryFn: async () => {
      // This is a bounded sample of consumption logs, not the quota projection.
      const response = await getUserLogs({
        type: 2,
        start_timestamp: startTimestamp,
        end_timestamp: endTimestamp,
        p: 1,
        // The API caps this endpoint at 100 rows; request the real bound so
        // the dashboard does not imply that it received a larger full slice.
        page_size: 100,
      })
      if (!response.success || !response.data) {
        throw new Error(response.message || i18n.t('用量记录加载失败'))
      }
      return {
        items: response.data.items as UsageLog[],
        total: response.data.total,
      }
    },
    staleTime: 60_000,
    placeholderData: (previousData, previousQuery) =>
      previousQuery?.queryKey[2] === user?.id &&
      previousQuery?.queryKey[3] === range
        ? previousData
        : undefined,
  })

  const failedQuery = useQuery({
    queryKey: [
      'dawn-overview',
      'failed',
      user?.id,
      range,
      startTimestamp,
      endTimestamp,
    ],
    queryFn: async () => {
      const response = await getUserLogs({
        type: 5,
        start_timestamp: startTimestamp,
        end_timestamp: endTimestamp,
        p: 1,
        page_size: 1,
      })
      if (!response.success || !response.data)
        throw new Error(response.message || i18n.t('用量记录加载失败'))
      return response.data.total
    },
    staleTime: 60_000,
    placeholderData: (previousData, previousQuery) =>
      previousQuery?.queryKey[2] === user?.id &&
      previousQuery?.queryKey[3] === range
        ? previousData
        : undefined,
  })

  const rows = useMemo(
    () => (usageQuery.data?.data ?? []) as QuotaDataItem[],
    [usageQuery.data]
  )

  const groups = useMemo(() => groupQuery.data?.data ?? [], [groupQuery.data])

  const logs = useMemo(() => {
    return logsQuery.data?.items ?? []
  }, [logsQuery.data])

  const spend = useMemo(
    () => rows.reduce((sum, row) => sum + Number(row.quota ?? 0), 0),
    [rows]
  )
  const requests = useMemo(
    () => rows.reduce((sum, row) => sum + Number(row.count ?? 0), 0),
    [rows]
  )
  const tokens = useMemo(
    () => rows.reduce((sum, row) => sum + Number(row.token_used ?? 0), 0),
    [rows]
  )

  const avgLatencyMs = useMemo(() => getOverviewGroupLatency(groups), [groups])

  const tokenSplit = useMemo(() => getTokenComposition(logs), [logs])

  const modelUsage = useMemo(() => {
    const map = new Map<string, number>()
    rows.forEach((row) => {
      const model = row.model_name || 'unknown'
      const quota = Number(row.quota ?? 0)
      if (quota > 0) map.set(model, (map.get(model) ?? 0) + quota)
    })
    return [...map.entries()]
      .sort((a, b) => b[1] - a[1])
      .slice(0, 5)
      .map(([name, quota]) => ({ name, quota }))
  }, [rows])

  const bars = useMemo(() => {
    const bucketSeconds = range === '24h' ? 3600 : 86400
    const aggregated = buildOverviewUsageTimeline(
      rows.map((row) => ({
        created_at: Number(row.created_at),
        quota: Number(row.quota ?? 0),
      })),
      startTimestamp,
      endTimestamp,
      bucketSeconds
    )
    const max = Math.max(1, ...aggregated.map((row) => row.quota))
    return aggregated.map((source) => {
      const date = new Date(source.created_at * 1000)
      const label =
        range === '24h'
          ? date.toLocaleTimeString(i18n.language, {
              hour: '2-digit',
              minute: '2-digit',
              hour12: false,
            })
          : date.toLocaleDateString(i18n.language, {
              month: 'short',
              day: 'numeric',
            })
      const detail = date.toLocaleString(i18n.language, {
        month: 'short',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
        hour12: false,
      })
      return {
        label,
        detail,
        timestamp: source.created_at,
        value: source.quota,
        height: source.quota > 0 ? Math.max(3, (source.quota / max) * 100) : 0,
      }
    })
  }, [rows, range, startTimestamp, endTimestamp])

  const tokenTotal =
    tokenSplit.cacheHit + tokenSplit.cacheMiss + tokenSplit.output
  const tokenSegments = [
    { label: t('缓存命中输入 Token'), value: tokenSplit.cacheHit, tone: 'hit' },
    {
      label: t('缓存未命中输入 Token'),
      value: tokenSplit.cacheMiss,
      tone: 'miss',
    },
    { label: t('输出 Token'), value: tokenSplit.output, tone: 'output' },
  ]

  const baseUrl = getConfiguredServerAddress()
  const activeGroup = groups[0]?.group ?? user?.group ?? '—'

  const balance = user?.quota == null ? null : Number(user.quota)

  const handleRefresh = async () => {
    const refreshedAt = Math.floor(Date.now() / 1000)
    if (refreshedAt !== queryNow) {
      // Moving the shared window refetches all four queries with current bounds.
      setQueryNow(refreshedAt)
      return
    }
    const results = await Promise.all([
      usageQuery.refetch(),
      groupQuery.refetch(),
      logsQuery.refetch(),
      failedQuery.refetch(),
    ])
    if (results.every((result) => !result.isError)) {
      toast.success(i18n.t('数据已刷新'))
    }
  }

  const handleCopy = async (text: string) => {
    const success = await copyToClipboard(text)
    if (success) toast.success(i18n.t('已复制'))
    else toast.error(i18n.t('Copy failed'))
  }

  const refreshing =
    usageQuery.isFetching ||
    groupQuery.isFetching ||
    logsQuery.isFetching ||
    failedQuery.isFetching
  const usageReady = usageQuery.isSuccess
  const logsReady = logsQuery.isSuccess
  const hasTokenSample = logsReady && logs.length > 0 && tokenTotal > 0
  const metrics = [
    { label: t('期间消耗'), value: usageReady ? formatQuota(spend) : '--' },
    { label: t('请求次数'), value: usageReady ? formatNumber(requests) : '--' },
    {
      label: t('Tokens'),
      value: usageReady ? formatCompactNumber(tokens) : '--',
    },
    {
      label: t('可用分组平均耗时'),
      value:
        groupQuery.isSuccess && avgLatencyMs != null
          ? `${(avgLatencyMs / 1000).toFixed(2)} s`
          : '--',
    },
  ]
  const rangeLabel = `${new Date(startTimestamp * 1000).toLocaleDateString(i18n.language, { month: 'short', day: 'numeric' })} – ${new Date(endTimestamp * 1000).toLocaleDateString(i18n.language, { month: 'short', day: 'numeric' })}`

  return (
    <div className='market-overview' aria-busy={refreshing}>
      <header className='market-overview-heading'>
        <h1>{t('概览')}</h1>
        <div className='market-overview-actions'>
          <SegmentedControl
            value={range}
            onChange={(value) => {
              setQueryNow(Math.floor(Date.now() / 1000))
              void navigate({
                to: '/dashboard/$section',
                params: { section: 'overview' },
                search: { range: value },
              })
            }}
            options={[
              { value: '24h', label: '24H' },
              { value: '7d', label: '7D' },
              { value: '30d', label: '30D' },
            ]}
          />
          <button
            type='button'
            className='overview-button'
            disabled={refreshing}
            onClick={() => void handleRefresh()}
          >
            <RefreshCw
              size={16}
              className={refreshing ? 'animate-spin' : ''}
              aria-hidden='true'
            />
            {t('刷新')}
          </button>
        </div>
      </header>

      {(usageQuery.isError ||
        groupQuery.isError ||
        logsQuery.isError ||
        failedQuery.isError) && (
        <DawnQueryError
          title={t('概览数据加载不完整')}
          description={t('部分统计暂时不可用，请重新加载。')}
          onRetry={() => void handleRefresh()}
          retrying={refreshing}
        />
      )}

      <div className='overview-summary'>
        <section className='overview-account' aria-label={t('可用总额度')}>
          <span className='overview-label'>{t('可用总额度')}</span>
          <strong className='overview-balance'>
            {balance == null ? '--' : formatQuota(balance)}
          </strong>
          <div className='market-overview-actions'>
            <Link className='overview-text-link' to='/wallet'>
              {t('钱包')}
              <ArrowUpRight size={15} aria-hidden='true' />
            </Link>
            <Link className='overview-text-link' to='/blind-box'>
              <Gift size={15} aria-hidden='true' />
              {t('盲盒')}
            </Link>
            <Link className='overview-text-link' to='/keys'>
              <KeyRound size={15} aria-hidden='true' />
              {t('API Keys')}
            </Link>
          </div>
        </section>
        <section className='overview-connection' aria-label={t('BASE URL')}>
          <span className='overview-label'>{t('BASE URL')}</span>
          <div className='overview-endpoint'>
            <code>{baseUrl}</code>
            <button
              className='overview-copy'
              type='button'
              onClick={() => void handleCopy(baseUrl)}
              aria-label={t('复制')}
              title={t('复制')}
            >
              <Copy size={16} aria-hidden='true' />
            </button>
          </div>
          <div className='overview-connection-meta'>
            <Link to='/market' className='overview-text-link'>
              <span>
                {t('可用分组')} · <strong>{activeGroup}</strong>
              </span>
              <ArrowUpRight size={15} aria-hidden='true' />
            </Link>
          </div>
        </section>
      </div>

      <section className='overview-usage-summary' aria-label={t('期间消耗')}>
        <dl className='overview-metrics' aria-live='polite'>
          {metrics.map((metric) => (
            <div key={metric.label}>
              <dt>{metric.label}</dt>
              <dd>{metric.value}</dd>
            </div>
          ))}
          <div>
            <dt>{t('Failed')}</dt>
            <dd>
              <Link
                className='overview-failed-link'
                to='/usage-logs/$section'
                params={{ section: 'common' }}
                search={{
                  type: ['5'],
                  startTime: startTimestamp,
                  endTime: endTimestamp,
                }}
              >
                <span>
                  {failedQuery.isSuccess
                    ? formatNumber(failedQuery.data)
                    : '--'}
                </span>
                <ArrowUpRight size={16} aria-hidden='true' />
              </Link>
            </dd>
          </div>
        </dl>
      </section>

      <div className='overview-chart-grid'>
        <section className='overview-section overview-trend'>
          <header>
            <h2>{t('期间消耗')}</h2>
            <span>{rangeLabel}</span>
          </header>
          {usageQuery.isLoading && (
            <div
              className='overview-chart-loading'
              aria-label={t('Loading...')}
            />
          )}
          {usageQuery.isError && (
            <div className='overview-empty' role='status'>
              {t('用量记录加载失败')}
            </div>
          )}
          {usageReady && rows.length === 0 && (
            <div className='overview-empty'>{t('窗口内暂无消耗记录')}</div>
          )}
          {usageReady && rows.length > 0 && (
            <>
              <div className='overview-bars'>
                {bars.map((bar) => (
                  <div className='overview-bar-slot' key={bar.timestamp}>
                    <Tooltip>
                      <TooltipTrigger
                        render={
                          <button
                            type='button'
                            className='overview-bar-target'
                            aria-label={`${bar.detail} · ${formatQuota(bar.value)}`}
                          >
                            <span
                              className='overview-bar'
                              data-empty={bar.value === 0}
                              style={{ height: `${bar.height}%` }}
                            />
                          </button>
                        }
                      />
                      <TooltipContent>
                        {bar.detail} · {formatQuota(bar.value)}
                      </TooltipContent>
                    </Tooltip>
                  </div>
                ))}
              </div>
              <div className='overview-chart-ticks'>
                {bars
                  .filter(
                    (_bar, index) =>
                      index % Math.max(1, Math.ceil((bars.length - 1) / 4)) ===
                        0 || index === bars.length - 1
                  )
                  .map((bar) => (
                    <time
                      key={bar.timestamp}
                      dateTime={new Date(bar.timestamp * 1000).toISOString()}
                    >
                      {bar.label}
                    </time>
                  ))}
              </div>
            </>
          )}
        </section>

        <section className='overview-section overview-composition'>
          <header>
            <h2>{t('Token 构成')}</h2>
            <span>{t('日志样本')}</span>
          </header>
          <div className='overview-token-total'>
            <strong>
              {hasTokenSample ? formatCompactNumber(tokenTotal) : '--'}
            </strong>
            <span>{t('Tokens')}</span>
          </div>
          {hasTokenSample && (
            <div className='overview-token-segments' aria-hidden='true'>
              {tokenSegments
                .filter((segment) => segment.value > 0)
                .map((segment) => (
                  <span
                    key={segment.tone}
                    className={`overview-${segment.tone}`}
                    style={{ flexGrow: segment.value, flexBasis: '6px' }}
                  />
                ))}
            </div>
          )}
          <dl className='overview-token-list'>
            {tokenSegments.map((segment) => (
              <div key={segment.tone}>
                <dt>
                  <i
                    className={`overview-${segment.tone}`}
                    aria-hidden='true'
                  />
                  {segment.label}
                </dt>
                <dd>
                  {hasTokenSample ? formatCompactNumber(segment.value) : '--'}
                </dd>
              </div>
            ))}
          </dl>
          {logsQuery.isLoading && (
            <p className='overview-scope'>{t('Loading...')}</p>
          )}
          {logsQuery.isError && (
            <p className='overview-scope' role='status'>
              {t('用量记录加载失败')}
            </p>
          )}
          {logsReady && logs.length === 0 && (
            <p className='overview-scope'>
              {t('本时段暂无消费日志样本，无法拆分 Token。')}
            </p>
          )}
          {logsReady && logs.length > 0 && tokenTotal === 0 && (
            <p className='overview-scope'>{t('最近日志未提供 Token 明细。')}</p>
          )}
          {logsReady && logs.length > 0 && (
            <p className='overview-scope'>
              {t(
                '本时段最近 {{count}} 条消费日志（共 {{total}} 条）；与上方按小时汇总的 Token 总量口径不同。',
                { count: logs.length, total: logsQuery.data.total }
              )}
            </p>
          )}
        </section>
      </div>

      <div className='overview-detail-grid'>
        <section className='overview-section'>
          <header>
            <h2>{t('模型用量')}</h2>
            <Link
              to='/dashboard/$section'
              params={{ section: 'models' }}
              className='overview-text-link'
            >
              {t('Model Call Analytics')}
              <ArrowUpRight size={15} aria-hidden='true' />
            </Link>
          </header>
          {usageQuery.isLoading && (
            <div
              className='overview-chart-loading'
              aria-label={t('Loading...')}
            />
          )}
          {usageQuery.isError && (
            <div className='overview-empty'>{t('用量记录加载失败')}</div>
          )}
          {usageReady && modelUsage.length === 0 && (
            <div className='overview-empty'>
              <span>{t('窗口内暂无消耗记录')}</span>
              <Link className='overview-text-link' to='/market'>
                {t('去市场')}
                <ArrowUpRight size={15} aria-hidden='true' />
              </Link>
            </div>
          )}
          {usageReady && modelUsage.length > 0 && (
            <div className='overview-models'>
              {modelUsage.map((item) => (
                <div key={item.name}>
                  <Link
                    className='overview-model-name'
                    to='/usage-logs/$section'
                    params={{ section: 'common' }}
                    search={{
                      model: item.name,
                      startTime: startTimestamp,
                      endTime: endTimestamp,
                    }}
                  >
                    {item.name}
                  </Link>
                  <span className='overview-model-track'>
                    <i
                      style={{
                        width: `${(item.quota / Math.max(1, modelUsage[0].quota)) * 100}%`,
                      }}
                    />
                  </span>
                  <strong>{formatQuota(item.quota)}</strong>
                </div>
              ))}
            </div>
          )}
        </section>

        <section className='overview-section'>
          <header>
            <h2>{t('Usage Logs')}</h2>
            <Link
              className='overview-text-link'
              to='/usage-logs/$section'
              params={{ section: 'common' }}
              search={{ startTime: startTimestamp, endTime: endTimestamp }}
            >
              {t('查看日志')}
              <ArrowUpRight size={15} aria-hidden='true' />
            </Link>
          </header>
          {logsQuery.isLoading && (
            <div
              className='overview-chart-loading'
              aria-label={t('Loading...')}
            />
          )}
          {logsQuery.isError && (
            <div className='overview-empty'>{t('用量记录加载失败')}</div>
          )}
          {logsReady && logs.length === 0 && (
            <div className='overview-empty'>{t('窗口内暂无消耗记录')}</div>
          )}
          {logsReady && logs.length > 0 && (
            <div className='overview-recent-wrap'>
              <table className='overview-recent'>
                <thead>
                  <tr>
                    <th>{t('Time')}</th>
                    <th>{t('Model')}</th>
                    <th>{t('Group')}</th>
                    <th>{t('Tokens')}</th>
                    <th>{t('Quota')}</th>
                  </tr>
                </thead>
                <tbody>
                  {logs.slice(0, 5).map((log) => (
                    <tr key={log.id}>
                      <td>
                        {new Date(log.created_at * 1000).toLocaleString(
                          i18n.language
                        )}
                      </td>
                      <td>{log.model_name || '—'}</td>
                      <td>{log.group || '—'}</td>
                      <td>
                        {formatCompactNumber(
                          Number(log.prompt_tokens || 0) +
                            Number(log.completion_tokens || 0)
                        )}
                      </td>
                      <td>{formatQuota(Number(log.quota || 0))}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      </div>
    </div>
  )
}
