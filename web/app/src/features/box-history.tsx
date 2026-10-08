import { useTranslation } from '../lib/i18n'
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import { resourceOptions } from '../lib/queries'
import { credits, date } from '../lib/format'
import { DataTable } from '../components/data-table'
import { Button, ErrorMessage, Loading } from '../components/ui'

export const boxHistoryOptions = (before = '') =>
  resourceOptions(
    'box-history',
    (signal) =>
      api
        .GET('/api/blind-box/history', {
          signal,
          params: { query: { before: before || undefined } },
        })
        .then(unwrap),
    [before],
  )

export function BoxHistory() {
  const { t } = useTranslation()
  const [before, setBefore] = useState('')
  const query = useQuery(boxHistoryOptions(before))
  const records = query.data
  return (
    <section className="section">
      <h2>{t('开启记录')}</h2>
      <ErrorMessage error={query.error} />
      {query.isError && (
        <Button variant="quiet" onClick={() => void query.refetch()}>
          {t('重试')}
        </Button>
      )}
      {query.isPending && <Loading />}
      {!query.isPending && !query.isError && (
        <DataTable
          rows={records ?? []}
          rowKey={(row) => String(row.id)}
          columns={[
            { label: '奖励', render: (row) => row.reward.title },
            {
              label: '额度',
              render: (row) =>
                row.reward.kind === 'credits' ? credits(row.reward.amount_micro) : '—',
            },
            { label: '开启时间', render: (row) => date(row.created_at) },
            { label: '保底补足', render: (row) => credits(row.guarantee_credits_micro ?? 0) },
          ]}
        />
      )}
      <div className="filters section">
        <Button
          variant="quiet"
          disabled={!before || query.isFetching}
          onClick={() => setBefore('')}
        >
          {t('返回最新')}
        </Button>
        <Button
          variant="quiet"
          disabled={query.isFetching || query.isError || (records?.length ?? 0) < 30}
          onClick={() => setBefore(String(records?.at(-1)?.id ?? ''))}
        >
          {t('下一页')}
        </Button>
      </div>
    </section>
  )
}
