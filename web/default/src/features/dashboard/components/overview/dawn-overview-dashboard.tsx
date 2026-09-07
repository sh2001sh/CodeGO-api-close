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
import { useMemo, useState, type CSSProperties } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import {
  Activity,
  ArrowUpRight,
  BrainCircuit,
  Copy,
  Gift,
  Globe,
  PieChart,
  Plug,
  RefreshCw,
  Wallet,
  Waypoints,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { copyToClipboard } from '@/lib/copy-to-clipboard'
import { getCurrencyLabel } from '@/lib/currency'
import { formatCompactNumber, formatNumber, formatQuota } from '@/lib/format'
import { getConfiguredServerAddress } from '@/lib/server-url'
import { cn } from '@/lib/utils'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import {
  getUserGroupOverview,
  getUserQuotaDates,
} from '@/features/dashboard/api'
import { aggregateUsageByBucket } from '@/features/dashboard/lib/overview-usage'
import type { QuotaDataItem } from '@/features/dashboard/types'
import { DawnQueryError } from '@/features/dawn/components/query-error'
import { getUserLogs } from '@/features/usage-logs/api'
import type { UsageLog } from '@/features/usage-logs/data/schema'
import { getTokenComposition } from '../../lib/token-composition'

type TimeRange = '24h' | '7d' | '30d'

const RANGE_CONFIG: Record<
  TimeRange,
  { hours: number; defaultTime: 'hour' | 'day'; bars: number }
> = {
  '24h': { hours: 24, defaultTime: 'hour', bars: 24 },
  '7d': { hours: 24 * 7, defaultTime: 'day', bars: 7 },
  '30d': { hours: 24 * 30, defaultTime: 'day', bars: 15 },
}

export function DawnOverviewDashboard() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const [range, setRange] = useState<TimeRange>('24h')
  const [queryNow, setQueryNow] = useState(() => Math.floor(Date.now() / 1000))

  const config = RANGE_CONFIG[range]
  const startTimestamp = queryNow - config.hours * 3600
  const endTimestamp = queryNow

  const usageQuery = useQuery({
    queryKey: [
      'dawn-overview',
      'usage',
      range,
      startTimestamp,
      endTimestamp,
      config.defaultTime,
    ],
    queryFn: () =>
      getUserQuotaDates({
        start_timestamp: startTimestamp,
        end_timestamp: endTimestamp,
        default_time: config.defaultTime,
      }),
    staleTime: 60_000,
  })

  const groupQuery = useQuery({
    queryKey: ['dawn-overview', 'groups', range, config.hours],
    queryFn: () => getUserGroupOverview(config.hours),
    staleTime: 60_000,
  })

  const logsQuery = useQuery({
    queryKey: ['dawn-overview', 'logs', range, startTimestamp, endTimestamp],
    queryFn: async () => {
      // The dashboard only needs token totals. Fetch one bounded aggregate
      // page instead of walking every log page sequentially on first paint.
      const response = await getUserLogs({
        type: 2,
        start_timestamp: startTimestamp,
        end_timestamp: endTimestamp,
        p: 1,
        page_size: 1000,
      })
      if (!response.success || !response.data) {
        throw new Error(response.message || '用量记录加载失败')
      }
      return response.data.items as UsageLog[]
    },
    staleTime: 60_000,
  })

  const rows = useMemo(
    () => (usageQuery.data?.data ?? []) as QuotaDataItem[],
    [usageQuery.data]
  )

  const groups = useMemo(() => groupQuery.data?.data ?? [], [groupQuery.data])

  const logs = useMemo(() => {
    return (logsQuery.data ?? []) as UsageLog[]
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

  const totalGroupRequests = groups.reduce(
    (sum, group) => sum + Number(group.request_count ?? 0),
    0
  )
  const avgLatencyMs =
    totalGroupRequests > 0
      ? groups.reduce(
          (sum, group) =>
            sum +
            Number(group.avg_latency_ms ?? 0) *
              Number(group.request_count ?? 0),
          0
        ) / totalGroupRequests
      : null

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
    const bucketSeconds = range === '24h' ? 60 * 60 : 24 * 60 * 60
    const aggregated = aggregateUsageByBucket(
      rows.map((row) => ({
        created_at: Number(row.created_at),
        quota: Number(row.quota ?? 0),
      })),
      bucketSeconds,
      config.bars
    )
    const values = aggregated.map((row) => row.quota)
    const max = Math.max(1, ...values)
    return aggregated.map((source) => {
      const value = source.quota
      const date = new Date(Number(source?.created_at ?? 0) * 1000)
      const label =
        range === '24h'
          ? `${String(date.getHours()).padStart(2, '0')}:00`
          : `${date.getMonth() + 1}/${date.getDate()}`
      return {
        label,
        value,
        height: Math.max(4, Math.round((value / max) * 100)),
      }
    })
  }, [rows, range, config.bars])

  const tokenTotal =
    tokenSplit.cacheHit + tokenSplit.cacheMiss + tokenSplit.output
  const hitDeg = tokenTotal > 0 ? (tokenSplit.cacheHit / tokenTotal) * 360 : 0
  const missDeg = tokenTotal > 0 ? (tokenSplit.cacheMiss / tokenTotal) * 360 : 0
  const donutStyle = {
    '--swp': `${hitDeg}deg`,
    '--swp2': `${missDeg}deg`,
  } as CSSProperties

  const baseUrl = getConfiguredServerAddress()
  const activeGroup = groups[0]?.group ?? user?.group ?? '—'

  const balance = Number(user?.quota ?? 0)

  const handleRefresh = async () => {
    const nextNow = Math.floor(Date.now() / 1000)
    if (nextNow !== queryNow) {
      setQueryNow(nextNow)
      toast.success('数据已刷新')
      return
    }
    await Promise.all([
      usageQuery.refetch(),
      groupQuery.refetch(),
      logsQuery.refetch(),
    ])
    toast.success('数据已刷新')
  }

  const handleCopy = async (text: string) => {
    const success = await copyToClipboard(text)
    if (success) toast.success('已复制')
  }

  return (
    <div className='dawn-overview'>
      <div className='pgw'>
        <div>
          <div className='kick'>
            <span className='n'>C·01</span>
            OVERVIEW
          </div>
          <h1 className='pg'>概览</h1>
        </div>
        <div style={{ display: 'flex', gap: 10, alignItems: 'center' }}>
          <div className='seg'>
            {(['24h', '7d', '30d'] as TimeRange[]).map((value) => (
              <button
                key={value}
                className={cn('rngb', range === value && 'on')}
                aria-pressed={range === value}
                onClick={() => {
                  setQueryNow(Math.floor(Date.now() / 1000))
                  setRange(value)
                }}
              >
                {value.toUpperCase()}
              </button>
            ))}
          </div>
          <button
            className='btn mini'
            onClick={() => void handleRefresh()}
            disabled={
              usageQuery.isFetching ||
              groupQuery.isFetching ||
              logsQuery.isFetching
            }
          >
            <RefreshCw
              size={14}
              className={
                usageQuery.isFetching ||
                groupQuery.isFetching ||
                logsQuery.isFetching
                  ? 'animate-spin'
                  : ''
              }
            />
            刷新
          </button>
        </div>
      </div>

      {usageQuery.isError || groupQuery.isError || logsQuery.isError ? (
        <DawnQueryError
          title='概览数据加载不完整'
          description='部分统计暂时不可用，请重新加载。'
          onRetry={() => void handleRefresh()}
          retrying={
            usageQuery.isFetching ||
            groupQuery.isFetching ||
            logsQuery.isFetching
          }
        />
      ) : null}

      <div className='apicard rise'>
        <div className='ap'>
          <span className='apl'>
            <Globe size={13} />
            BASE URL
          </span>
          <code>{baseUrl}</code>
          <button className='btn mini' onClick={() => void handleCopy(baseUrl)}>
            <Copy size={14} />
            复制
          </button>
        </div>

        <div className='apdiv' />
        <div className='ap'>
          <span className='apl'>
            <Plug size={13} />
            协议
          </span>
          <span className='prtag'>OpenAI Compatible</span>
          <span className='prtag'>Anthropic</span>
          <span className='prtag'>Gemini</span>
        </div>
        <div className='apdiv' />
        <div className='ap'>
          <span className='apl'>
            <Waypoints size={13} />
            可用分组
          </span>
          <span
            className='prtag'
            style={{
              borderColor: 'rgba(184,86,46,.4)',
              color: 'var(--dawn-copper)',
            }}
          >
            {activeGroup}
          </span>
          <Link className='btn mini' to='/market'>
            去市场 <ArrowUpRight size={13} />
          </Link>
        </div>
      </div>

      <div
        className='hero rise'
        style={{ animationDelay: '.06s', marginTop: 18 }}
      >
        <div className='halo' />
        <div className='hl'>
          <Wallet size={13} />
          可用总额度
          <span
            style={{
              marginLeft: 'auto',
              fontFamily: 'var(--dawn-mono)',
              fontSize: 10,
              border: '1px solid var(--dawn-line)',
              borderRadius: 6,
              padding: '2px 8px',
            }}
          >
            {getCurrencyLabel()}
          </span>
        </div>
        <div className='amt'>
          <span>{formatQuota(balance)}</span>
          <Link className='btn mini' to='/wallet' style={{ marginLeft: 12 }}>
            <ArrowUpRight size={14} />
            钱包
          </Link>
          <Link className='btn mini primary' to='/blind-box'>
            <Gift size={14} />
            盲盒
          </Link>
        </div>
      </div>

      <div className='sb4 rise' style={{ animationDelay: '.12s' }}>
        <div className='cell'>
          <b>{formatQuota(spend)}</b>
          <span>期间消耗</span>
        </div>
        <div className='cell'>
          <b>{formatNumber(requests)}</b>
          <span>请求次数</span>
        </div>
        <div className='cell'>
          <b>{formatCompactNumber(tokens)}</b>
          <span>Tokens</span>
        </div>
        <div className='cell'>
          <b>
            {avgLatencyMs != null ? `${(avgLatencyMs / 1000).toFixed(2)}` : '—'}
            <span className='u'>s</span>
          </b>
          <span>平均耗时</span>
        </div>
      </div>

      <div className='cols'>
        <div className='panel rise' style={{ animationDelay: '.18s' }}>
          <div className='ph2'>
            <Activity size={15} />
            请求走势
            <span className='win'>{range.toUpperCase()} · 消耗</span>
          </div>
          {bars.length === 0 ? (
            <div className='pp-empty' style={{ padding: '28px 0' }}>
              <Activity size={20} />
              <span>窗口内暂无数据</span>
            </div>
          ) : (
            <>
              <div className='ubars'>
                {bars.map((bar, index) => (
                  <div className='u1' key={`${bar.label}-${index}`}>
                    <Tooltip>
                      <TooltipTrigger
                        render={
                          <button
                            type='button'
                            className='bar2'
                            aria-label={`${bar.label} · ${formatQuota(bar.value)}`}
                            style={{ height: `${bar.height}%` }}
                          />
                        }
                      />
                      <TooltipContent>
                        {bar.label} · {formatQuota(bar.value)}
                      </TooltipContent>
                    </Tooltip>
                  </div>
                ))}
              </div>
              <div className='mt-2 flex justify-between gap-2'>
                {bars
                  .filter(
                    (_bar, index) =>
                      index % Math.max(1, Math.ceil((bars.length - 1) / 4)) ===
                        0 || index === bars.length - 1
                  )
                  .map((bar, index) => (
                    <span
                      key={`${bar.label}-${index}`}
                      style={{
                        textAlign: 'center',
                        fontFamily: 'var(--dawn-mono)',
                        fontSize: 9,
                        color: 'var(--dawn-ink2)',
                        whiteSpace: 'nowrap',
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                      }}
                    >
                      {bar.label}
                    </span>
                  ))}
              </div>
            </>
          )}
        </div>

        <div className='panel rise' style={{ animationDelay: '.24s' }}>
          <div className='ph2'>
            <PieChart size={15} />
            {t('Token 构成')}
            <span className='win'>
              {range.toUpperCase()} ·{' '}
              {t('最近 {{count}} 条调用', { count: logs.length })}
            </span>
          </div>
          <div className='donutwrap'>
            <div
              className='donut'
              data-empty={tokenTotal === 0}
              style={donutStyle}
              aria-hidden='true'
            />
            <div className='dleg'>
              <div className='li'>
                <i className='token-cache-hit' />
                <span>{t('缓存命中输入 Token')}</span>
                <b>{formatCompactNumber(tokenSplit.cacheHit)}</b>
              </div>
              <div className='li'>
                <i className='token-cache-miss' />
                <span>{t('缓存未命中输入 Token')}</span>
                <b>{formatCompactNumber(tokenSplit.cacheMiss)}</b>
              </div>
              <div className='li'>
                <i className='token-output' />
                <span>{t('输出 Token')}</span>
                <b>{formatCompactNumber(tokenSplit.output)}</b>
              </div>
            </div>
          </div>
          {tokenTotal === 0 && (
            <p className='text-muted-foreground text-sm'>
              {t('暂无 Token 用量')}
            </p>
          )}
        </div>
      </div>

      <div
        className='panel rise'
        style={{ animationDelay: '.3s', marginTop: 18 }}
      >
        <div className='ph2'>
          <BrainCircuit size={15} />
          模型用量
          <span className='win'>{range.toUpperCase()} · TOP 5</span>
        </div>
        {modelUsage.length ? (
          <div className='musage'>
            {modelUsage.map((item) => (
              <div className='mu' key={item.name}>
                <span className='n'>{item.name}</span>
                <span className='v'>{formatQuota(item.quota)}</span>
                <span className='tbar'>
                  <i
                    style={{
                      width: `${Math.round(
                        (item.quota / Math.max(1, modelUsage[0].quota)) * 100
                      )}%`,
                    }}
                  />
                </span>
              </div>
            ))}
          </div>
        ) : (
          <div className='pp-empty' style={{ padding: '18px 0' }}>
            <Activity size={20} />
            <span>窗口内暂无消耗记录</span>
          </div>
        )}
      </div>
    </div>
  )
}
