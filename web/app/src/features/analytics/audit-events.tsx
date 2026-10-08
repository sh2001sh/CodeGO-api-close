// "事件" tab: /api/audit/events, a broader log of top-up/consume/manage/
// system/error/refund events (the pre-settlement event stream).
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import { resourceOptions } from '../../lib/queries'
import { credits, date } from '../../lib/format'
import { useTranslation } from '../../lib/i18n'
import { DataTable } from '../../components/data-table'
import { Button, ErrorMessage, Loading } from '../../components/ui'
import { displayInt } from './format'
import { useCursorPager } from './pagination'
import { emptyFilters, usageQuery, type UsageFilters } from './types'
import { UsageFilterBar } from './filter-bar'
import { rangeFromPreset, type Range, type RangePreset } from './time-range'

const eventTypeLabels: Record<number, string> = {
  0: '未知',
  1: '充值',
  2: '消费',
  3: '管理',
  4: '系统',
  5: '失败',
  6: '退款',
}

export function AuditEventsTab(props: { isAdmin?: boolean }) {
  const { t } = useTranslation()
  const [filters, setFilters] = useState<UsageFilters>(emptyFilters)
  const [preset, setPreset] = useState<RangePreset>('7d')
  const [range, setRange] = useState<Range>(() => rangeFromPreset('7d'))
  const [eventType, setEventType] = useState('')
  const pager = useCursorPager()
  const query = usageQuery(filters, range)
  const keyDeps = [
    query.model ?? '',
    query.user_id ?? '',
    query.key_id ?? '',
    query.channel_id ?? '',
    query.from ?? '',
    query.to ?? '',
    eventType,
    pager.cursor,
  ]
  const list = useQuery(
    resourceOptions(
      'audit-events',
      (signal) =>
        api
          .GET('/api/audit/events', {
            signal,
            params: {
              query: {
                ...query,
                page_size: 50,
                cursor: pager.cursor || undefined,
                event_type: eventType ? Number(eventType) : undefined,
              },
            },
          })
          .then((result) => unwrap(result)),
      keyDeps,
    ),
  )
  const exportParams = new URLSearchParams(
    Object.entries({ ...query, event_type: eventType || undefined })
      .filter(([, value]) => value !== undefined)
      .map(([key, value]) => [key, String(value)]),
  )
  return (
    <>
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
        <label className="field" htmlFor="event-type-filter">
          <span>{t('事件类型')}</span>
          <select
            id="event-type-filter"
            value={eventType}
            onChange={(event) => {
              setEventType(event.target.value)
              pager.reset()
            }}
          >
            <option value="">{t('全部')}</option>
            {Object.entries(eventTypeLabels).map(([value, label]) => (
              <option key={value} value={value}>
                {t(label)}
              </option>
            ))}
          </select>
        </label>
        <a className="button button-quiet" href={`/api/audit/events/export?${exportParams}`}>
          {t('导出本页')}
        </a>
      </div>
      <ErrorMessage error={list.error} />
      {list.isFetching && <Loading />}
      <DataTable
        rows={list.data?.items ?? []}
        rowKey={(row) => row.id}
        empty="暂无事件记录"
        columns={[
          { label: '时间', render: (row) => date(row.created_at) },
          { label: '类型', render: (row) => t(eventTypeLabels[row.event_type] ?? '未知') },
          { label: '模型', render: (row) => row.model || '—' },
          { label: '内容', render: (row) => row.content, hideOnMobile: true },
          { label: '金额', render: (row) => credits(row.amount), numeric: true },
          {
            label: '渠道 ID',
            render: (row) => displayInt(row.channel_id),
            numeric: true,
            hideOnMobile: true,
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
    </>
  )
}
