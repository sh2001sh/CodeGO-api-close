// "用量" tab: /api/audit/usage (+stat, export) mirrors /api/log/self but
// scoped to the audit module's read path. Shares the same stat-card and chart
// treatment as the usage-logs pages.
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import { resourceOptions } from '../../lib/queries'
import { credits, date } from '../../lib/format'
import { useTranslation } from '../../lib/i18n'
import { DataTable } from '../../components/data-table'
import { Button, ErrorMessage, Loading, Stat, StatGrid, Status } from '../../components/ui'
import { BarChart } from './chart'
import { bucketByDay } from './bucket'
import { displayInt } from './format'
import { useCursorPager } from './pagination'
import { emptyFilters, usageQuery, type UsageFilters } from './types'
import { UsageFilterBar } from './filter-bar'
import { rangeFromPreset, type Range, type RangePreset } from './time-range'

export function AuditUsageTab(props: { isAdmin?: boolean }) {
  const { t } = useTranslation()
  const [filters, setFilters] = useState<UsageFilters>(emptyFilters)
  const [preset, setPreset] = useState<RangePreset>('7d')
  const [range, setRange] = useState<Range>(() => rangeFromPreset('7d'))
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
      'audit-usage',
      (signal) =>
        api
          .GET('/api/audit/usage', {
            signal,
            params: { query: { ...query, page_size: 50, cursor: pager.cursor || undefined } },
          })
          .then((result) => unwrap(result)),
      [...keyDeps, pager.cursor],
    ),
  )
  const stat = useQuery(
    resourceOptions(
      'audit-usage-stat',
      (signal) =>
        api
          .GET('/api/audit/usage/stat', { signal, params: { query } })
          .then((result) => unwrap(result)),
      keyDeps,
    ),
  )
  const exportParams = new URLSearchParams(
    Object.entries({ ...query, cursor: pager.cursor || undefined })
      .filter(([, value]) => value !== undefined)
      .map(([key, value]) => [key, String(value)]),
  )
  return (
    <>
      <ErrorMessage error={stat.error ?? list.error} />
      {stat.data && (
        <StatGrid>
          <Stat label="请求数" value={displayInt(stat.data.requests)} />
          <Stat label="扣费合计" value={credits(stat.data.amount)} />
          <Stat label="缓存 token" value={displayInt(stat.data.cached_tokens)} />
        </StatGrid>
      )}
      <div className="filters">
        <UsageFilterBar
          filters={filters}
          range={range}
          preset={preset}
          onChange={(nextFilters, nextRange, nextPreset) => {
            setFilters(nextFilters)
            setRange(nextRange)
            setPreset(nextPreset)
            pager.reset()
          }}
          showUser={props.isAdmin}
          showKey
          showChannel
        />
        <a className="button button-quiet" href={`/api/audit/usage/export?${exportParams}`}>
          {t('导出本页')}
        </a>
      </div>
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
      {list.isFetching && <Loading />}
      <DataTable
        rows={list.data?.items ?? []}
        rowKey={(row) => `${row.request_id}-${row.created_at}`}
        empty="暂无用量记录"
        columns={[
          { label: '时间', render: (row) => date(row.created_at) },
          { label: '模型', render: (row) => row.model },
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
    </>
  )
}
