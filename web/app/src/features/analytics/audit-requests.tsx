// "请求" tab: /api/audit/requests list + detail drawer with attempts timeline
// and the sampled request/response body when available.
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import { resourceOptions } from '../../lib/queries'
import { date } from '../../lib/format'
import { useTranslation } from '../../lib/i18n'
import { DataTable } from '../../components/data-table'
import { Button, Callout, Drawer, ErrorMessage, Loading, Status } from '../../components/ui'
import { displayInt } from './format'
import { useCursorPager } from './pagination'
import { emptyFilters, usageQuery, type AuditRequestAudit, type UsageFilters } from './types'
import { UsageFilterBar } from './filter-bar'
import { rangeFromPreset, type Range, type RangePreset } from './time-range'

function AttemptsTimeline(props: { requestID: string }) {
  const { t } = useTranslation()
  const attempts = useQuery(
    resourceOptions(
      `audit-attempts-${props.requestID}`,
      (signal) =>
        api
          .GET('/api/audit/requests/{request}/attempts', {
            signal,
            params: { path: { request: props.requestID }, query: { page_size: 50 } },
          })
          .then((result) => unwrap(result)),
      [],
    ),
  )
  if (attempts.isPending) return <Loading rows={2} />
  if (attempts.error) return <ErrorMessage error={attempts.error} />
  const items = attempts.data?.items ?? []
  if (items.length === 0) return <Callout tone="info">{t('暂无重试记录')}</Callout>
  return (
    <ol className="analytics-attempt-timeline">
      {items.map((attempt) => (
        <li key={attempt.attempt_id}>
          <div className="row-actions">
            <strong>
              {t('尝试')} #{displayInt(attempt.attempt_no)}
            </strong>
            <Status value={attempt.status} />
          </div>
          <dl className="kv">
            <dt>{t('渠道 ID')}</dt>
            <dd>{displayInt(attempt.channel_id)}</dd>
            <dt>{t('故障域')}</dt>
            <dd>{attempt.fault_domain || '—'}</dd>
            <dt>{t('耗时')}</dt>
            <dd>{displayInt(attempt.duration_ms)} ms</dd>
            <dt>{t('开始时间')}</dt>
            <dd>{date(attempt.started_at)}</dd>
          </dl>
        </li>
      ))}
    </ol>
  )
}

function SamplePreview(props: { requestID: string }) {
  const { t } = useTranslation()
  const sample = useQuery({
    ...resourceOptions(
      `audit-sample-${props.requestID}`,
      (signal) =>
        api
          .GET('/api/audit/samples/{request}', {
            signal,
            params: { path: { request: props.requestID } },
          })
          .then((result) => unwrap(result)),
      [],
    ),
    retry: false,
  })
  if (sample.isPending) return <Loading rows={1} />
  if (sample.error) return <Callout tone="info">{t('没有可查看的采样记录')}</Callout>
  return (
    <div className="form-stack">
      <h3>{t('请求体')}</h3>
      <pre className="code-block">{JSON.stringify(sample.data?.request, null, 2)}</pre>
      <h3>{t('响应体')}</h3>
      <pre className="code-block">{JSON.stringify(sample.data?.response, null, 2)}</pre>
    </div>
  )
}

export function AuditRequestsTab(props: { isAdmin?: boolean }) {
  const { t } = useTranslation()
  const [filters, setFilters] = useState<UsageFilters>(emptyFilters)
  const [preset, setPreset] = useState<RangePreset>('7d')
  const [range, setRange] = useState<Range>(() => rangeFromPreset('7d'))
  const [selected, setSelected] = useState<AuditRequestAudit | null>(null)
  const pager = useCursorPager()
  const query = usageQuery(filters, range)
  const keyDeps = [
    query.model ?? '',
    query.user_id ?? '',
    query.key_id ?? '',
    query.channel_id ?? '',
    query.from ?? '',
    query.to ?? '',
    pager.cursor,
  ]
  const list = useQuery(
    resourceOptions(
      'audit-requests',
      (signal) =>
        api
          .GET('/api/audit/requests', {
            signal,
            params: { query: { ...query, page_size: 50, cursor: pager.cursor || undefined } },
          })
          .then((result) => unwrap(result)),
      keyDeps,
    ),
  )
  return (
    <>
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
      <ErrorMessage error={list.error} />
      {list.isFetching && <Loading />}
      <DataTable
        rows={list.data?.items ?? []}
        rowKey={(row) => row.request_id}
        onRowClick={setSelected}
        empty="暂无请求记录"
        columns={[
          { label: '时间', render: (row) => date(row.started_at) },
          { label: '模型', render: (row) => row.model },
          { label: '状态', render: (row) => <Status value={row.status} /> },
          { label: '重试次数', render: (row) => displayInt(row.retry_count), numeric: true },
          { label: '尝试次数', render: (row) => displayInt(row.attempts_count), numeric: true },
          {
            label: '可计费',
            render: (row) =>
              row.status === 'historical_unknown' ? t('未知') : row.billable ? t('是') : t('否'),
          },
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
      <Drawer
        open={selected !== null}
        onOpenChange={(open) => !open && setSelected(null)}
        title="请求详情"
        description={selected?.request_id}
      >
        {selected && (
          <div className="form-stack">
            {selected.status === 'historical_unknown' && (
              <Callout tone="info" title="历史结果未知">
                {t('历史记录缺少最终响应状态，请以用量日志和账单明细核对实际扣费。')}
              </Callout>
            )}
            <dl className="kv">
              <dt>{t('追踪 ID')}</dt>
              <dd className="mono">{selected.trace_id}</dd>
              <dt>{t('协议')}</dt>
              <dd>{selected.protocol}</dd>
              <dt>{t('分组')}</dt>
              <dd>{selected.group_name}</dd>
              <dt>{t('状态码')}</dt>
              <dd>{displayInt(selected.status_code)}</dd>
              <dt>{t('错误码')}</dt>
              <dd>{selected.error_code || '—'}</dd>
              <dt>{t('最终渠道 ID')}</dt>
              <dd>{displayInt(selected.final_channel_id)}</dd>
              <dt>{t('开始时间')}</dt>
              <dd>{date(selected.started_at)}</dd>
              <dt>{t('结束时间')}</dt>
              <dd>
                {selected.completed_at?.startsWith('0001-01-01')
                  ? '—'
                  : date(selected.completed_at)}
              </dd>
            </dl>
            <h3>{t('重试时间线')}</h3>
            <AttemptsTimeline requestID={selected.request_id} />
            {props.isAdmin && (
              <>
                <h3>{t('采样内容')}</h3>
                <SamplePreview requestID={selected.request_id} />
              </>
            )}
          </div>
        )}
      </Drawer>
    </>
  )
}
