import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import { resourceOptions } from '../lib/queries'
import { credits, date } from '../lib/format'
import { useTranslation } from '../lib/i18n'
import { DataTable } from '../components/data-table'
import { Button, ErrorMessage, Loading, PageHeader, Status, StatGrid, Stat } from '../components/ui'
import { BarChart } from '../features/analytics/chart'
import { bucketByDay } from '../features/analytics/bucket'
import { UsageFilterBar } from '../features/analytics/filter-bar'
import { LogDetailDrawer } from '../features/analytics/log-detail'
import { useCursorPager } from '../features/analytics/pagination'
import { displayInt } from '../features/analytics/format'
import {
  emptyFilters,
  usageQuery,
  type AuditUsage,
  type UsageFilters,
} from '../features/analytics/types'
import { rangeFromPreset, type Range, type RangePreset } from '../features/analytics/time-range'

export default function AdminLogsPage() {
  const { t } = useTranslation()
  const [filters, setFilters] = useState<UsageFilters>(emptyFilters)
  const [preset, setPreset] = useState<RangePreset>('7d')
  const [range, setRange] = useState<Range>(() => rangeFromPreset('7d'))
  const [detail, setDetail] = useState<AuditUsage | null>(null)
  const pager = useCursorPager()
  const query = usageQuery(filters, range)
  const keyDeps = [
    query.model ?? '',
    query.user_id ?? '',
    query.key_id ?? '',
    query.channel_id ?? '',
    query.from ?? '',
    query.to ?? '',
  ]

  const list = useQuery(
    resourceOptions(
      'admin-logs',
      (signal) =>
        api
          .GET('/api/log/', {
            signal,
            params: { query: { ...query, page_size: 50, cursor: pager.cursor || undefined } },
          })
          .then((result) => unwrap(result)),
      [...keyDeps, pager.cursor],
    ),
  )
  const stat = useQuery(
    resourceOptions(
      'admin-logs-stat',
      (signal) =>
        api.GET('/api/log/stat', { signal, params: { query } }).then((result) => unwrap(result)),
      keyDeps,
    ),
  )

  const applyFilters = (next: UsageFilters, nextRange: Range, nextPreset: RangePreset) => {
    setFilters(next)
    setRange(nextRange)
    setPreset(nextPreset)
    pager.reset()
  }

  const exportParams = new URLSearchParams(
    Object.entries({ ...query, cursor: pager.cursor || undefined })
      .filter(([, value]) => value !== undefined)
      .map(([key, value]) => [key, String(value)]),
  )

  return (
    <>
      <PageHeader
        title="全站日志"
        action={
          <a className="button button-quiet" href={`/api/log/export?${exportParams}`}>
            {t('导出本页')}
          </a>
        }
      />
      <ErrorMessage error={stat.error ?? list.error} />
      {stat.data && (
        <StatGrid>
          <Stat label="请求数" value={displayInt(stat.data.requests)} />
          <Stat label="扣费合计" value={credits(stat.data.amount)} />
          <Stat label="输入 token" value={displayInt(stat.data.prompt_tokens)} />
          <Stat label="输出 token" value={displayInt(stat.data.completion_tokens)} />
        </StatGrid>
      )}
      {BigInt(stat.data?.prompt_tokens_unknown_requests ?? 0) > 0 && (
        <p className="subtle">
          {t('输入 token 汇总不含历史统计异常的请求；扣费合计包含全部请求。')}
        </p>
      )}
      <UsageFilterBar
        filters={filters}
        range={range}
        preset={preset}
        onChange={applyFilters}
        showUser
        showKey
        showChannel
      />
      {list.isFetching && <Loading />}
      {!list.isFetching && (list.data?.items?.length ?? 0) > 0 && (
        <BarChart
          title="每日请求数"
          valueLabel="请求数"
          points={bucketByDay(
            list.data?.items ?? [],
            (row) => row.created_at,
            () => 1,
          )}
        />
      )}
      <DataTable
        rows={list.data?.items ?? []}
        rowKey={(row) => `${row.request_id}-${row.created_at}`}
        onRowClick={setDetail}
        empty="暂无使用记录"
        columns={[
          { label: '时间', render: (row) => date(row.created_at) },
          { label: '用户 ID', render: (row) => displayInt(row.user_id), numeric: true },
          { label: '模型', render: (row) => row.model },
          {
            label: '渠道 ID',
            render: (row) => displayInt(row.channel_id),
            numeric: true,
            hideOnMobile: true,
          },
          {
            label: '输入 token',
            render: (row) =>
              BigInt(row.prompt_tokens) < 0 ? t('历史统计异常') : displayInt(row.prompt_tokens),
            numeric: true,
          },
          {
            label: '输出 token',
            render: (row) => displayInt(row.completion_tokens),
            numeric: true,
          },
          { label: '扣费', render: (row) => credits(row.amount), numeric: true },
          { label: '终态', render: (row) => <Status value={row.terminal} /> },
        ]}
      />
      <div className="filters section">
        <Button variant="quiet" disabled={pager.atStart} onClick={pager.goBack}>
          {t('上一页')}
        </Button>
        <Button
          variant="quiet"
          disabled={!list.data?.has_more}
          onClick={() => pager.goNext(list.data?.next_cursor ?? '')}
        >
          {t('下一页')}
        </Button>
      </div>
      <LogDetailDrawer usage={detail} onClose={() => setDetail(null)} />
    </>
  )
}
