import { useTranslation } from '../../lib/i18n'
import { useState } from 'react'
import { useMutation, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import { resourceOptions } from '../../lib/queries'
import { credits, date, toMicroCredits } from '../../lib/format'
import { DataTable } from '../../components/data-table'
import { Button, ErrorMessage, Field, Status, confirmAction } from '../../components/ui'
import { MarketForm, text } from './form'
import {
  analyticsQuery,
  ownerLogCursor,
  type OwnerAnalyticsFilters,
  type OwnerLogCursor,
} from './owner-analytics-format'

export const incomeOptions = (admin = false) =>
  admin
    ? resourceOptions('market-admin-income', (signal) =>
        api.GET('/api/marketplace/admin/owner-income', { signal }).then((result) => unwrap(result)),
      )
    : resourceOptions('market-income', (signal) =>
        api
          .GET('/api/marketplace/channels/mine/observability', { signal })
          .then((result) => unwrap(result)),
      )
export { MarketSecurity, securityOptions } from './security-audit'

export function MarketIncome(props: { admin?: boolean }) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const rows = useSuspenseQuery(incomeOptions(props.admin)).data
  const [operationID, setOperationID] = useState(() => crypto.randomUUID())
  const [result, setResult] = useState('')
  const reclaim = useMutation({
    mutationFn: (body: {
      operation_id: string
      owner_user_ids: bigint[]
      max_amount_micro: bigint
    }) => api.POST('/api/marketplace/admin/owner-income/release', { body }).then(unwrap),
    onSuccess: (response) => {
      setOperationID(crypto.randomUUID())
      setResult(
        `${t('处理状态：')}${t(response.status)}${t('，已回收 ')}${response.reclaimed_count}${t(' 笔，共 ')}${credits(response.reclaimed_amount_micro)}${response.error_message ? `；${response.error_message}` : ''}`,
      )
      void client.invalidateQueries({ queryKey: ['market-admin-income'] })
    },
  })
  return (
    <section className="section">
      <h2>{t('渠道收入与结算')}</h2>
      <DataTable
        rows={rows}
        rowKey={(row) => String(row.owner_user_id)}
        columns={[
          { label: '渠道主', render: (row) => String(row.owner_user_id) },
          { label: '请求数', render: (row) => String(row.request_count), numeric: true },
          { label: '累计净收入', render: (row) => credits(row.total_income_micro), numeric: true },
          { label: '待结算', render: (row) => credits(row.pending_income_micro), numeric: true },
          { label: '已结算', render: (row) => credits(row.released_income_micro), numeric: true },
          { label: '已回收', render: (row) => credits(row.reclaimed_income_micro), numeric: true },
        ]}
      />
      <p className="muted section">{t('收入在保留期结束后自动结算到钱包。')}</p>
      {props.admin && (
        <details className="section">
          <summary>{t('回收已结算收入')}</summary>
          <MarketForm
            pending={reclaim.isPending}
            submit="回收收入"
            onSubmit={(fields) => {
              const owners = text(fields, 'income-owners')
                .split(',')
                .map((value) => value.trim())
              if (owners.some((id) => !/^[1-9]\d*$/.test(id)))
                throw new Error('渠道主 ID 使用逗号分隔的正整数')
              const amount = text(fields, 'income-amount')
              // Validate before the confirmation Promise, inside MarketForm's error boundary.
              const body = {
                operation_id: operationID,
                owner_user_ids: owners.map(BigInt),
                max_amount_micro: amount ? BigInt(toMicroCredits(amount)) : 0n,
              }
              void confirmAction({
                title: '确认从指定渠道主的钱包回收已结算收入？',
                danger: true,
              }).then((ok) => {
                if (ok) reclaim.mutate(body)
              })
            }}
          >
            <Field name="income-owners" label="渠道主 ID（逗号分隔）" required />
            <Field name="income-amount" label="回收 credits（留空回收全部）" />
          </MarketForm>
          <ErrorMessage error={reclaim.error} />
          {result && (
            <p className="notice" role="status">
              {result}
            </p>
          )}
        </details>
      )}
    </section>
  )
}

export const ownerLogsOptions = (cursor?: OwnerLogCursor, filters?: OwnerAnalyticsFilters) =>
  resourceOptions(
    'market-owner-logs',
    (signal) =>
      api
        .GET('/api/marketplace/channels/mine/logs', {
          signal,
          params: { query: { page_size: 50, ...cursor, ...filters } },
        })
        .then((result) => unwrap(result)),
    [cursor?.before ?? '', String(cursor?.before_id ?? ''), analyticsQuery(filters)],
  )
export const ownerUsageOptions = (filters?: OwnerAnalyticsFilters) =>
  resourceOptions(
    'market-owner-usage',
    (signal) =>
      api
        .GET('/api/marketplace/channels/mine/user-usage', { signal, params: { query: filters } })
        .then((result) => unwrap(result)),
    [analyticsQuery(filters)],
  )
export function MarketOwnerLogs({ filters }: { filters?: OwnerAnalyticsFilters } = {}) {
  const { t } = useTranslation()
  const [cursors, setCursors] = useState<(OwnerLogCursor | undefined)[]>([undefined])
  const rows = useSuspenseQuery(ownerLogsOptions(cursors.at(-1), filters)).data
  const consumers = useSuspenseQuery(ownerUsageOptions(filters)).data
  return (
    <section className="section">
      <div className="page-header">
        <h2>{t('渠道调用记录')}</h2>
        <a
          className="button button-quiet"
          href={`/api/marketplace/channels/mine/logs/export${analyticsQuery(filters)}`}
          download
        >
          {t(filters ? '导出筛选结果' : '导出全部调用记录')}
        </a>
      </div>
      <DataTable
        rows={rows}
        rowKey={(row) => String(row.id)}
        columns={[
          { label: '时间', render: (row) => date(row.created_at) },
          { label: '渠道', render: (row) => String(row.channel_id) },
          { label: '用户', render: (row) => String(row.user_id) },
          { label: '模型', render: (row) => row.model },
          { label: '消费', render: (row) => credits(row.amount_micro), numeric: true },
          { label: '状态', render: (row) => <Status value={row.terminal} /> },
        ]}
      />
      <div className="filters section">
        <Button
          variant="quiet"
          disabled={cursors.length === 1}
          onClick={() => setCursors(cursors.slice(0, -1))}
        >
          {t('上一页')}
        </Button>
        <span>{cursors.length}</span>
        <Button
          variant="quiet"
          disabled={rows.length < 50}
          onClick={() => {
            const last = rows.at(-1)
            if (last) setCursors([...cursors, ownerLogCursor(last)])
          }}
        >
          {t('下一页')}
        </Button>
      </div>
      <h3 className="section">{t(filters ? '所选时段的用户消费' : '全部历史调用的用户消费')}</h3>
      <DataTable
        rows={Object.values(consumers)}
        rowKey={(row) => String(row.user_id)}
        columns={[
          { label: '用户', render: (row) => String(row.user_id) },
          { label: '调用次数', render: (row) => String(row.request_count), numeric: true },
          { label: '消费', render: (row) => credits(row.amount_micro), numeric: true },
        ]}
      />
    </section>
  )
}
